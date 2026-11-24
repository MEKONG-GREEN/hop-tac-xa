import { describe, expect, it } from "vitest";
import { createApiClient, unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { htxCoop } from "@/api/mock/data";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { scaledArea } from "@/api/mock/household-season-state";
import { mockToken } from "@/api/mock/personas";
import { stopReasonError } from "./StopSeasonForm";

describe("AH-70 stopReasonError", () => {
  it("bắt buộc lý do 10–500 ký tự", () => {
    expect(stopReasonError("  ngắn  ")).toBe("Nhập lý do dừng vụ (ít nhất 10 ký tự).");
    expect(stopReasonError("x".repeat(501))).toBe("Lý do tối đa 500 ký tự.");
    expect(stopReasonError("Ngập úng mất trắng")).toBeNull();
  });
});

describe("AH-70 mock dừng vụ", () => {
  it("dừng vụ đang canh tác: trạng thái Đã dừng vụ, giải phóng diện tích, phát lại, Đại diện bị từ chối", async () => {
    const client = (persona: string) => createApiClient({ baseUrl: "https://api.test", getAccessToken: () => mockToken(persona), fetch: mockFetchNoDelay });
    const api = client("htx-member"), coop = htxCoop().api.ref;
    const remaining = async (ref: string) => (await unwrap(api.GET("/v1/cooperatives/{ref}/households", { params: { path: { ref: coop }, query: { limit: 200 } } })))
      .items.find((h) => h.household_ref === ref)!.remaining_area_ha!;
    const households = await unwrap(api.GET("/v1/cooperatives/{ref}/households", { params: { path: { ref: coop }, query: { limit: 200 } } }));
    const h = households.items.find((h) => h.can_open_season && scaledArea(h.remaining_area_ha ?? "0") >= 100n)!;
    const today = new Intl.DateTimeFormat("en-CA", { timeZone: "Asia/Ho_Chi_Minh" }).format(new Date());
    const before = await remaining(h.household_ref);
    const opened = await unwrap(api.POST("/v1/cooperatives/{ref}/households/{household_ref}/seasons", { params: { path: { ref: coop, household_ref: h.household_ref }, header: { "Idempotency-Key": newIdempotencyKey() } },
      body: { crop_code: "LUA", season_code: "DX", season_year: 2033, variety_code: "OM5451", area_ha: "0.01", anchor_date: today, channel: "web", gps: { source: "denied" } } }));
    expect(scaledArea(await remaining(h.household_ref))).toBe(scaledArea(before) - 100n);
    const path = { ref: coop, household_ref: h.household_ref, season_ref: opened.season.season_ref };
    const body = { expected_season_event_id: opened.season.current_event_id, reason: "Ngập úng mất trắng", channel: "web" as const };
    const denied = await client("htx").POST("/v1/cooperatives/{ref}/households/{household_ref}/seasons/{season_ref}/stop", { params: { path, header: { "Idempotency-Key": newIdempotencyKey() } }, body });
    expect(denied.response.status).toBe(403);
    const short = await api.POST("/v1/cooperatives/{ref}/households/{household_ref}/seasons/{season_ref}/stop", { params: { path, header: { "Idempotency-Key": newIdempotencyKey() } }, body: { ...body, reason: "ngắn" } });
    expect(short.response.status).toBe(422);
    const key = newIdempotencyKey();
    const stopped = await unwrap(api.POST("/v1/cooperatives/{ref}/households/{household_ref}/seasons/{season_ref}/stop", { params: { path, header: { "Idempotency-Key": key } }, body }));
    expect(stopped.status).toBe("cancelled");
    expect(stopped.stop_reason).toBe("Ngập úng mất trắng");
    expect(await remaining(h.household_ref)).toBe(before);
    const again = await unwrap(api.POST("/v1/cooperatives/{ref}/households/{household_ref}/seasons/{season_ref}/stop", { params: { path, header: { "Idempotency-Key": key } }, body }));
    expect(again.current_event_id).toBe(stopped.current_event_id);
    const twice = await api.POST("/v1/cooperatives/{ref}/households/{household_ref}/seasons/{season_ref}/stop", { params: { path, header: { "Idempotency-Key": newIdempotencyKey() } }, body: { ...body, expected_season_event_id: stopped.current_event_id } });
    expect(twice.response.status).toBe(409);
  });
});
