import { describe, expect, it } from "vitest";
import { createApiClient, unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { htxCoop } from "@/api/mock/data";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { scaledArea } from "@/api/mock/household-season-state";
import { mockToken } from "@/api/mock/personas";
import { harvestErrors, isZeroTonnes } from "./HarvestDeclarationForm";
import { actualVsExpected } from "./YieldEstimatePanel";

describe("AH-100 harvestErrors", () => {
  it("kiểm số tấn và ngày thu hoạch trong khoảng mốc gốc – hôm nay", () => {
    expect(harvestErrors({ mass: "9,75", date: "2026-09-20" }, "2026-06-01", "2026-11-25")).toEqual([]);
    expect(harvestErrors({ mass: "-1", date: "2026-09-20" }, "2026-06-01", "2026-11-25")).toHaveLength(1);
    expect(harvestErrors({ mass: "1.234", date: "2026-09-20" }, "2026-06-01", "2026-11-25")).toHaveLength(1);
    expect(harvestErrors({ mass: "9", date: "2026-05-31" }, "2026-06-01", "2026-11-25")[0]).toContain("mốc gốc");
    expect(harvestErrors({ mass: "9", date: "2026-11-26" }, "2026-06-01", "2026-11-25")).toHaveLength(1);
    expect(isZeroTonnes("0")).toBe(true);
    expect(isZeroTonnes("0,00")).toBe(true);
    expect(isZeroTonnes("0.5")).toBe(false);
  });
  it("chênh lệch thực tế − dự kiến bằng decimal", () => {
    expect(actualVsExpected("9.75", "9.00")).toBe("+0,75");
    expect(actualVsExpected("8.10", "9.00")).toBe("-0,90");
    expect(actualVsExpected("9.00", "9.00")).toBe("0,00");
  });
});

describe("AH-100 mock khai báo sản lượng", () => {
  it("khai báo: vụ Đã thu hoạch, việc Thu hoạch tự Xong, giải phóng diện tích, 0 cần xác nhận, Đại diện bị từ chối", async () => {
    const client = (persona: string) => createApiClient({ baseUrl: "https://api.test", getAccessToken: () => mockToken(persona), fetch: mockFetchNoDelay });
    const api = client("htx-member"), coop = htxCoop().api.ref;
    const remaining = async (ref: string) => (await unwrap(api.GET("/v1/cooperatives/{ref}/households", { params: { path: { ref: coop }, query: { limit: 200 } } })))
      .items.find((h) => h.household_ref === ref)!.remaining_area_ha!;
    const households = await unwrap(api.GET("/v1/cooperatives/{ref}/households", { params: { path: { ref: coop }, query: { limit: 200 } } }));
    const h = households.items.find((h) => h.can_open_season && scaledArea(h.remaining_area_ha ?? "0") >= 100n)!;
    const today = new Intl.DateTimeFormat("en-CA", { timeZone: "Asia/Ho_Chi_Minh" }).format(new Date());
    const before = await remaining(h.household_ref);
    const opened = await unwrap(api.POST("/v1/cooperatives/{ref}/households/{household_ref}/seasons", { params: { path: { ref: coop, household_ref: h.household_ref }, header: { "Idempotency-Key": newIdempotencyKey() } },
      body: { crop_code: "LUA", season_code: "DX", season_year: 2035, variety_code: "OM5451", area_ha: "0.01", anchor_date: today, channel: "web", gps: { source: "denied" } } }));
    const path = { ref: coop, household_ref: h.household_ref, season_ref: opened.season.season_ref };
    const url = "/v1/cooperatives/{ref}/households/{household_ref}/seasons/{season_ref}/harvest" as const;
    const body = { expected_season_event_id: opened.season.current_event_id, mass_t: "0.06", harvest_date: today, gps: { source: "denied" as const }, channel: "web" as const };
    const denied = await client("htx").POST(url, { params: { path, header: { "Idempotency-Key": newIdempotencyKey() } }, body });
    expect(denied.response.status).toBe(403);
    const zero = await api.POST(url, { params: { path, header: { "Idempotency-Key": newIdempotencyKey() } }, body: { ...body, mass_t: "0" } });
    expect(zero.response.status).toBe(409);
    const declared = await unwrap(api.POST(url, { params: { path, header: { "Idempotency-Key": newIdempotencyKey() } }, body }));
    expect(declared.season.status).toBe("completed");
    expect(declared.harvest.mass_t).toBe("0.06");
    expect(declared.auto_completed_steps.length).toBeGreaterThan(0);
    expect(await remaining(h.household_ref)).toBe(before);
    const again = await api.POST(url, { params: { path, header: { "Idempotency-Key": newIdempotencyKey() } }, body: { ...body, expected_season_event_id: declared.season.current_event_id } });
    expect(again.response.status).toBe(409);
  });
});
