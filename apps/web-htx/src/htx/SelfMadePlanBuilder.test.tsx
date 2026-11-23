import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { createApiClient } from "@/api/client";
import type { HouseholdListItem } from "@/api/types";
import { MOCK_PERSONAS } from "@/api/mock/personas";
import { AuthContextForTests, type AuthContextValue } from "@/auth/AuthProvider";
import { buildPrincipal } from "@/auth/principal";
import { HouseholdSeasonsDialog } from "./HouseholdSeasonsDialog";
import { selfMadeErrors, toSelfMadePlan } from "./SelfMadePlanBuilder";

describe("AH-162 quy trình tự tạo", () => {
  it("kiểm sớm Từ ≤ Đến, bắt buộc nội dung; đánh số seq liên tục theo giai đoạn", () => {
    const stages = [
      { name: "Chuẩn bị", from: "-3", to: "1", tasks: [{ name: "Làm đất", activity: "lam_dat", kind: "date_range" as const, from: "-3", to: "-1" }] },
      { name: "Chăm sóc", from: "", to: "", tasks: [{ name: "Tưới", activity: "tuoi_nuoc", kind: "none" as const, from: "", to: "" }, { name: "Thu hái", activity: "thu_hoach", kind: "date_range" as const, from: "200", to: "210" }] },
    ];
    expect(selfMadeErrors(stages)).toEqual([]);
    expect(selfMadeErrors([{ ...stages[0]!, tasks: [{ ...stages[0]!.tasks[0]!, from: "3", to: "1" }] }])).toContain("Giai đoạn 1, việc 1: Từ, Đến là số ngày, Từ ≤ Đến.");
    expect(selfMadeErrors([])).toContain("Nhập ít nhất một giai đoạn và một việc.");
    const plan = toSelfMadePlan(stages);
    expect(plan.stages).toEqual([{ seq: 1, name: "Chuẩn bị", from_day: -3, to_day: 1 }, { seq: 2, name: "Chăm sóc", from_day: null, to_day: null }]);
    expect(plan.steps.map((s) => [s.seq, s.stage_seq, s.task_seq])).toEqual([[1, 1, 1], [2, 2, 1], [3, 2, 2]]);
    expect(plan.steps[1]!.window).toEqual({ kind: "none" });
  });

  it("loại cây chưa có SOP → Tự tạo quy trình; gửi self_made_plan, không gửi sop_version_id", async () => {
    Object.defineProperty(navigator, "geolocation", { configurable: true, value: undefined });
    const p = MOCK_PERSONAS.find((x) => x.id === "htx-member")!;
    const principal = buildPrincipal({ sub: p.me.actor_ref, name: p.displayName, amr: p.amr, realm_access: { roles: p.me.roles } }, p.me);
    const posts: { path: string; body: Record<string, unknown> }[] = [];
    const api = createApiClient({ baseUrl: "https://api.test", getAccessToken: () => "t", fetch: async (input) => {
      const req = input as Request; const path = new URL(req.url).pathname;
      if (req.method === "POST") {
        const body = await req.json() as Record<string, unknown>; posts.push({ path, body });
        if (path.endsWith("/season-plan-preview")) return Response.json({ as_of_date: "2026-11-24", anchor_date: "2026-12-01", expected_harvest_date: null, sop_version_id: "00000000-0000-0000-0000-000000000000", current_event_id: null, stages: [] });
        return Response.json({ season: { ...body, season_ref: "new", status: "open", sop_snapshot: { name: "Quy trình tự tạo", anchor_name: "Ngày trồng", version_no: 1, steps: [], stages: [], self_made: true } } }, { status: 201 });
      }
      if (path.includes("masterdata/catalogs/")) {
        const name = path.split("/").at(-1);
        const items = name === "crop" ? [{ code: "CAF", name: "Cà phê", attrs: {} }] : name === "rice_variety" ? [{ code: "ARA", name: "Arabica", attrs: { crop_code: "CAF" } }] : [{ code: "DX", name: "Đông xuân", attrs: {} }];
        return Response.json({ items, next_cursor: null });
      }
      if (path === "/v1/cropcycle/variety-bindings") return Response.json({ items: [] });
      return Response.json({ items: [], next_cursor: null });
    } });
    const household = { household_ref: "hh", label: "Hộ A", remaining_area_ha: "1", can_open_season: true } as HouseholdListItem;
    render(<AuthContextForTests.Provider value={{ status: "authenticated", api, principal } as AuthContextValue}>
      <HouseholdSeasonsDialog coopRef="coop" household={household} onClose={() => {}} onSaved={() => {}} />
    </AuthContextForTests.Provider>);
    fireEvent.click(await screen.findByRole("button", { name: "Mở vụ mới" }));
    await screen.findByRole("option", { name: "Cà phê" });
    fireEvent.change(screen.getByLabelText("Loại cây"), { target: { value: "CAF" } });
    fireEvent.change(screen.getByLabelText("Giống"), { target: { value: "ARA" } });
    fireEvent.change(screen.getByLabelText("Mùa vụ"), { target: { value: "DX" } });
    fireEvent.change(screen.getByLabelText("Diện tích vụ (ha)"), { target: { value: "0.50" } });
    fireEvent.click(await screen.findByRole("button", { name: "Tự tạo quy trình" }));
    expect(screen.queryByLabelText("SOP đang áp dụng")).toBeNull();
    fireEvent.change(screen.getByLabelText("Tên mốc gốc"), { target: { value: "Ngày trồng" } });
    fireEvent.change(screen.getByLabelText("Tên giai đoạn 1"), { target: { value: "Chăm sóc" } });
    fireEvent.change(screen.getByLabelText("Việc 1.1"), { target: { value: "Tưới nước" } });
    fireEvent.change(screen.getByLabelText("Loại việc 1.1"), { target: { value: "tuoi_nuoc" } });
    fireEvent.change(screen.getByLabelText("Thời gian 1.1"), { target: { value: "none" } });
    fireEvent.change(screen.getByLabelText("Mốc gốc"), { target: { value: "2026-12-01" } });
    await waitFor(() => expect(posts.some((x) => x.path.endsWith("/season-plan-preview"))).toBe(true));
    await waitFor(() => expect(screen.getByRole("button", { name: "Lưu vụ" })).not.toBeDisabled());
    fireEvent.click(screen.getByRole("button", { name: "Lưu vụ" }));
    await waitFor(() => expect(posts.some((x) => x.path.endsWith("/households/hh/seasons"))).toBe(true));
    const open = posts.find((x) => x.path.endsWith("/households/hh/seasons"))!.body;
    expect(open).not.toHaveProperty("sop_version_id");
    expect(open).toMatchObject({ crop_code: "CAF", variety_code: "ARA", self_made_anchor: "Ngày trồng",
      self_made_plan: { stages: [{ seq: 1, name: "Chăm sóc" }], steps: [{ seq: 1, stage_seq: 1, task_seq: 1, name: "Tưới nước", activity_code: "tuoi_nuoc", window: { kind: "none" } }] } });
  });
});
