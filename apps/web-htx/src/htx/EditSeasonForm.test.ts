import { describe, expect, it } from "vitest";
import { createApiClient, unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { htxCoop } from "@/api/mock/data";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { scaledArea } from "@/api/mock/household-season-state";
import { mockToken } from "@/api/mock/personas";
import { editSeasonErrors } from "./EditSeasonForm";

const season = { anchor_date: "2026-11-20", area_ha: "0.50", variety_code: "OM5451" };

describe("AH-175 editSeasonErrors", () => {
  it("bắt buộc lý do, diện tích hợp lệ, SOP khi đổi giống, có thay đổi", () => {
    const ok = { anchor: "2026-11-22", area: "0.50", variety: "OM5451", sop: "", reason: "Sạ lại do mưa lớn" };
    expect(editSeasonErrors(season, ok)).toEqual([]);
    expect(editSeasonErrors(season, { ...ok, reason: "ngắn" })).toEqual(["Nhập lý do sửa vụ (10–500 ký tự)."]);
    expect(editSeasonErrors(season, { ...ok, area: "0" })).toEqual(["Diện tích phải lớn hơn 0 và có tối đa hai chữ số thập phân."]);
    expect(editSeasonErrors(season, { ...ok, variety: "ST25" })).toEqual(["Đổi giống phải chọn SOP đang áp dụng theo giống mới."]);
    expect(editSeasonErrors(season, { ...ok, anchor: "2026-11-20" })).toEqual(["Chưa có nội dung thay đổi."]);
  });
});

describe("AH-175 mock sửa vụ", () => {
  it("đổi mốc gốc và diện tích, đổi giống cần SOP, có nhật ký thì không đổi giống, Đại diện bị từ chối", async () => {
    const client = (persona: string) => createApiClient({ baseUrl: "https://api.test", getAccessToken: () => mockToken(persona), fetch: mockFetchNoDelay });
    const api = client("htx-member"), coop = htxCoop().api.ref;
    const households = await unwrap(api.GET("/v1/cooperatives/{ref}/households", { params: { path: { ref: coop }, query: { limit: 200 } } }));
    const h = households.items.find((h) => h.can_open_season && scaledArea(h.remaining_area_ha ?? "0") >= 200n)!;
    const today = new Intl.DateTimeFormat("en-CA", { timeZone: "Asia/Ho_Chi_Minh" }).format(new Date());
    const opened = await unwrap(api.POST("/v1/cooperatives/{ref}/households/{household_ref}/seasons", { params: { path: { ref: coop, household_ref: h.household_ref }, header: { "Idempotency-Key": newIdempotencyKey() } },
      body: { crop_code: "LUA", season_code: "DX", season_year: 2035, variety_code: "OM5451", area_ha: "0.01", anchor_date: today, channel: "web", gps: { source: "denied" } } }));
    const path = { ref: coop, household_ref: h.household_ref, season_ref: opened.season.season_ref };
    const post = (persona: string, body: Record<string, unknown>) => client(persona).POST("/v1/cooperatives/{ref}/households/{household_ref}/seasons/{season_ref}/corrections", {
      params: { path, header: { "Idempotency-Key": newIdempotencyKey() } }, body: body as never });
    const base = { expected_season_event_id: opened.season.current_event_id, reason: "Sạ lại do mưa lớn đầu vụ" };
    expect((await post("htx", { ...base, anchor_date: "2030-01-02" })).response.status).toBe(403);
    const edited = await post("htx-member", { ...base, anchor_date: "2030-01-02", area_ha: "0.02" });
    expect(edited.response.status).toBe(200);
    expect(edited.data).toMatchObject({ anchor_date: "2030-01-02", area_ha: "0.02" });
    const event = edited.data!.current_event_id;
    expect((await post("htx-member", { ...base, area_ha: "0.03" })).response.status).toBe(409);
    expect((await post("htx-member", { expected_season_event_id: event, reason: base.reason, variety_code: "ST25" })).response.status).toBe(422);
  });
});
