import { webcrypto } from "node:crypto";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { IDBFactory } from "fake-indexeddb";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createApiClient } from "@/api/client";
import type { components } from "@/api/generated/application";
import { MOCK_PERSONAS } from "@/api/mock/personas";
import { AuthContextForTests, type AuthContextValue } from "@/auth/AuthProvider";
import { buildPrincipal } from "@/auth/principal";
import { HouseholdChecklist } from "./HouseholdChecklist";
import { queuedTasks } from "./taskJournalQueue";

beforeEach(() => { vi.stubGlobal("indexedDB", new IDBFactory()); vi.stubGlobal("crypto", webcrypto); });

// AH-114 AC1: tích nhanh khi mất mạng → lưu trên máy "Chờ đồng bộ", có dải báo ngoại tuyến.
describe("AH-114 tích nhanh ngoại tuyến trên Web", () => {
  it("lỗi mạng khi xác nhận → bản ghi việc Hoàn tất với vật tư khuyến cáo của việc hiệu lực, dải ngoại tuyến", async () => {
    Object.defineProperty(navigator, "geolocation", { configurable: true, value: { getCurrentPosition: (_ok: unknown, fail: () => void) => fail() } });
    const p = MOCK_PERSONAS.find((x) => x.id === "htx-member")!;
    const principal = buildPrincipal({ sub: p.me.actor_ref, amr: p.amr, realm_access: { roles: p.me.roles } }, p.me);
    const task = { seq: 6, stage_seq: 1, task_seq: 1, name: "Phun thuốc cỏ", status: "due", completion_status: "due", undated: true, from_date: null, to_date: null, done_on: null, can_complete: true, custom: true };
    const checklist = { season_ref: "season", current_event_id: "event", records: [], progress: { as_of_date: "2026-11-24", done: 0, skipped: 0, total: 1, percent: "0.00", stages: [{ seq: 1, name: "Chăm sóc", status: "not_started", tasks: [task] }] } };
    let posts = 0;
    const api = createApiClient({ baseUrl: "https://api.test", getAccessToken: () => "test", fetch: async (input) => {
      const req = input as Request;
      if (req.method === "GET") return Response.json(req.url.includes("photo-key") ? { public_key: "k" } : checklist);
      posts++;
      throw new TypeError("offline");
    } });
    const season = { season_ref: "season", sop_snapshot: { steps: [], stages: [], custom: { added: [{ seq: 6, name: "Phun thuốc cỏ", materials: [{ name: "Thuốc cỏ", dose_per_ha: "1.5", unit: "l" }] }] } } } as unknown as components["schemas"]["HouseholdSeason"];
    render(<AuthContextForTests.Provider value={{ status: "authenticated", api, principal } as AuthContextValue}>
      <HouseholdChecklist coopRef={principal.orgRef!} householdRef="household" seasonRef="season" season={season} />
    </AuthContextForTests.Provider>);
    fireEvent.click(await screen.findByRole("button", { name: /Chăm sóc/ }));
    fireEvent.click(await screen.findByRole("checkbox", { name: "Hoàn thành Phun thuốc cỏ" }));
    fireEvent.click(screen.getByRole("button", { name: "Xác nhận" }));
    expect(await screen.findByText("Đang ngoại tuyến — dữ liệu sẽ đồng bộ khi có mạng")).toBeTruthy();
    await waitFor(async () => expect(await queuedTasks(principal, principal.orgRef!, "household", "season")).toHaveLength(1));
    const [row] = await queuedTasks(principal, principal.orgRef!, "household", "season");
    expect(row!.input).toMatchObject({ step_seq: 6, state: "done", expected_season_event_id: "event", file_refs: [], zero_confirmed: false, channel: "web",
      materials: [{ material_code: "", name: "Thuốc cỏ", unit: "l", dose_per_ha: "1.5" }] });
    expect(posts).toBeGreaterThanOrEqual(1);
    expect(await screen.findByText(/Chờ đồng bộ/)).toBeTruthy();
    expect(screen.queryByRole("region", { name: "Xác nhận hoàn thành" })).toBeNull();
  });
});
