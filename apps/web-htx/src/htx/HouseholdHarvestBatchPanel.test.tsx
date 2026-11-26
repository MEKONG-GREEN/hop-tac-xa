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
import { draftError, errorCsv, HouseholdHarvestBatchPanel, normalizeTonnes } from "./HouseholdHarvestBatchPanel";

describe("AH-177 kiểm sớm dòng", () => {
  it("tấn, ngày, xác nhận 0", () => {
    expect(normalizeTonnes("9,75")).toBe("9.75");
    expect(normalizeTonnes("-1")).toBeNull();
    expect(normalizeTonnes("1.234")).toBeNull();
    expect(draftError({ mass: "", date: "2026-09-20", zero: false }, "2026-09-29")).toContain("Nhập sản lượng");
    expect(draftError({ mass: "3", date: "", zero: false }, "2026-09-29")).toContain("ngày");
    expect(draftError({ mass: "3", date: "2026-10-01", zero: false }, "2026-09-29")).toContain("sau hôm nay");
    expect(draftError({ mass: "0", date: "2026-09-20", zero: false }, "2026-09-29")).toContain("xác nhận");
    expect(draftError({ mass: "0", date: "2026-09-20", zero: true }, "2026-09-29")).toBeNull();
  });
  it("CSV danh sách lỗi chỉ gồm dòng bị bỏ qua", () => {
    const csv = errorCsv({ accepted: 1, duplicate: 0, rejected: 1, rows: [
      { line: 2, status: "accepted", season_ref: "s1", code: null, reason: null, needs_confirm: false, harvest_id: "h" },
      { line: 3, status: "rejected", season_ref: "s2", code: "x", reason: "Thiếu sản lượng (tấn)", needs_confirm: false, harvest_id: null },
    ] }, () => "Hộ A · DX 2026");
    expect(csv).toContain("3,s2,Hộ A · DX 2026,Thiếu sản lượng (tấn)");
    expect(csv).not.toContain("s1");
  });
});

describe("AH-177 bảng nhập nhanh", () => {
  it("gửi các dòng đã nhập, tô dòng lỗi giữ số, công tắc KH-20, giữ key khi thử lại cùng nội dung", async () => {
    const posts: { key: string | null; body: { rows: { season_ref: string; mass_t: string }[] } }[] = [];
    const queries: string[] = [];
    const item = (ref: string) => ({ season_ref: ref, household_ref: "h1", crop_code: "LUA", season_code: "DX", season_year: 2026, variety_code: "OM5451", area_ha: "0.50", status: "open",
      opened_by: "m1", expected_harvest_date: "2026-09-09", overdue: true, harvest_recorded: true, current_event_id: `ev-${ref}`, expected_t: "3.00", expected_straw_t: null, expected_source: "computed",
      actual_t: null, harvest_date: null, yield_t_per_ha: null, zero_confirmed: false, diff_t: null, diff_pct: null });
    let fail = true;
    const api = createApiClient({ baseUrl: "https://api.test", getAccessToken: () => "t", fetch: async (input) => {
      const req = input as Request;
      const url = new URL(req.url);
      if (url.pathname.endsWith("/harvest-batches")) {
        posts.push({ key: req.headers.get("Idempotency-Key"), body: await req.json() });
        if (fail) { fail = false; return new Response(null, { status: 503 }); }
        return Response.json({ accepted: 1, duplicate: 0, rejected: 1, rows: [
          { line: 1, status: "accepted", season_ref: "s1", code: null, reason: null, needs_confirm: false, harvest_id: "h1" },
          { line: 2, status: "rejected", season_ref: "s2", code: "cropcycle.season_state", reason: "Vụ không còn \"Đang canh tác\"", needs_confirm: false, harvest_id: null },
        ] });
      }
      queries.push(url.search);
      return Response.json({ items: [item("s1"), item("s2"), item("s3")], next_cursor: null, totals: { pending: 3, declared: 0, declared_without_estimate: 0, pending_expected_t: "9.00", expected_t: "0.00", actual_t: "0.00", diff_t: "0.00", diff_pct: null } });
    } });
    const p = MOCK_PERSONAS.find((x) => x.id === "htx-member")!;
    const principal = buildPrincipal({ sub: p.me.actor_ref, amr: p.amr, realm_access: { roles: p.me.roles } }, p.me);
    render(<AuthContextForTests.Provider value={{ status: "authenticated", api, principal } as AuthContextValue}>
      <HouseholdHarvestBatchPanel coopRef="coop" householdLabel={() => "Hộ A"} />
    </AuthContextForTests.Provider>);
    const inputs = await screen.findAllByLabelText(/Sản lượng tấn Hộ A/);
    expect(queries[0]).toContain("harvest_recorded_only=true");
    const dates = screen.getAllByLabelText(/Ngày thu hoạch cuối Hộ A/);
    fireEvent.change(inputs[0]!, { target: { value: "3,5" } });
    fireEvent.change(dates[0]!, { target: { value: "2026-09-20" } });
    fireEvent.change(inputs[1]!, { target: { value: "2" } });
    fireEvent.change(dates[1]!, { target: { value: "2026-09-20" } });
    fireEvent.click(screen.getByRole("button", { name: "Lưu tất cả" }));
    await waitFor(() => expect(posts.length).toBe(1));
    expect(posts[0]!.body.rows.map((r) => [r.season_ref, r.mass_t])).toEqual([["s1", "3.5"], ["s2", "2"]]);
    await screen.findByRole("alert");
    fireEvent.click(screen.getByRole("button", { name: "Lưu tất cả" }));
    await waitFor(() => expect(posts.length).toBe(2));
    expect(posts[1]!.key).toBe(posts[0]!.key);
    expect((await screen.findByText(/Vụ không còn/)).closest("tr")!.querySelector("input")!.value).toBe("2");
    expect(screen.getByRole("status").textContent).toContain("1 vụ đã lưu · 1 dòng bị bỏ qua");
    fireEvent.click(screen.getByLabelText(/Hiện mọi vụ đang canh tác/));
    await waitFor(() => expect(queries.at(-1)).toContain("harvest_recorded_only=false"));
  });
});

