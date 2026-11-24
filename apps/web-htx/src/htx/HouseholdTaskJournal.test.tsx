import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { createApiClient } from "@/api/client";
import type { components } from "@/api/generated/application";
import { MOCK_PERSONAS } from "@/api/mock/personas";
import { AuthContextForTests, type AuthContextValue } from "@/auth/AuthProvider";
import { buildPrincipal } from "@/auth/principal";
import { HouseholdTaskJournal, reasonError } from "./HouseholdTaskJournal";

type S = components["schemas"];
const rec = (id: string, log: string, step: number, state: "done" | "partial", done: string) => ({
  id, log_ref: log, step_seq: step, state, done_on: done, sop_version_id: "v", inputs: [{ name: "URE", qty: "25", unit: "kg" }], gps_missing: true, gps: { source: "denied" },
  actor_ref: "a", event_time: `${done}T01:00:00Z`, recorded_at: `${done}T01:00:00Z`, version: 1,
  details: { actual_materials: [{ material_code: "URE", name: "URE", unit: "kg", dose_per_ha: "50", quantity: "25" }], backfilled: false, zero_confirmed: false },
}) as unknown as S["HouseholdTaskRecord"];
const task = (seq: number, name: string, activity: string) => ({ seq, stage_seq: seq, task_seq: 1, name, activity_code: activity, window_kind: "date_range", from_date: seq === 2 ? "2026-11-21" : null, to_date: seq === 2 ? "2026-11-23" : null, expected_date: null, status: "due", undated: seq !== 2, completion_status: "due", done_on: null, can_complete: true });
const checklist = {
  season_ref: "s", current_event_id: "e", records: [rec("r1", "l1", 1, "done", "2026-11-20"), rec("r2", "l2", 2, "partial", "2026-11-22")],
  progress: { as_of_date: "2026-11-24", done: 1, skipped: 0, total: 2, percent: "50.00", stages: [
    { seq: 1, name: "Chuẩn bị", status: "done", tasks: [task(1, "Làm đất", "lam_dat")] },
    { seq: 2, name: "Chăm sóc", status: "in_progress", tasks: [task(2, "Bón thúc", "bon_phan")] },
  ] },
} as unknown as S["HouseholdChecklist"];

function setup(persona: string, failOnce = false) {
  const p = MOCK_PERSONAS.find((x) => x.id === persona)!;
  const principal = buildPrincipal({ sub: p.me.actor_ref, amr: p.amr, realm_access: { roles: p.me.roles } }, p.me);
  const calls: { path: string; key: string | null; body: unknown }[] = [];
  let failures = failOnce ? 1 : 0;
  const api = createApiClient({ baseUrl: "https://api.test", getAccessToken: () => "t", fetch: async (input) => {
    const req = input as Request; const path = new URL(req.url).pathname;
    if (path.endsWith("/history")) return Response.json({ items: [rec("r1", "l1", 1, "done", "2026-11-20"), { ...rec("r9", "l1", 1, "partial", "2026-11-20"), version: 2, correction_reason: "Mới bón một nửa diện tích" }] });
    calls.push({ path, key: req.headers.get("Idempotency-Key"), body: await req.json() });
    if (failures-- > 0) return Response.json({ type: "", title: "Lỗi", status: 503, code: "dependency_unavailable" }, { status: 503 });
    return Response.json({ record: rec("r3", "l1", 1, "done", "2026-11-20"), checklist });
  } });
  const onChanged = vi.fn();
  render(<AuthContextForTests.Provider value={{ status: "authenticated", api, principal } as AuthContextValue}>
    <HouseholdTaskJournal path={{ ref: "c", household_ref: "h", season_ref: "s" }} checklist={checklist} writable onChanged={onChanged} />
  </AuthContextForTests.Provider>);
  return { calls, onChanged };
}

