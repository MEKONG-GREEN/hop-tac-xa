import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { createApiClient } from "@/api/client";
import { MOCK_PERSONAS } from "@/api/mock/personas";
import { AuthContextForTests, type AuthContextValue } from "@/auth/AuthProvider";
import { buildPrincipal } from "@/auth/principal";
import { HouseholdChecklist } from "./HouseholdChecklist";

// AH-124: chi tiết vụ — % tiến độ, số việc đến hạn/trễ, giai đoạn mặc định đóng, nút sang Kế hoạch mùa vụ.
describe("AH-124 chi tiết vụ", () => {
  it("hiện tiến độ, đến hạn, trễ; giai đoạn đóng sẵn và mở được; nút Kế hoạch mùa vụ", async () => {
    const p = MOCK_PERSONAS.find((x) => x.id === "htx")!;
    const principal = buildPrincipal({ sub: p.me.actor_ref, amr: p.amr, realm_access: { roles: p.me.roles } }, p.me);
    const task = (seq: number, name: string, status: string) => ({ seq, stage_seq: 1, task_seq: seq, name, status, completion_status: status, undated: false,
      from_date: "2026-11-20", to_date: "2026-11-22", done_on: null, can_complete: false });
    const checklist = { season_ref: "season", current_event_id: "event", records: [], progress: { as_of_date: "2026-11-24", done: 1, skipped: 1, total: 4, due: 1, overdue: 1, percent: "33.33",
      stages: [{ seq: 1, name: "Chăm sóc", status: "in_progress", tasks: [task(1, "Bón thúc", "due"), task(2, "Làm cỏ", "overdue"), task(3, "Tỉa dặm", "skipped"),
        { ...task(4, "Bơm nước", "done_late"), done_on: "2026-11-23" }] }] } };
    const api = createApiClient({ baseUrl: "https://api.test", getAccessToken: () => "test", fetch: async () => Response.json(checklist) });
    const openPlan = vi.fn();
    render(<AuthContextForTests.Provider value={{ status: "authenticated", api, principal } as AuthContextValue}>
      <HouseholdChecklist coopRef="coop" householdRef="household" seasonRef="season" onOpenPlan={openPlan} />
    </AuthContextForTests.Provider>);
    expect(await screen.findByText("33.33%")).toBeTruthy();
    expect(screen.getByText(/Đến hạn:/).textContent).toContain("Đến hạn: 1 · Trễ: 1");
    const stage = screen.getByRole("button", { name: /Chăm sóc · Đang thực hiện · 4 việc/ });
    expect(stage.getAttribute("aria-expanded")).toBe("false");
    expect(screen.queryByText("Bón thúc")).toBeNull();
    fireEvent.click(stage);
    expect(stage.getAttribute("aria-expanded")).toBe("true");
    for (const [name, label] of [["Bón thúc", "Đến hạn"], ["Làm cỏ", "Trễ"], ["Tỉa dặm", "Bỏ qua"], ["Bơm nước", "Xong trễ"]] as const) {
      const row = screen.getByText(name).closest("li")!;
      expect(row.textContent).toContain(label);
    }
    fireEvent.click(screen.getByRole("button", { name: "Kế hoạch mùa vụ" }));
    expect(openPlan).toHaveBeenCalledOnce();
  });
});
