import { describe, expect, it } from "vitest";
import { createApiClient, unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { htxCoop } from "@/api/mock/data";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { scaledArea } from "@/api/mock/household-season-state";
import { tonnes } from "@/api/mock/household-yield-estimate";
import { mockToken } from "@/api/mock/personas";
import { Decimal } from "@/lib/exactDecimal";
import { tonnesInputError } from "./YieldEstimatePanel";

describe("AH-87 tonnesInputError", () => {
  it("ô trống hợp lệ, số âm/quá 2 chữ số lẻ bị chặn, nhận dấu phẩy thập phân", () => {
    expect(tonnesInputError("")).toBeNull();
    expect(tonnesInputError("6,5")).toBeNull();
    expect(tonnesInputError("12.25")).toBeNull();
    expect(tonnesInputError("-1")).not.toBeNull();
    expect(tonnesInputError("1.234")).not.toBeNull();
    expect(tonnesInputError("abc")).not.toBeNull();
  });
  it("làm tròn half-up về 2 chữ số", () => {
    expect(tonnes(Decimal.parse("2.345")!)).toBe("2.35");
    expect(tonnes(Decimal.parse("0.005")!)).toBe("0.01");
    expect(tonnes(Decimal.parse("0.0049")!)).toBe("0.00");
  });
});

describe("AH-87 mock sản lượng dự kiến", () => {
  it("mở vụ tính lúa/rơm, Thành viên nhập lúa thì rơm tính lại, Đại diện bị từ chối, phiên bản cũ 409, phát lại", async () => {
    const client = (persona: string) => createApiClient({ baseUrl: "https://api.test", getAccessToken: () => mockToken(persona), fetch: mockFetchNoDelay });
    const api = client("htx-member"), coop = htxCoop().api.ref;
    const households = await unwrap(api.GET("/v1/cooperatives/{ref}/households", { params: { path: { ref: coop }, query: { limit: 200 } } }));
    const h = households.items.find((h) => h.can_open_season && scaledArea(h.remaining_area_ha ?? "0") >= 10000n)!;
    const today = new Intl.DateTimeFormat("en-CA", { timeZone: "Asia/Ho_Chi_Minh" }).format(new Date());
    const opened = await unwrap(api.POST("/v1/cooperatives/{ref}/households/{household_ref}/seasons", { params: { path: { ref: coop, household_ref: h.household_ref }, header: { "Idempotency-Key": newIdempotencyKey() } },
      body: { crop_code: "LUA", season_code: "DX", season_year: 2034, variety_code: "OM5451", area_ha: "0.25", anchor_date: today, channel: "web", gps: { source: "denied" } } }));
    const e = opened.season.yield_estimate!;
    // 0,25 ha × 6 t/ha = 1,50 t; rơm 35 % = 0,525 → 0,53.
    expect([e.seq, e.is_rice, e.computed_rice_t, e.computed_straw_t, e.rice_t, e.straw_t]).toEqual([1, true, "1.50", "0.53", "1.50", "0.53"]);
    const path = { ref: coop, household_ref: h.household_ref, season_ref: opened.season.season_ref };
    const url = "/v1/cooperatives/{ref}/households/{household_ref}/seasons/{season_ref}/yield-estimates" as const;
    const denied = await client("htx").POST(url, { params: { path, header: { "Idempotency-Key": newIdempotencyKey() } }, body: { expected_seq: 1, rice_t: "2" } });
    expect(denied.response.status).toBe(403);
    const bad = await api.POST(url, { params: { path, header: { "Idempotency-Key": newIdempotencyKey() } }, body: { expected_seq: 1, rice_t: "1.234" } });
    expect(bad.response.status).toBe(422);
    const key = newIdempotencyKey();
    const saved = await unwrap(api.POST(url, { params: { path, header: { "Idempotency-Key": key } }, body: { expected_seq: 1, rice_t: "2", straw_t: null } }));
    expect([saved.yield_estimate.seq, saved.yield_estimate.rice_t, saved.yield_estimate.computed_straw_t, saved.yield_estimate.straw_t]).toEqual([2, "2.00", "0.70", "0.70"]);
    const again = await unwrap(api.POST(url, { params: { path, header: { "Idempotency-Key": key } }, body: { expected_seq: 1, rice_t: "2", straw_t: null } }));
    expect(again.yield_estimate.seq).toBe(2);
    const stale = await api.POST(url, { params: { path, header: { "Idempotency-Key": newIdempotencyKey() } }, body: { expected_seq: 1, rice_t: "3" } });
    expect(stale.response.status).toBe(409);
  });
});
