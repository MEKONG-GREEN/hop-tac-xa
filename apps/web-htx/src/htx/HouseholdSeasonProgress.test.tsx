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
import { HouseholdSeasonProgress } from "./HouseholdSeasonProgress";

// AH-140: Đại diện HTX xem chỉ đọc danh sách vụ và tiến độ; lọc trạng thái, Thành viên.
describe("AH-140 tiến độ vụ nông hộ", () => {
  it("bảng chỉ đọc có tiến độ, đến hạn, trễ; lọc gửi tham số", async () => {
    const queries: string[] = [];
    const item = { season_ref: "s1", household_ref: "h1", crop_code: "LUA", season_code: "DX", season_year: 2026, variety_code: "OM5451", area_ha: "0.50", status: "open",
      opened_by: "m1", opened_by_label: "Thành viên Một", opened_at: "2026-11-20T00:00:00Z", progress: { percent: "20.00", done: 1, total: 5, skipped: 0, due: 1, overdue: 2 } };
    const api = createApiClient({ baseUrl: "https://api.test", getAccessToken: () => "t", fetch: async (input) => {
      const url = new URL((input as Request).url);
      if (url.pathname.endsWith("/season-calendar-recipients")) return Response.json({ items: [{ actor_ref: "m1", label: "Thành viên Một" }], next_offset: 0 });
      queries.push(url.search);
      return Response.json({ items: url.searchParams.get("status") === "completed" ? [] : [item], next_cursor: null });
    } });
    const p = MOCK_PERSONAS.find((x) => x.id === "htx")!;
    const principal = buildPrincipal({ sub: p.me.actor_ref, amr: p.amr, realm_access: { roles: p.me.roles } }, p.me);
    render(<AuthContextForTests.Provider value={{ status: "authenticated", api, principal } as AuthContextValue}>
      <HouseholdSeasonProgress coopRef="coop" householdLabel={() => "Nguyễn V. A."} />
    </AuthContextForTests.Provider>);
    const row = (await screen.findByText("Nguyễn V. A.")).closest("tr")!;
    expect(row.textContent).toContain("20.00%");
    expect(row.textContent).toContain("Đang canh tác");
    expect(row.textContent).toContain("Thành viên Một");
    expect([...row.querySelectorAll("td")].map((td) => td.textContent).slice(5, 7)).toEqual(["1", "2"]);
    expect(screen.queryByRole("button", { name: /Dừng|Sửa|Mở vụ/ })).toBeNull();
    await screen.findByRole("option", { name: "Thành viên Một" });
    fireEvent.change(screen.getByLabelText("Thành viên mở vụ"), { target: { value: "m1" } });
    await waitFor(() => expect(queries.at(-1)).toContain("opened_by=m1"));
    fireEvent.change(screen.getByLabelText("Trạng thái vụ"), { target: { value: "completed" } });
    expect(await screen.findByText("Không có vụ phù hợp.")).toBeTruthy();
  });

  it("mock: Đại diện thấy vụ do Thành viên mở kèm tiến độ; HTX khác bị từ chối", async () => {
    const client = (persona: string) => createApiClient({ baseUrl: "https://api.test", getAccessToken: () => mockToken(persona), fetch: mockFetchNoDelay });
    const member = client("htx-member"), coop = htxCoop().api.ref;
    const households = await unwrap(member.GET("/v1/cooperatives/{ref}/households", { params: { path: { ref: coop }, query: { limit: 200 } } }));
    const h = households.items.find((h) => h.can_open_season && scaledArea(h.remaining_area_ha ?? "0") >= 100n)!;
    const today = new Intl.DateTimeFormat("en-CA", { timeZone: "Asia/Ho_Chi_Minh" }).format(new Date());
    const opened = await unwrap(member.POST("/v1/cooperatives/{ref}/households/{household_ref}/seasons", { params: { path: { ref: coop, household_ref: h.household_ref }, header: { "Idempotency-Key": newIdempotencyKey() } },
      body: { crop_code: "LUA", season_code: "DX", season_year: 2034, variety_code: "OM5451", area_ha: "0.01", anchor_date: today, channel: "web", gps: { source: "denied" } } }));
    const page = await unwrap(client("htx").GET("/v1/cooperatives/{ref}/household-seasons", { params: { path: { ref: coop }, query: { limit: 200 } } }));
    const mine = page.items.find((s) => s.season_ref === opened.season.season_ref)!;
    expect(mine.status).toBe("open");
    expect(mine.progress?.total).toBeGreaterThan(0);
    const other = await client("htx").GET("/v1/cooperatives/{ref}/household-seasons", { params: { path: { ref: "00000000-0000-7000-8000-000000000000" } } });
    expect(other.response.status).toBe(404);
  });
});