describe("AH-126 nhật ký của vụ", () => {
  it("AC1: lọc theo giai đoạn, loại việc, ngày; mới nhất trước", () => {
    setup("htx-member");
    const list = () => screen.getAllByRole("listitem").map((li) => li.textContent ?? "");
    expect(list()[0]).toContain("Bón thúc");
    fireEvent.change(screen.getByLabelText("Giai đoạn"), { target: { value: "1" } });
    expect(list()).toHaveLength(1); expect(list()[0]).toContain("Làm đất");
    fireEvent.change(screen.getByLabelText("Giai đoạn"), { target: { value: "" } });
    fireEvent.change(screen.getByLabelText("Loại việc"), { target: { value: "bon_phan" } });
    expect(list()).toHaveLength(1); expect(list()[0]).toContain("Bón thúc");
    fireEvent.change(screen.getByLabelText("Loại việc"), { target: { value: "" } });
    fireEvent.change(screen.getByLabelText("Từ ngày"), { target: { value: "2026-11-21" } });
    expect(list()).toHaveLength(1);
  });

  it("AC4, AC5: bỏ tích bắt buộc lý do; thử lại đúng thao tác dùng lại khóa", async () => {
    expect(reasonError("ngắn")).not.toBeNull(); expect(reasonError("Tích nhầm việc chưa làm")).toBeNull();
    const { calls, onChanged } = setup("htx-member", true);
    fireEvent.click(screen.getByRole("button", { name: "Bỏ tích" }));
    const form = screen.getByRole("form", { name: "Bỏ tích việc đã xong" });
    fireEvent.submit(form);
    expect(await within(form).findByRole("alert")).toHaveTextContent("Nhập lý do");
    expect(calls).toHaveLength(0);
    fireEvent.change(within(form).getByLabelText("Lý do *"), { target: { value: "Tích nhầm việc, chưa làm" } });
    fireEvent.submit(form);
    await waitFor(() => expect(calls).toHaveLength(1));
    await waitFor(() => expect(within(form).getByRole("button", { name: "Bỏ tích" })).not.toBeDisabled());
    fireEvent.submit(form);
    await waitFor(() => expect(onChanged).toHaveBeenCalled());
    expect(calls[1]!.key).toBe(calls[0]!.key);
    expect(calls[1]!.path).toBe("/v1/cooperatives/c/households/h/seasons/s/task-records/l1/void");
    expect(calls[1]!.body).toEqual({ expected_record_id: "r1", reason: "Tích nhầm việc, chưa làm", channel: "web" });
  });

  it("AC2: sửa kèm lý do gửi bản ghi bù; lịch sử hiện giá trị cũ – mới và lý do", async () => {
    const { calls } = setup("htx-member");
    fireEvent.click(screen.getAllByRole("button", { name: "Sửa" })[1]!);
    const form = screen.getByRole("form", { name: "Sửa bản ghi" });
    fireEvent.change(within(form).getByLabelText("Kết quả"), { target: { value: "partial" } });
    fireEvent.change(within(form).getByLabelText("Liều/ha URE (kg)"), { target: { value: "30" } });
    fireEvent.change(within(form).getByLabelText("Lý do sửa *"), { target: { value: "Mới bón một nửa diện tích" } });
    fireEvent.submit(form);
    await waitFor(() => expect(calls).toHaveLength(1));
    expect(calls[0]!.path).toBe("/v1/cooperatives/c/households/h/seasons/s/task-records/l1/corrections");
    expect(calls[0]!.body).toMatchObject({ expected_record_id: "r1", state: "partial", done_on: "2026-11-20", reason: "Mới bón một nửa diện tích",
      materials: [{ material_code: "URE", unit: "kg", dose_per_ha: "30" }] });
    fireEvent.click(screen.getAllByRole("button", { name: "Lịch sử" })[1]!);
    const history = await screen.findByRole("region", { name: "Lịch sử bản ghi" });
    expect(await within(history).findByText(/Phiên bản 2/)).toBeTruthy();
    expect(within(history).getByText("Lý do: Mới bón một nửa diện tích")).toBeTruthy();
  });

  it("AH-141 AC1: dòng nhật ký có ngày dự kiến để đối chiếu với ngày làm", () => {
    setup("htx");
    const rows = screen.getAllByRole("listitem").map((li) => li.textContent ?? "");
    expect(rows[0]).toContain("làm 22/11/2026 (dự kiến 21/11/2026 – 23/11/2026)");
    expect(rows[1]).toContain("(không đặt ngày)");
  });

  it("AC6, AH-141 AC4: Đại diện HTX chỉ xem lịch sử, không có nút sửa/xóa", () => {
    setup("htx");
    expect(screen.queryByRole("button", { name: "Sửa" })).toBeNull();
    expect(screen.queryByRole("button", { name: /Bỏ tích|Xóa/ })).toBeNull();
    expect(screen.getAllByRole("button", { name: "Lịch sử" })).toHaveLength(2);
  });
});
