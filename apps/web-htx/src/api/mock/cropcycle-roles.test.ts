import { describe, expect, it } from "vitest";
import { createApiClient, unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { htxCoop } from "@/api/mock/data";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { mockToken } from "@/api/mock/personas";
import { ApiProblem } from "@/api/problem";
import { todayVN } from "@/htx/rules";

// Ma trận KHMV (AH-39) trên backend giả lập: Thành viên HTX mở vụ, ghi nhật ký; Đại diện HTX chỉ xem.
const app = (persona: string) => createApiClient({ baseUrl: "https://api.test", getAccessToken: () => mockToken(persona), fetch: mockFetchNoDelay });

async function problemOf(p: Promise<unknown>): Promise<ApiProblem> {
  try {
    await p;
  } catch (e) {
    if (e instanceof ApiProblem) return e;
    throw e;
  }
  throw new Error("mong đợi ApiProblem");
}

/** Một lô đang dùng của HTX giả lập chưa có vụ mở, và một vụ mẫu để lấy mã vụ, giống hợp lệ. */
async function freePlot(api: ReturnType<typeof app>) {
  const coop = htxCoop().api.ref;
  const open = await unwrap(api.GET("/v1/cropcycle/seasons", { params: { query: { cooperative_ref: coop, status: "open", limit: 200 } } }));
  const plots = await unwrap(api.GET("/v1/cooperatives/{ref}/plots", { params: { path: { ref: coop }, query: { limit: 1000 } } }));
  const busy = new Set(open.items.map((s) => s.plot_ref));
  const plot = plots.features.find((p) => p.properties.status === "active" && p.properties.cooperative_ref === coop && !busy.has(p.properties.ref));
  const sample = open.items[0];
  if (!plot || !sample) throw new Error("dữ liệu giả lập thiếu lô trống hoặc vụ mẫu");
  return { plot: plot.properties.ref, seasonCode: sample.season_code, variety: sample.variety_code, openPlot: sample.plot_ref };
}

const openBody = (plot: string, seasonCode: string, variety: string) => ({
  plot_ref: plot,
  season_code: seasonCode,
  season_year: Number(todayVN().slice(0, 4)),
  variety_code: variety,
  sowing_date: todayVN(),
  gps: { lat: "10.4", lon: "105.6", source: "manual_pick" as const },
});

describe("[AH-39] ma trận KHMV trên API vụ mùa giả lập", () => {
  it("Thành viên HTX mở vụ (201) và ghi nhật ký (201) cho HTX mình", async () => {
    const api = app("htx-member");
    const f = await freePlot(api);
    const opened = await unwrap(
      api.POST("/v1/cropcycle/seasons", { params: { header: { "Idempotency-Key": newIdempotencyKey() } }, body: openBody(f.plot, f.seasonCode, f.variety) }),
    );
    expect(opened.season.plot_ref).toBe(f.plot);
    expect(opened.season.on_behalf).toBe(true);
    const log = await unwrap(
      api.POST("/v1/cropcycle/farm-logs", { params: { header: { "Idempotency-Key": newIdempotencyKey() } }, body: { plot_ref: f.openPlot, activity_code: "kiem_tra" } }),
    );
    expect(log.log.season_ref).toBeTruthy();
  });

  it("Đại diện HTX đọc danh sách vụ (200) nhưng mở vụ, ghi nhật ký, nhập hàng loạt bị 403 cropcycle.read_only", async () => {
    const api = app("htx");
    const f = await freePlot(api);
    const open = await problemOf(
      unwrap(api.POST("/v1/cropcycle/seasons", { params: { header: { "Idempotency-Key": newIdempotencyKey() } }, body: openBody(f.plot, f.seasonCode, f.variety) })),
    );
    expect(open.status).toBe(403);
    expect(open.code).toBe("cropcycle.read_only");
    const log = await problemOf(
      unwrap(api.POST("/v1/cropcycle/farm-logs", { params: { header: { "Idempotency-Key": newIdempotencyKey() } }, body: { plot_ref: f.openPlot, activity_code: "kiem_tra" } })),
    );
    expect(log.code).toBe("cropcycle.read_only");
    const bulk = await problemOf(
      unwrap(api.POST("/v1/cropcycle/harvests/bulk", { params: { header: { "Idempotency-Key": newIdempotencyKey() } }, body: { rows: [{ plot_code: "X", mass_kg: "1" }] } })),
    );
    expect(bulk.code).toBe("cropcycle.read_only");
  });
});