describe("AH-177 mock", () => {
  it("lô có dòng hợp lệ và dòng lỗi; nhập CSV; Đại diện bị từ chối", async () => {
    const client = (persona: string) => createApiClient({ baseUrl: "https://api.test", getAccessToken: () => mockToken(persona), fetch: mockFetchNoDelay });
    const api = client("htx-member"), coop = htxCoop().api.ref;
    const households = await unwrap(api.GET("/v1/cooperatives/{ref}/households", { params: { path: { ref: coop }, query: { limit: 200 } } }));
    const h = households.items.find((h) => h.can_open_season && scaledArea(h.remaining_area_ha ?? "0") >= 200n)!;
    const today = new Intl.DateTimeFormat("en-CA", { timeZone: "Asia/Ho_Chi_Minh" }).format(new Date());
    const open = async () => unwrap(api.POST("/v1/cooperatives/{ref}/households/{household_ref}/seasons", { params: { path: { ref: coop, household_ref: h.household_ref }, header: { "Idempotency-Key": newIdempotencyKey() } },
      body: { crop_code: "LUA", season_code: "DX", season_year: 2039, variety_code: "OM5451", area_ha: "0.01", anchor_date: today, channel: "web", gps: { source: "denied" } } }));
    const a = await open(), b = await open();
    const url = "/v1/cooperatives/{ref}/harvest-batches" as const;
    const denied = await client("htx").POST(url, { params: { path: { ref: coop }, header: { "Idempotency-Key": newIdempotencyKey() } }, body: { rows: [{ season_ref: a.season.season_ref, mass_t: "1", harvest_date: today }] } });
    expect(denied.response.status).toBe(403);
    const res = await unwrap(api.POST(url, { params: { path: { ref: coop }, header: { "Idempotency-Key": newIdempotencyKey() } }, body: { rows: [
      { line: 1, season_ref: a.season.season_ref, mass_t: "0.05", harvest_date: today },
      { line: 2, season_ref: b.season.season_ref, mass_t: "-1", harvest_date: today },
    ] } }));
    expect([res.accepted, res.rejected]).toEqual([1, 1]);
    expect(res.rows[1]?.status).toBe("rejected");
    const csv = `Mã vụ,Sản lượng (tấn),Ngày thu hoạch cuối,Xác nhận 0\n${b.season.season_ref},0,${today},x\n${a.season.season_ref},1,${today},\n`;
    const imp = await unwrap(api.POST("/v1/cooperatives/{ref}/harvest-imports", { params: { path: { ref: coop }, header: { "Idempotency-Key": newIdempotencyKey() } },
      body: csv as unknown as string, bodySerializer: (x: unknown) => x as BodyInit, headers: { "Content-Type": "text/csv" } }));
    expect([imp.accepted, imp.rejected]).toEqual([1, 1]);
  });
});
