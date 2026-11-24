import { describe, expect, it } from "vitest";
import { createApiClient, unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { htxCoop } from "@/api/mock/data";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { scaledArea } from "@/api/mock/household-season-state";
import { mockToken } from "@/api/mock/personas";
import { batchErrors } from "./TaskBatchForm";

describe("AH-176 batchErrors", () => {
  it("requires seasons, a date not after today and a reason for backfill beyond 3 days", () => {
    expect(batchErrors({ selected: 2, doneOn: "2026-11-20", materials: [], backfill: "" }, "2026-11-21")).toEqual([]);
    expect(batchErrors({ selected: 0, doneOn: "2026-11-22", materials: [], backfill: "" }, "2026-11-21")).toEqual(["Chọn ít nhất một vụ.", "Ngày làm không được sau hôm nay."]);
    expect(batchErrors({ selected: 51, doneOn: "2026-11-17", materials: [{ name: "Vôi", unit: "kg", dose: "-1" }], backfill: "" }, "2026-11-21"))
      .toEqual(["Chọn tối đa 50 vụ.", "Vật tư 1: liều không âm, tối đa 4 chữ số thập phân.", "Nhập lý do khi ghi bù quá 3 ngày."]);
  });
});

describe("AH-176 mock task batch", () => {
  it("records each same-SOP season once, skips resolved or unknown seasons, replays, and keeps representatives read-only", async () => {
    const client = (persona: string) => createApiClient({ baseUrl: "https://api.test", getAccessToken: () => mockToken(persona), fetch: mockFetchNoDelay });
    const api = client("htx-member"), coop = htxCoop().api.ref;
    const households = await unwrap(api.GET("/v1/cooperatives/{ref}/households", { params: { path: { ref: coop }, query: { limit: 200 } } }));
    const household = households.items.find((h) => h.can_open_season && scaledArea(h.remaining_area_ha ?? "0") >= 200n)!;
    const base = { ref: coop, household_ref: household.household_ref };
    const today = new Intl.DateTimeFormat("en-CA", { timeZone: "Asia/Ho_Chi_Minh" }).format(new Date());
    const open = (year: number) => unwrap(api.POST("/v1/cooperatives/{ref}/households/{household_ref}/seasons", { params: { path: base, header: { "Idempotency-Key": newIdempotencyKey() } }, body: { crop_code: "LUA", season_code: "DX", season_year: year, variety_code: "OM5451", area_ha: "0.01", anchor_date: today, channel: "web", gps: { source: "denied" } } }));
    const a = await open(2031), b = await open(2032);
    const path = { ...base, season_ref: a.season.season_ref }, pathB = { ...base, season_ref: b.season.season_ref };
    const checklist = await unwrap(api.GET("/v1/cooperatives/{ref}/households/{household_ref}/seasons/{season_ref}/checklist", { params: { path } }));
    const task = checklist.progress.stages.flatMap((s) => s.tasks).find((t) => t.can_complete)!;
    const ticked = await unwrap(api.POST("/v1/cooperatives/{ref}/households/{household_ref}/seasons/{season_ref}/task-completions", { params: { path: pathB, header: { "Idempotency-Key": newIdempotencyKey() } }, body: { step_seq: task.seq, expected_season_event_id: b.season.current_event_id, gps: { source: "denied" }, channel: "web" } }));
    const candidates = await unwrap(api.GET("/v1/cooperatives/{ref}/households/{household_ref}/seasons/{season_ref}/task-batch-candidates", { params: { path, query: { step_seq: task.seq } } }));
    const mine = candidates.items.filter((c) => c.season_ref === a.season.season_ref || c.season_ref === b.season.season_ref);
    expect(mine.find((c) => c.is_source)?.eligible).toBe(true);
    expect(mine.find((c) => !c.is_source)?.code).toBe("cropcycle.task_already_resolved");
    const body = { step_seq: task.seq, state: "done" as const, done_on: today, note: "Bón thúc chung", zero_confirmed: true, materials: [], gps: { source: "no_device" as const }, channel: "web" as const,
      seasons: [{ household_ref: base.household_ref, season_ref: a.season.season_ref, expected_season_event_id: a.season.current_event_id },
        { household_ref: base.household_ref, season_ref: b.season.season_ref, expected_season_event_id: ticked.checklist.current_event_id },
        { household_ref: base.household_ref, season_ref: newIdempotencyKey(), expected_season_event_id: a.season.current_event_id }] };
    const key = newIdempotencyKey();
    const send = () => unwrap(api.POST("/v1/cooperatives/{ref}/households/{household_ref}/seasons/{season_ref}/task-batches", { params: { path, header: { "Idempotency-Key": key } }, body }));
    const first = await send();
    expect([first.recorded, first.skipped]).toEqual([1, 2]);
    expect(first.results.map((r) => r.code ?? r.status)).toEqual(["recorded", "cropcycle.task_already_resolved", "cropcycle.batch_task_mismatch"]);
    const again = await send();
    expect(again.results[0]?.record?.id).toBe(first.results[0]?.record?.id);
    const denied = await client("htx").POST("/v1/cooperatives/{ref}/households/{household_ref}/seasons/{season_ref}/task-batches", { params: { path, header: { "Idempotency-Key": newIdempotencyKey() } }, body });
    expect(denied.response.status).toBe(403);
  }, 15000);
});
