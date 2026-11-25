import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { createApiClient } from "@/api/client";
import type { HouseholdListItem } from "@/api/types";
import { MOCK_PERSONAS } from "@/api/mock/personas";
import { AuthContextForTests, type AuthContextValue } from "@/auth/AuthProvider";
import { buildPrincipal } from "@/auth/principal";
import { HouseholdSeasonsDialog, householdAreaError } from "./HouseholdSeasonsDialog";
import type { CalendarPrefill } from "./seasonCalendarModel";

function setup(persona: string, calendarPrefill?: CalendarPrefill) {
  const calls: { key: string | null; body: Record<string, unknown> }[] = [];
  const plans: { key: string | null; body: Record<string, unknown> }[] = [];
  const onSaved = vi.fn();
  const p = MOCK_PERSONAS.find((p) => p.id === persona)!;
  const principal = buildPrincipal({ sub: p.me.actor_ref, name: p.displayName, amr: p.amr, realm_access: { roles: p.me.roles } }, p.me);
  const household = { household_ref: "household", label: "Hộ đã che", remaining_area_ha: "1", can_open_season: true } as HouseholdListItem;
  const api = createApiClient({ baseUrl: "https://api.test", getAccessToken: () => "test", fetch: async (input) => {
    const req = input as Request;
    const path = new URL(req.url).pathname;
    if (path.endsWith("/checklist")) return Response.json({ season_ref: "new-season", current_event_id: "event", progress: { as_of_date: "2026-11-18", stages: [], done: 0, skipped: 0, total: 0, percent: null }, records: [] });
    if (path.endsWith("/plan")) return Response.json({ as_of_date: "2026-11-18", stages: [] });
    if (path.endsWith("/summary")) return Response.json({ status: "open", area_ha: "0.25", mass_t: null, mass_kg: null, yield_t_per_ha: null, harvest_date: null, materials: [],
      tasks: { early: 0, on_time: 0, late: 0, skipped: 0, not_done: 0, total: 0 }, summary_step_seq: null, can_close: false, blockers: ["harvest_required"], unresolved_steps: [],
      harvest_reminder: false, cost_vnd: null, note: null, closed_at: null, closed_by: null });
    if (req.method === "POST") {
      const body = await req.json() as Record<string, unknown>;
      if (path.endsWith("/season-plan-preview")) {
        plans.push({ key: req.headers.get("Idempotency-Key"), body });
        return Response.json({ as_of_date: "2026-11-18", stages: [] });
      }
      calls.push({ key: req.headers.get("Idempotency-Key"), body });
      if (body.area_ha !== "0.20") return Response.json({ type: "", title: "Diện tích vượt", status: 409, code: "cropcycle.household_area_exceeded", remaining_area_ha: "0.20" }, { status: 409 });
      return Response.json({ season: { ...body, season_ref: "new-season", entry_kind: "household", status: "open", gps_missing: true, sop_snapshot: { name: "SOP đã chụp", anchor_name: "Ngày xuống giống", version_no: 1, steps: [{ name: "Chăm sóc" }] } } }, { status: 201 });
    }
    if (path.includes("masterdata/catalogs/")) {
      const name = path.split("/").at(-1);
      const items = name === "crop" ? [{ code: "LUA", name: "Lúa", attrs: {} }] : name === "rice_variety" ? [{ code: "OM5451", name: "OM5451", attrs: { crop_code: "LUA" } }] : [{ code: "DX", name: "Đông xuân", attrs: {} }];
      return Response.json({ items, next_cursor: null });
    }
    if (path === "/v1/cropcycle/sops") return Response.json({ items: [{ ref: "sop", name: "Quy trình lúa", crop: "LUA", scope: "system", status: "published", published_version: 1, anchor_name: "Ngày xuống giống" }], next_cursor: null });
    if (path === "/v1/cropcycle/sops/sop") return Response.json({ versions: [{ id: "version", version_no: 1, state: "active" }] });
    if (path.endsWith("/households")) return Response.json({ items: [{ ...household, remaining_area_ha: "0", can_open_season: false }], next_cursor: null });
    return Response.json({ items: [], next_cursor: null });
  } });
  render(<AuthContextForTests.Provider value={{ status: "authenticated", api, principal } as AuthContextValue}>
    <HouseholdSeasonsDialog coopRef="coop" household={household} calendarPrefill={calendarPrefill} onClose={() => {}} onSaved={onSaved} />
  </AuthContextForTests.Provider>);
  return { calls, onSaved, plans };
}

