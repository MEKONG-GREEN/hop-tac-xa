import { describe, expect, it } from "vitest";
import { createApiClient, unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { htxCoop } from "@/api/mock/data";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { scaledArea } from "@/api/mock/household-season-state";
import { mockToken } from "@/api/mock/personas";
import { costError, normalizeCost, noteError, taskLine } from "./SeasonSummaryPanel";

describe("AH-152 kiểm chi phí, ghi chú và dòng số việc", () => {
  it("chi phí tùy chọn, số nguyên VND tối đa 12 chữ số", () => {
    expect(costError("")).toBeNull();
    expect(costError("15.000.000")).toBeNull();
    expect(normalizeCost("15.000.000")).toBe("15000000");
    expect(costError("-1")).not.toBeNull();
    expect(costError("1234567890123")).not.toBeNull();
    expect(costError("abc")).not.toBeNull();
  });
  it("ghi chú không chứa SĐT/CCCD, tối đa 1000 ký tự", () => {
    expect(noteError("")).toBeNull();
    expect(noteError("Vụ được mùa")).toBeNull();
    expect(noteError("Gọi 0912345678")).not.toBeNull();
    expect(noteError("a".repeat(1001))).not.toBeNull();
  });
  it("hiện riêng sớm, đúng hạn, trễ, bỏ qua, chưa làm", () => {
    expect(taskLine({ early: 1, on_time: 2, late: 3, skipped: 1, not_done: 0, total: 7 })).toBe("Sớm 1 · Đúng hạn 2 · Trễ 3 · Bỏ qua 1 · Chưa làm 0 (tổng 7 việc)");
  });
});

describe("AH-152 mock tổng kết và hoàn thành vụ", () => {
  it("chặn khi chưa khai báo, hoàn thành vụ kèm chi phí, bản chụp cố định, sửa cần lý do, không hủy", async () => {
    const client = (persona: string) => createApiClient({ baseUrl: "https://api.test", getAccessToken: () => mockToken(persona), fetch: mockFetchNoDelay });
    const api = client("htx-member"), coop = htxCoop().api.ref;
    const households = await unwrap(api.GET("/v1/cooperatives/{ref}/households", { params: { path: { ref: coop }, query: { limit: 200 } } }));
    const h = households.items.find((h) => h.can_open_season && scaledArea(h.remaining_area_ha ?? "0") >= 100n)!;
    const today = new Intl.DateTimeFormat("en-CA", { timeZone: "Asia/Ho_Chi_Minh" }).format(new Date());
    const opened = await unwrap(api.POST("/v1/cooperatives/{ref}/households/{household_ref}/seasons", { params: { path: { ref: coop, household_ref: h.household_ref }, header: { "Idempotency-Key": newIdempotencyKey() } },
      body: { crop_code: "LUA", season_code: "DX", season_year: 2037, variety_code: "OM5451", area_ha: "0.01", anchor_date: today, channel: "web", gps: { source: "denied" } } }));
    const path = { ref: coop, household_ref: h.household_ref, season_ref: opened.season.season_ref };
    const closeUrl = "/v1/cooperatives/{ref}/households/{household_ref}/seasons/{season_ref}/close" as const;
    const summaryUrl = "/v1/cooperatives/{ref}/households/{household_ref}/seasons/{season_ref}/summary" as const;
    const early = await api.POST(closeUrl, { params: { path, header: { "Idempotency-Key": newIdempotencyKey() } }, body: { expected_season_event_id: opened.season.current_event_id } });
    expect(early.response.status).toBe(409);
    expect((await unwrap(api.GET(summaryUrl, { params: { path } }))).blockers).toContain("harvest_required");
    const declared = await unwrap(api.POST("/v1/cooperatives/{ref}/households/{household_ref}/seasons/{season_ref}/harvest", { params: { path, header: { "Idempotency-Key": newIdempotencyKey() } },
      body: { expected_season_event_id: opened.season.current_event_id, mass_t: "0.06", harvest_date: today, gps: { source: "denied" }, channel: "web" } }));
    const before = await unwrap(api.GET(summaryUrl, { params: { path } }));
    expect(before.mass_t).toBe("0.06");
    const denied = await client("htx").POST(closeUrl, { params: { path, header: { "Idempotency-Key": newIdempotencyKey() } }, body: { expected_season_event_id: declared.season.current_event_id } });
    expect(denied.response.status).toBe(403);
    const badCost = await api.POST(closeUrl, { params: { path, header: { "Idempotency-Key": newIdempotencyKey() } }, body: { expected_season_event_id: declared.season.current_event_id, cost_vnd: "-1" } });
    expect(badCost.response.status).toBe(422);
    expect(before.can_close).toBe(true);
    const closed = await unwrap(api.POST(closeUrl, { params: { path, header: { "Idempotency-Key": newIdempotencyKey() } }, body: { expected_season_event_id: declared.season.current_event_id, cost_vnd: "15000000", note: "Vụ được mùa" } }));
    expect(closed.season.status).toBe("closed");
    expect(closed.summary.cost_vnd).toBe("15000000");
    const editUrl = "/v1/cooperatives/{ref}/households/{household_ref}/seasons/{season_ref}/harvest/corrections" as const;
    const noReason = await api.POST(editUrl, { params: { path, header: { "Idempotency-Key": newIdempotencyKey() } }, body: { expected_harvest_id: declared.harvest.harvest_id, mass_t: "0.07", harvest_date: today } });
    expect(noReason.response.status).toBe(422);
    await unwrap(api.POST(editUrl, { params: { path, header: { "Idempotency-Key": newIdempotencyKey() } }, body: { expected_harvest_id: declared.harvest.harvest_id, mass_t: "0.07", harvest_date: today, reason: "Cân lại tại kho của HTX" } }));
    expect((await unwrap(api.GET(summaryUrl, { params: { path } }))).mass_t).toBe("0.06");
  });
});
