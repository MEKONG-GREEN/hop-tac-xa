import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { createApiClient } from "@/api/client";
import { MOCK_PERSONAS } from "@/api/mock/personas";
import { AuthContextForTests, type AuthContextValue } from "@/auth/AuthProvider";
import { buildPrincipal } from "@/auth/principal";
import { HouseholdChecklist } from "./HouseholdChecklist";

function setup(persona: string, failOnce: boolean | "inflight" = false) {
  const p = MOCK_PERSONAS.find((p) => p.id === persona)!;
  const principal = buildPrincipal({ sub: p.me.actor_ref, amr: p.amr, realm_access: { roles: p.me.roles } }, p.me);
  const calls: { key: string | null; body: unknown }[] = [];
  let isDone = false;
  const task = { seq: 1, name: "Bón phân", status: "due", completion_status: "due", undated: true, from_date: null, to_date: null, done_on: null, can_complete: true };
  const before = { season_ref: "season", current_event_id: "event", records: [], progress: { as_of_date: "2026-11-18", done: 0, skipped: 0, total: 1, percent: "0.00", stages: [{ seq: 1, name: "Chăm sóc", status: "not_started", tasks: [task] }] } };
  const record = { id: "log", log_ref: "log", step_seq: 1, state: "done", done_on: "2026-11-18", gps_missing: true, inputs: [{ name: "URE", qty: "25", unit: "kg" }] };
  const after = { ...before, records: [record], progress: { ...before.progress, done: 1, percent: "100.00", stages: [{ seq: 1, name: "Chăm sóc", status: "done", tasks: [{ ...task, can_complete: false, done_on: record.done_on, completion_status: "done_on_time" }] }] } };
  const api = createApiClient({ baseUrl: "https://api.test", getAccessToken: () => "test", fetch: async (input) => {
    const req = input as Request;
    if (req.method === "GET") return Response.json(isDone ? after : before);
    calls.push({ key: req.headers.get("Idempotency-Key"), body: await req.json() });
    if (failOnce && calls.length === 1) {
      if (failOnce === "inflight") return Response.json({ type: "", title: "Đang xử lý", status: 409, code: "idempotency_in_progress" }, { status: 409 });
      throw new TypeError("offline");
    }
    isDone = true;
    return Response.json({ record, checklist: after }, { status: 201 });
  } });
  render(<AuthContextForTests.Provider value={{ status: "authenticated", api, principal } as AuthContextValue}>
    <HouseholdChecklist coopRef="coop" householdRef="household" seasonRef="season" />
  </AuthContextForTests.Provider>);
  return calls;
}

describe("AH-88 checklist", () => {
  it.each([true, "inflight"] as const)("hai chạm, hủy không ghi; retry giữ key/GPS (%s)", async (failure) => {
    Object.defineProperty(navigator, "geolocation", { configurable: true, value: { getCurrentPosition: (_ok: unknown, fail: () => void) => fail() } });
    const calls = setup("htx-member", failure);
    fireEvent.click(await screen.findByRole("button", { name: /Chăm sóc/ }));
    fireEvent.click(await screen.findByRole("checkbox", { name: "Hoàn thành Bón phân" }));
    expect(calls).toHaveLength(0);
    fireEvent.click(screen.getByRole("button", { name: "Hủy" }));
    expect(calls).toHaveLength(0);
    expect(screen.queryByRole("region", { name: "Xác nhận hoàn thành" })).toBeNull();
    fireEvent.click(screen.getByRole("checkbox", { name: "Hoàn thành Bón phân" }));
    fireEvent.click(screen.getByRole("button", { name: "Xác nhận" }));
    await waitFor(() => expect(calls).toHaveLength(1));
    await waitFor(() => expect(screen.getByRole("button", { name: "Xác nhận" })).not.toBeDisabled());
    fireEvent.click(screen.getByRole("button", { name: "Xác nhận" }));
    await screen.findByText("100.00%");
    expect(calls).toHaveLength(2);
    expect(calls[1]).toEqual(calls[0]);
    expect(calls[0]?.body).toEqual({ step_seq: 1, expected_season_event_id: "event", gps: { source: "denied" }, channel: "web" });
    expect(screen.getByText("Xong đúng hạn")).toBeTruthy();
    expect(screen.getByText(/Thiếu GPS/)).toBeTruthy();
    expect(screen.queryByRole("checkbox")).toBeNull();
  });
  it("Đại diện không có ô tích dù dữ liệu cũ còn cờ cho ghi", async () => {
    const calls = setup("htx");
    fireEvent.click(await screen.findByRole("button", { name: /Chăm sóc/ }));
    await screen.findByText("Bón phân");
    expect(screen.queryByRole("checkbox")).toBeNull();
    expect(calls).toHaveLength(0);
  });
});
