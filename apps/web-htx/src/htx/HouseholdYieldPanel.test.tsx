import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { createApiClient, unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { htxCoop } from "@/api/mock/data";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { scaledArea } from "@/api/mock/household-season-state";
import { MOCK_PERSONAS, mockToken } from "@/api/mock/personas";
import { AuthContextForTests, type AuthContextValue } from "@/auth/AuthProvider";
import { buildPrincipal } from "@/auth/principal";
import { diffText, HouseholdYieldPanel, tonnesText } from "./HouseholdYieldPanel";

describe("AH-153 định dạng", () => {
  it("tấn và chênh lệch có dấu", () => {
    expect(tonnesText(null)).toBe("—");
    expect(tonnesText("8.4")).toBe("8,40 tấn");
    expect(diffText("0.90", "12.0")).toBe("+0,90 tấn (+12,0%)");
    expect(diffText("-1.00", "-25.0")).toBe("−1,00 tấn (−25,0%)");
    expect(diffText("0.00", null)).toBe("0,00 tấn");
    expect(diffText(null, null)).toBe("—");
  });
});

describe("AH-153 bảng thực tế so với dự kiến", () => {
  it("chỉ đọc, hai nhóm, tổng hợp, lọc gửi tham số", async () => {
    const queries: string[] = [];
    const base = { household_ref: "h1", crop_code: "LUA", season_code: "DX", season_year: 2026, variety_code: "OM5451", area_ha: "0.50", opened_by: "m1", opened_by_label: "Thành viên Một",
      expected_straw_t: null, yield_t_per_ha: null, zero_confirmed: false, harvest_date: null, actual_t: null, diff_t: null, diff_pct: null };
    const pending = { ...base, season_ref: "s1", status: "open", expected_harvest_date: "2026-09-09", overdue: true, expected_t: "3.00", expected_source: "computed" };
    const declared = { ...base, season_ref: "s2", status: "closed", expected_harvest_date: "2026-09-12", overdue: false, expected_t: "4.00", expected_source: "entered",
      actual_t: "3.00", harvest_date: "2026-09-20", yield_t_per_ha: "6.00", diff_t: "-1.00", diff_pct: "-25.0" };
    const totals = { pending: 1, declared: 1, declared_without_estimate: 0, pending_expected_t: "3.00", expected_t: "4.00", actual_t: "3.00", diff_t: "-1.00", diff_pct: "-25.0" };
    const api = createApiClient({ baseUrl: "https://api.test", getAccessToken: () => "t", fetch: async (input) => {
      const url = new URL((input as Request).url);
      if (url.pathname.endsWith("/season-calendar-recipients")) return Response.json({ items: [{ actor_ref: "m1", label: "Thành viên Một" }], next_offset: 0 });
      queries.push(url.search);
      return Response.json({ items: url.searchParams.get("state") === "declared" ? [declared] : [pending], next_cursor: null, totals });
    } });
    const p = MOCK_PERSONAS.find((x) => x.id === "htx")!;
    const principal = buildPrincipal({ sub: p.me.actor_ref, amr: p.amr, realm_access: { roles: p.me.roles } }, p.me);
    render(<AuthContextForTests.Provider value={{ status: "authenticated", api, principal } as AuthContextValue}>
      <HouseholdYieldPanel coopRef="coop" householdLabel={() => "Nguyễn V. A."} />
    </AuthContextForTests.Provider>);
    const row = (await screen.findByText("Nguyễn V. A.")).closest("tr")!;
    expect(row.textContent).toContain("09/09/2026");
    expect(row.textContent).toContain("Quá ngày dự kiến");
    expect(row.textContent).toContain("3,00 tấn");
    expect(screen.getByLabelText("Tổng hợp").textContent).toContain("Dự kiến của 1 vụ chờ khai báo: 3,00 tấn");
    expect(screen.queryByRole("button", { name: /Khai báo sản lượng|Sửa|Hủy/ })).toBeNull();
    expect(queries[0]).toContain("state=pending");
    fireEvent.click(screen.getByRole("tab", { name: /Đã khai báo/ }));
    await waitFor(() => expect(queries.at(-1)).toContain("state=declared"));
    const row2 = (await screen.findByText("Đã hoàn thành vụ")).closest("tr")!;
    expect(row2.textContent).toContain("−1,00 tấn (−25,0%)");
    expect(row2.textContent).toContain("(TV nhập)");
    expect(screen.getByLabelText("Tổng hợp").textContent).toContain("chênh lệch −1,00 tấn (−25,0%)");
    await screen.findByRole("option", { name: "Thành viên Một" });
    fireEvent.change(screen.getByLabelText("Thành viên mở vụ"), { target: { value: "m1" } });
    await waitFor(() => expect(queries.at(-1)).toContain("opened_by=m1"));
    fireEvent.change(screen.getByLabelText("Năm"), { target: { value: "2026" } });
    await waitFor(() => expect(queries.at(-1)).toContain("season_year=2026"));
  });
});

describe("AH-153 mock", () => {
  it("vụ mới vào nhóm chờ, khai báo chuyển sang đã khai báo; HTX khác 404", async () => {
    const client = (persona: string) => createApiClient({ baseUrl: "https://api.test", getAccessToken: () => mockToken(persona), fetch: mockFetchNoDelay });
    const api = client("htx-member"), coop = htxCoop().api.ref;
    const households = await unwrap(api.GET("/v1/cooperatives/{ref}/households", { params: { path: { ref: coop }, query: { limit: 200 } } }));
    const h = households.items.find((h) => h.can_open_season && scaledArea(h.remaining_area_ha ?? "0") >= 100n)!;
    const today = new Intl.DateTimeFormat("en-CA", { timeZone: "Asia/Ho_Chi_Minh" }).format(new Date());
    const opened = await unwrap(api.POST("/v1/cooperatives/{ref}/households/{household_ref}/seasons", { params: { path: { ref: coop, household_ref: h.household_ref }, header: { "Idempotency-Key": newIdempotencyKey() } },
      body: { crop_code: "LUA", season_code: "HT", season_year: 2038, variety_code: "OM5451", area_ha: "0.01", anchor_date: today, channel: "web", gps: { source: "denied" } } }));
    const url = "/v1/cooperatives/{ref}/household-yields" as const;
    const manager = client("htx");
    const pending = await unwrap(manager.GET(url, { params: { path: { ref: coop }, query: { state: "pending", season_year: 2038 } } }));
    expect(pending.items.map((i) => i.season_ref)).toEqual([opened.season.season_ref]);
    await unwrap(api.POST("/v1/cooperatives/{ref}/households/{household_ref}/seasons/{season_ref}/harvest", { params: { path: { ref: coop, household_ref: h.household_ref, season_ref: opened.season.season_ref }, header: { "Idempotency-Key": newIdempotencyKey() } },
      body: { expected_season_event_id: opened.season.current_event_id, mass_t: "0.06", harvest_date: today, gps: { source: "denied" }, channel: "web" } }));
    const declared = await unwrap(manager.GET(url, { params: { path: { ref: coop }, query: { state: "declared", season_year: 2038 } } }));
    expect(declared.items[0]?.actual_t).toBe("0.06");
    expect(declared.totals.declared).toBe(1);
    const bad = await manager.GET(url, { params: { path: { ref: coop }, query: { state: "bogus" as "pending" } } });
    expect(bad.response.status).toBe(422);
  });
});
