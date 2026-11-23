import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { createApiClient } from "@/api/client";
import type { components } from "@/api/generated/application";
import { MOCK_PERSONAS } from "@/api/mock/personas";
import { AuthContextForTests, type AuthContextValue } from "@/auth/AuthProvider";
import { buildPrincipal } from "@/auth/principal";
import { SeasonPlanEditor, type PlanStage } from "./SeasonPlanEditor";
import { RunningSeasonPlan } from "./SeasonPlanCustomizer";

type S = components["schemas"];

const stages: PlanStage[] = [
  { seq: 1, name: "Chuẩn bị", tasks: [
    { seq: 1, stage_seq: 1, name: "Làm đất", from_date: "2026-09-26", to_date: "2026-09-28", undated: false, locked: true },
    { seq: 2, stage_seq: 1, name: "Gieo sạ", from_date: "2026-09-29", to_date: "2026-09-30", undated: false },
    { seq: 6, stage_seq: 1, name: "Phun thuốc cỏ", custom: true, from_date: null, to_date: null, undated: true },
  ] },
  { seq: 2, name: "Chăm sóc", tasks: [{ seq: 4, stage_seq: 2, name: "Phun thuốc", from_date: null, to_date: null, undated: true, skipped: true }] },
];

describe("AH-113 SeasonPlanEditor", () => {
  it("việc đã xong và việc bỏ qua không có nút; việc tự thêm có nhãn; ▲▼✕ gửi đúng thay đổi", async () => {
    const onChange = vi.fn(async () => {});
    render(<SeasonPlanEditor stages={stages} onChange={onChange} />);
    expect(screen.queryByRole("button", { name: "Sửa Làm đất" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Bỏ Phun thuốc" })).toBeNull();
    expect(screen.getByText(/Đã xong/)).toBeTruthy();
    expect(screen.getByText(/Bỏ qua/)).toBeTruthy();
    expect(screen.getByText(/Tự thêm/)).toBeTruthy();
    expect(screen.getByRole("button", { name: "Đưa Gieo sạ lên" })).not.toBeDisabled();
    expect(screen.getByRole("button", { name: "Đưa Phun thuốc cỏ xuống" })).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "Đưa Phun thuốc cỏ lên" }));
    await waitFor(() => expect(onChange).toHaveBeenLastCalledWith({ kind: "move", step_seq: 6, direction: "up" }));
    fireEvent.click(screen.getByRole("button", { name: "Bỏ Gieo sạ" }));
    await waitFor(() => expect(onChange).toHaveBeenLastCalledWith({ kind: "remove", step_seq: 2 }));
  });

  it("thêm việc: kiểm Từ ≤ Đến và liều > 0 trước khi gửi; gửi đủ nội dung, loại việc, cửa sổ, vật tư", async () => {
    const onChange = vi.fn(async () => {});
    render(<SeasonPlanEditor stages={stages} onChange={onChange} />);
    fireEvent.click(screen.getByRole("button", { name: "＋ Thêm việc vào Chăm sóc" }));
    const form = screen.getByRole("form", { name: "Thêm việc vào Chăm sóc" });
    fireEvent.change(screen.getByLabelText("Nội dung việc"), { target: { value: "Bón bổ sung" } });
    fireEvent.change(screen.getByLabelText("Loại việc"), { target: { value: "bon_phan" } });
    fireEvent.change(screen.getByLabelText("Từ (ngày)"), { target: { value: "20" } });
    fireEvent.change(screen.getByLabelText("Đến (ngày)"), { target: { value: "15" } });
    fireEvent.click(screen.getByRole("button", { name: "Thêm vật tư" }));
    fireEvent.change(screen.getByLabelText("Tên vật tư 1"), { target: { value: "Kali" } });
    fireEvent.change(screen.getByLabelText("Liều/ha 1"), { target: { value: "0" } });
    fireEvent.change(screen.getByLabelText("Đơn vị 1"), { target: { value: "kg" } });
    fireEvent.submit(form);
    expect(await screen.findByText("Đến phải lớn hơn hoặc bằng Từ.")).toBeTruthy();
    expect(screen.getByText("Vật tư 1: liều phải lớn hơn 0.")).toBeTruthy();
    expect(onChange).not.toHaveBeenCalled();
    fireEvent.change(screen.getByLabelText("Đến (ngày)"), { target: { value: "25" } });
    fireEvent.change(screen.getByLabelText("Liều/ha 1"), { target: { value: "30" } });
    fireEvent.submit(form);
    await waitFor(() => expect(onChange).toHaveBeenCalledWith({ kind: "add", step: { name: "Bón bổ sung", stage_seq: 2, activity_code: "bon_phan",
      window: { kind: "date_range", anchor: "nxg", from: 20, to: 25 }, materials: [{ name: "Kali", dose_per_ha: "30", unit: "kg" }] } }));
  });

  it("sửa việc: điền sẵn cửa sổ và vật tư hiện tại", async () => {
    const onChange = vi.fn(async () => {});
    render(<SeasonPlanEditor stages={stages} onChange={onChange} lookup={(seq) => seq === 2 ? { window: { kind: "date_range", anchor: "nxg", from: 0, to: 1 }, materials: [{ name: "Giống", dose_per_ha: "120", unit: "kg" }] } : undefined} />);
    fireEvent.click(screen.getByRole("button", { name: "Sửa Gieo sạ" }));
    expect(screen.getByLabelText("Từ (ngày)")).toHaveValue("0");
    expect(screen.getByLabelText("Liều/ha 1")).toHaveValue("120");
    fireEvent.change(screen.getByLabelText("Đến (ngày)"), { target: { value: "3" } });
    fireEvent.submit(screen.getByRole("form", { name: "Sửa Gieo sạ" }));
    await waitFor(() => expect(onChange).toHaveBeenCalledWith({ kind: "edit", step_seq: 2, window: { kind: "date_range", anchor: "nxg", from: 0, to: 3 }, materials: [{ name: "Giống", dose_per_ha: "120", unit: "kg" }] }));
  });
});

