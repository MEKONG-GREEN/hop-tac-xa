import { describe, expect, it } from "vitest";
import { createApiClient, unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { htxCoop } from "./data";
import { mockFetchNoDelay } from "./handler";
import { mockToken } from "./personas";
import { scaledArea } from "./household-season-state";

describe("AH-61 mock route registration and budget", () => {
  it("opens once, pins SOP, refreshes household remaining area, representative stays read-only", async () => {
    const client = (persona: string) => createApiClient({ baseUrl: "https://api.test", getAccessToken: () => mockToken(persona), fetch: mockFetchNoDelay });
    const api = client("htx-member");
    const coop = htxCoop().api.ref;
    const households = await unwrap(api.GET("/v1/cooperatives/{ref}/households", { params: { path: { ref: coop }, query: { limit: 200 } } }));
    const h = households.items.find((h) => h.can_open_season && h.remaining_area_ha && scaledArea(h.remaining_area_ha) >= 100n)!;
    expect(h).toBeTruthy();
    const path = { ref: coop, household_ref: h.household_ref };
    const key = newIdempotencyKey();
    const body = { crop_code: "LUA", season_code: "DX", season_year: 2030, variety_code: "OM5451", area_ha: "0.01", anchor_date: "2030-01-01", gps: { source: "denied" as const }, channel: "web" as const };
    const send = () => unwrap(api.POST("/v1/cooperatives/{ref}/households/{household_ref}/seasons", { params: { path, header: { "Idempotency-Key": key } }, body }));
    const first = await send();
    const replay = await send();
    expect(replay.season.season_ref).toBe(first.season.season_ref);
    expect(first.season.gps_missing).toBe(true);
    expect(first.season.sop_snapshot.crop_code).toBe("LUA");
    const refreshed = await unwrap(api.GET("/v1/cooperatives/{ref}/households", { params: { path: { ref: coop }, query: { limit: 200 } } }));
    const remaining = refreshed.items.find((r) => r.household_ref === h.household_ref)!.remaining_area_ha!;
    expect(scaledArea(h.remaining_area_ha!) - scaledArea(remaining)).toBe(100n);
    const representative = client("htx");
    const detail = await unwrap(representative.GET("/v1/cooperatives/{ref}/households/{household_ref}/seasons/{season_ref}", { params: { path: { ...path, season_ref: first.season.season_ref } } }));
    expect(detail.sop_snapshot).toEqual(first.season.sop_snapshot);
    const denied = await representative.POST("/v1/cooperatives/{ref}/households/{household_ref}/seasons", { params: { path, header: { "Idempotency-Key": newIdempotencyKey() } }, body });
    expect(denied.response.status).toBe(403);
  });
});