describe("AH-61 mở vụ theo hộ trên web", () => {
  it("AH-59: điền ngày đầu khung và vẫn lưu được ngày ngoài khung",async()=>{
    Object.defineProperty(navigator,"geolocation",{configurable:true,value:undefined});
    const{calls}=setup("htx-member",{crop_code:"LUA",season_code:"DX",season_year:2026,sow_from:"2026-12-25"});
    await screen.findByRole("option",{name:"Lúa"});
    expect(screen.getByLabelText("Loại cây")).toHaveValue("LUA");expect(screen.getByLabelText("Mùa vụ")).toHaveValue("DX");
    expect(screen.getByLabelText("Mốc gốc")).toHaveValue("2026-12-25");
    fireEvent.change(screen.getByLabelText("Giống"),{target:{value:"OM5451"}});
    fireEvent.change(screen.getByLabelText("Diện tích vụ (ha)"),{target:{value:"0.20"}});
    await screen.findByRole("option",{name:/Quy trình lúa/});
    const anchor=screen.getByLabelText("Ngày xuống giống");
    fireEvent.change(anchor,{target:{value:"2027-02-01"}});
    await waitFor(()=>expect(screen.getByRole("button",{name:"Lưu vụ"})).not.toBeDisabled());
    fireEvent.click(screen.getByRole("button",{name:"Lưu vụ"}));
    await waitFor(()=>expect(calls).toHaveLength(1));expect(calls[0]!.body.anchor_date).toBe("2027-02-01");expect(calls[0]!.body).not.toHaveProperty("calendar_ref");
  });
  it("từ chối số không, âm, số mũ và hơn hai chữ số mà không làm tròn", () => {
    for (const raw of ["0", "-1", "0.001", "0.25001", "1e-2", "1,5", "NaN"]) expect(householdAreaError(raw)).not.toBeNull();
    for (const raw of ["0.01", "1", "1.50"]) expect(householdAreaError(raw)).toBeNull();
  });
  it("Đại diện không có nút mở vụ", async () => {
    setup("htx");
    await screen.findByText("Chưa có vụ theo hộ.");
    expect(screen.queryByRole("button", { name: "Mở vụ mới" })).toBeNull();
  });
  it("giữ form và key khi xung đột, đổi key khi sửa, vẫn lưu khi GPS bị từ chối", async () => {
    Object.defineProperty(navigator, "geolocation", { configurable: true, value: { getCurrentPosition: (_ok: unknown, fail: () => void) => fail() } });
    const { calls, onSaved, plans } = setup("htx-member");
    fireEvent.click(screen.getByRole("button", { name: "Mở vụ mới" }));
    await screen.findByRole("option", { name: "Lúa" });
    fireEvent.change(screen.getByLabelText("Loại cây"), { target: { value: "LUA" } });
    fireEvent.change(screen.getByLabelText("Giống"), { target: { value: "OM5451" } });
    fireEvent.change(screen.getByLabelText("Mùa vụ"), { target: { value: "DX" } });
    fireEvent.change(screen.getByLabelText("Năm"), { target: { value: "2030" } });
    fireEvent.change(screen.getByLabelText("Diện tích vụ (ha)"), { target: { value: "0.50" } });
    await screen.findByRole("option", { name: "Quy trình lúa · bản 1" });
    fireEvent.change(screen.getByLabelText("Ngày xuống giống"), { target: { value: "2030-01-01" } });
    await waitFor(() => expect(screen.getByRole("button", { name: "Lưu vụ" })).not.toBeDisabled());
    fireEvent.click(screen.getByRole("button", { name: "Lưu vụ" }));
    await waitFor(() => expect(calls).toHaveLength(1));
    await waitFor(() => expect(screen.getByRole("button", { name: "Lưu vụ" })).not.toBeDisabled());
    expect((screen.getByLabelText("Diện tích vụ (ha)") as HTMLInputElement).value).toBe("0.50");
    expect((screen.getByLabelText("Ngày xuống giống") as HTMLInputElement).value).toBe("2030-01-01");
    fireEvent.click(screen.getByRole("button", { name: "Lưu vụ" }));
    await waitFor(() => expect(calls).toHaveLength(2));
    expect(calls[1]?.key).toBe(calls[0]?.key);
    await waitFor(() => expect(screen.getByRole("button", { name: "Lưu vụ" })).not.toBeDisabled());
    fireEvent.change(screen.getByLabelText("Diện tích vụ (ha)"), { target: { value: "0.20" } });
    await waitFor(() => expect(screen.getByRole("button", { name: "Lưu vụ" })).not.toBeDisabled());
    fireEvent.click(screen.getByRole("button", { name: "Lưu vụ" }));
    await waitFor(() => expect(onSaved).toHaveBeenCalledOnce());
    expect(calls[2]?.key).not.toBe(calls[0]?.key);
    expect(calls[2]?.body.gps).toEqual({ source: "denied" });
    expect(plans.map((p) => p.body.area_ha)).toEqual(["0.50", "0.20"]);
    expect(plans[1]?.key).not.toBe(plans[0]?.key);
    expect(await screen.findByText("Thiếu GPS", { exact: false })).toBeTruthy();
    expect(screen.getByText("Chăm sóc")).toBeTruthy();
  });
});