function running(persona: string, fail = 0) {
  const calls: { key: string | null; body: unknown }[] = [];
  const p = MOCK_PERSONAS.find((x) => x.id === persona)!;
  const principal = buildPrincipal({ sub: p.me.actor_ref, name: p.displayName, amr: p.amr, realm_access: { roles: p.me.roles } }, p.me);
  let failures = fail;
  const season = { season_ref: "s1", status: "open", current_event_id: "e1", sop_snapshot: { steps: [], stages: [] } } as unknown as S["HouseholdSeason"];
  const checklist = { season_ref: "s1", current_event_id: "e1", records: [], progress: { as_of_date: "2026-10-01", done: 0, skipped: 0, total: 1, percent: "0.00",
    stages: [{ seq: 1, name: "Chuẩn bị", status: "not_started", tasks: [{ seq: 2, stage_seq: 1, task_seq: 1, name: "Gieo sạ", activity_code: "gieo_sa", window_kind: "none", from_date: null, to_date: null, expected_date: null, status: "due", undated: true, completion_status: "due", done_on: null, can_complete: true }] }] } };
  const api = createApiClient({ baseUrl: "https://api.test", getAccessToken: () => "t", fetch: async (input) => {
    const req = input as Request;
    const path = new URL(req.url).pathname;
    if (path.endsWith("/checklist")) return Response.json(checklist);
    if (path.endsWith("/plan-history")) return Response.json({ items: [{ event_id: "e0", actor_ref: p.me.actor_ref, event_time: "2026-10-01T02:00:00Z", summary: `Bỏ việc "Phun thuốc" (Bỏ qua)` }] });
    if (path.endsWith("/plan-changes")) {
      calls.push({ key: req.headers.get("Idempotency-Key"), body: await req.json() });
      if (failures-- > 0) return Response.json({ type: "", title: "Lỗi", status: 503, code: "dependency_unavailable" }, { status: 503 });
      return Response.json({ season: { ...season, current_event_id: "e2" }, checklist, summary: "ok" });
    }
    return Response.json({});
  } });
  const onSeason = vi.fn();
  render(<AuthContextForTests.Provider value={{ status: "authenticated", api, principal } as AuthContextValue}>
    <RunningSeasonPlan coopRef="c" householdRef="h" season={season} onSeason={onSeason} />
  </AuthContextForTests.Provider>);
  return { calls, onSeason };
}

describe("AH-113 RunningSeasonPlan", () => {
  it("AC2: gửi thay đổi kèm phiên bản vụ; thử lại đúng thao tác dùng lại khóa; hiện lịch sử", async () => {
    const { calls, onSeason } = running("htx-member", 1);
    expect(await screen.findByText(/Bỏ việc "Phun thuốc" \(Bỏ qua\)/)).toBeTruthy();
    expect(screen.getByText(/Bạn/)).toBeTruthy();
    fireEvent.click(await screen.findByRole("button", { name: "Bỏ Gieo sạ" }));
    await waitFor(() => expect(calls).toHaveLength(1));
    fireEvent.click(await screen.findByRole("button", { name: "Bỏ Gieo sạ" }));
    await waitFor(() => expect(onSeason).toHaveBeenCalled());
    expect(calls).toHaveLength(2);
    expect(calls[1]!.key).toBe(calls[0]!.key);
    expect(calls[1]!.body).toEqual({ expected_season_event_id: "e1", change: { kind: "remove", step_seq: 2 } });
  });

  it("AC6: Đại diện HTX chỉ xem lịch sử, không có nút tùy chỉnh", async () => {
    running("htx");
    expect(await screen.findByText(/Bỏ việc "Phun thuốc" \(Bỏ qua\)/)).toBeTruthy();
    expect(screen.queryByRole("region", { name: "Tùy chỉnh quy trình của vụ" })).toBeNull();
    expect(screen.queryByRole("button", { name: /Bỏ Gieo sạ/ })).toBeNull();
  });
});
