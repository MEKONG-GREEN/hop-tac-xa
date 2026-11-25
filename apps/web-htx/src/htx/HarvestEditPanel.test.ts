import { describe, expect, it } from "vitest";
import { createApiClient, unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { htxCoop } from "@/api/mock/data";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { scaledArea } from "@/api/mock/household-season-state";
import { mockToken } from "@/api/mock/personas";
import { historyLine, reasonError } from "./HarvestEditPanel";

describe("AH-189 kiểm lý do và dòng lịch sử", () => {
  it("lý do tùy chọn khi sửa, bắt buộc khi hủy", () => {
    expect(reasonError("", false)).toBeNull();
    expect(reasonError("ngắn", false)).not.toBeNull();
    expect(reasonError("", true)).not.toBeNull();
    expect(reasonError("Khai báo nhầm vụ của hộ khác", true)).toBeNull();
  });
  it("cũ → mới", () => {
    const base = { harvest_id: "a", mass_kg: "9750", yield_t_per_ha: "6.5", zero_confirmed: false, declared_by: "u", declared_at: "2026-09-20T03:00:00Z", supersedes_id: null, reason: null };
    const declared = { ...base, kind: "declared" as const, mass_t: "9.75", harvest_date: "2026-09-20" };
    const corrected = { ...base, harvest_id: "b", kind: "corrected" as const, mass_t: "10.20", harvest_date: "2026-09-22", supersedes_id: "a", reason: "Cân lại" };
    expect(historyLine(corrected, declared)).toBe("Sửa 9,75 tấn → 10,20 tấn, ngày 20/09/2026 → 22/09/2026");
    expect(historyLine(declared)).toBe("Khai báo 9,75 tấn (20/09/2026)");
    expect(historyLine({ ...corrected, kind: "voided" })).toBe("Hủy khai báo 10,20 tấn (22/09/2026)");
  });
});

describe("AH-189 mock sửa, hủy khai báo", () => {
  it("sửa giữ lịch sử, hủy đưa vụ về Đang canh tác, Đại diện bị từ chối", async () => {
    const client = (persona: string) => createApiClient({ baseUrl: "https://api.test", getAccessToken: () => mockToken(persona), fetch: mockFetchNoDelay });
    const api = client("htx-member"), coop = htxCoop().api.ref;
    const households = await unwrap(api.GET("/v1/cooperatives/{ref}/households", { params: { path: { ref: coop }, query: { limit: 200 } } }));
    const h = households.items.find((h) => h.can_open_season && scaledArea(h.remaining_area_ha ?? "0") >= 100n)!;
    const today = new Intl.DateTimeFormat("en-CA", { timeZone: "Asia/Ho_Chi_Minh" }).format(new Date());
    const opened = await unwrap(api.POST("/v1/cooperatives/{ref}/households/{household_ref}/seasons", { params: { path: { ref: coop, household_ref: h.household_ref }, header: { "Idempotency-Key": newIdempotencyKey() } },
      body: { crop_code: "LUA", season_code: "DX", season_year: 2036, variety_code: "OM5451", area_ha: "0.01", anchor_date: today, channel: "web", gps: { source: "denied" } } }));
    const path = { ref: coop, household_ref: h.household_ref, season_ref: opened.season.season_ref };
    const declared = await unwrap(api.POST("/v1/cooperatives/{ref}/households/{household_ref}/seasons/{season_ref}/harvest", { params: { path, header: { "Idempotency-Key": newIdempotencyKey() } },
      body: { expected_season_event_id: opened.season.current_event_id, mass_t: "0.06", harvest_date: today, gps: { source: "denied" }, channel: "web" } }));
    const editUrl = "/v1/cooperatives/{ref}/households/{household_ref}/seasons/{season_ref}/harvest/corrections" as const;
    const denied = await client("htx").POST(editUrl, { params: { path, header: { "Idempotency-Key": newIdempotencyKey() } }, body: { expected_harvest_id: declared.harvest.harvest_id, mass_t: "0.07", harvest_date: today } });
    expect(denied.response.status).toBe(403);
    const edited = await unwrap(api.POST(editUrl, { params: { path, header: { "Idempotency-Key": newIdempotencyKey() } }, body: { expected_harvest_id: declared.harvest.harvest_id, mass_t: "0.07", harvest_date: today } }));
    expect(edited.harvest.mass_t).toBe("0.07");
    const stale = await api.POST(editUrl, { params: { path, header: { "Idempotency-Key": newIdempotencyKey() } }, body: { expected_harvest_id: declared.harvest.harvest_id, mass_t: "0.08", harvest_date: today } });
    expect(stale.response.status).toBe(409);
    const voidUrl = "/v1/cooperatives/{ref}/households/{household_ref}/seasons/{season_ref}/harvest/void" as const;
    const noReason = await api.POST(voidUrl, { params: { path, header: { "Idempotency-Key": newIdempotencyKey() } }, body: { expected_harvest_id: edited.harvest.harvest_id, reason: "" } });
    expect(noReason.response.status).toBe(422);
    const voided = await unwrap(api.POST(voidUrl, { params: { path, header: { "Idempotency-Key": newIdempotencyKey() } }, body: { expected_harvest_id: edited.harvest.harvest_id, reason: "Khai báo nhầm vụ của hộ khác" } }));
    expect(voided.status).toBe("open");
    expect(voided.harvest).toBeUndefined();
    const history = await unwrap(api.GET("/v1/cooperatives/{ref}/households/{household_ref}/seasons/{season_ref}/harvest/history", { params: { path } }));
    expect(history.items.map((v) => v.kind)).toEqual(["declared", "corrected", "voided"]);
  });
});
