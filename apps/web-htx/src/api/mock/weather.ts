// Demo của pf/weather (FN-TT-01..03, ADR-0065): số liệu sinh tất định theo HTX và giờ, cùng quy tắc với máy chủ — buổi 6
// giờ (BR-03), nhãn ngày đã qua (BR-04), phạm vi dự báo (AC-03), âm lịch GMT+7. Không gọi nhà cung cấp thật.
import type { components as Admin } from "../generated/admin";
import type { components as App } from "../generated/application";
import { lunarLabel, solarToLunar } from "../../lib/lunar";
import { COOPERATIVES } from "./data";
import { hasRole, json, problem, validation, type Ctx, type Route } from "./http";

type Today = App["schemas"]["WeatherToday"];
type Calendar = App["schemas"]["WeatherCalendar"];
type Summary = App["schemas"]["WeatherSummary"];
type Period = App["schemas"]["WeatherPeriod"];
type Config = Admin["schemas"]["WeatherConfig"];

const PROVIDERS = [{ code: "open_meteo", label: "Open-Meteo.com" }, { code: "fake", label: "Nguồn thử nghiệm" }];
const DEFAULT = { provider: "fake", refresh_minutes: 60, forecast_days: 7, last4: "" };
const configs = new Map<string, typeof DEFAULT & { version: number; updated_at: string }>();
let failure = "";

/** Kiểm thử: đặt lỗi của nhà cung cấp giả (unavailable | quota | auth | bad_response; "" = thành công). */
export function setMockWeatherFailure(code: string) { failure = code; }
export function resetWeatherMock() { configs.clear(); failure = ""; }

const VN_OFFSET = 7 * 3600_000;
const vnParts = (t: number) => { const d = new Date(t + VN_OFFSET); return { y: d.getUTCFullYear(), m: d.getUTCMonth() + 1, d: d.getUTCDate(), h: d.getUTCHours() }; };
const iso = (y: number, m: number, d: number) => `${y}-${String(m).padStart(2, "0")}-${String(d).padStart(2, "0")}`;
const lunarDto = (y: number, m: number, d: number) => { const l = solarToLunar(y, m, d); return { ...l, label: lunarLabel(l), first_day: l.day === 1, full_moon: l.day === 15 }; };
const DIRS = ["B", "ĐB", "Đ", "ĐN", "N", "TN", "T", "TB"];
const dir = (deg: number) => DIRS[Math.floor((((deg % 360) + 360) % 360 + 22.5) / 45) % 8]!;
const condition = (code: number) => (code >= 61 ? "Mưa nhỏ" : code === 3 ? "Nhiều mây" : code === 2 ? "Có mây" : "Ít mây");

interface Hour { h: number; temp: number; rain: number; prob: number; wind: number; dir: number; hum: number; code: number }
function hours(seed: number, dayIndex: number): Hour[] {
  return Array.from({ length: 24 }, (_, h) => {
    const x = seed + dayIndex * 24 + h;
    const rain = h >= 13 && h <= 17 && Math.floor(x) % 3 === 0 ? (Math.floor(x) % 6) + 0.5 : 0;
    return { h, temp: Math.round((27 + 4 * Math.sin(((h - 9) * Math.PI) / 12) + Math.sin(x) / 2) * 10) / 10, rain, prob: Math.floor(x * 13) % 90,
      wind: Math.round((8 + 6 * Math.abs(Math.sin(x / 5))) * 10) / 10, dir: Math.floor(x * 37) % 360, hum: 65 + (Math.floor(x) % 30), code: rain > 0 ? 61 : 2 };
  });
}
function summary(hs: Hour[]): Summary {
  const max = (k: keyof Hour) => Math.max(...hs.map((h) => h[k]));
  const rain = Math.round(hs.reduce((s, h) => s + h.rain, 0) * 10) / 10;
  const windiest = hs.reduce((a, b) => (b.wind > a.wind ? b : a));
  const code = rain > 0 ? 61 : 2;
  return { condition: condition(code), weather_code: code, temperature_min_c: Math.round(Math.min(...hs.map((h) => h.temp))), temperature_max_c: Math.round(max("temp")),
    precipitation_mm: rain, precipitation_probability_max: Math.round(max("prob")), wind_speed_max_kmh: Math.round(windiest.wind), wind_direction: dir(windiest.dir),
    humidity_pct: Math.round(hs.reduce((s, h) => s + h.hum, 0) / hs.length) };
}
function periods(hs: Hour[]): Period[] {
  return (["Đêm", "Sáng", "Chiều", "Tối"] as const).map((name, i) => {
    const b = hs.slice(i * 6, i * 6 + 6);
    const rain = Math.round(b.reduce((s, h) => s + h.rain, 0) * 10) / 10;
    const code = Math.max(...b.map((h) => h.code));
    return { name, condition: condition(code), weather_code: code, temperature_c: Math.round(b.reduce((s, h) => s + h.temp, 0) / b.length),
      precipitation_mm: rain, precipitation_probability_max: Math.max(...b.map((h) => h.prob)), wind_speed_max_kmh: Math.round(Math.max(...b.map((h) => h.wind))) };
  });
}

function scope(ctx: Ctx): { coop: string } | Response {
  const p = ctx.persona;
  if (!hasRole(p, "platform_admin", "coop_manager", "htx_member")) return problem(403, "forbidden", "Không có quyền");
  const want = ctx.url.searchParams.get("cooperative_ref") ?? "";
  if (hasRole(p, "platform_admin")) return want ? { coop: want } : validation([{ field: "cooperative_ref", reason: "Chọn HTX" }]);
  const own = p.me.org?.ref ?? "";
  if (want && want !== own) return problem(403, "forbidden", "Không có quyền");
  return { coop: own };
}

function site(coop: string) {
  const c = COOPERATIVES.find((x) => x.api.ref === coop);
  return c ? { found: true, seed: c.lnglat[1] * 7 + c.lnglat[0] * 3, located: !!c.api.location } : { found: false, seed: 0, located: false };
}

function source(tenant: string, now: number) {
  const cfg = configs.get(tenant) ?? { ...DEFAULT };
  const fetched = now - ((now / 60000) % cfg.refresh_minutes) * 60000;
  return { provider: cfg.provider, label: PROVIDERS.find((p) => p.code === cfg.provider)?.label ?? cfg.provider, updated_at: new Date(fetched).toISOString(), stale: failure !== "" };
}

export const weatherAppRoutes: Route[] = [
  { method: "GET", pattern: /^\/v1\/weather\/today$/, handler: (ctx) => {
    const s = scope(ctx); if (s instanceof Response) return s;
    const now = Date.now(), t = vnParts(now), st = site(s.coop);
    if (!st.found) return problem(404, "not_found", "Không tìm thấy HTX");
    const base: Today = { cooperative_ref: s.coop, date: iso(t.y, t.m, t.d), lunar: lunarDto(t.y, t.m, t.d), status: "no_location", current: null, today: null, source: source(ctx.persona.me.tenant_id, now) };
    if (!st.located) return json({ ...base, message: "Chưa có vị trí để xem thời tiết. Liên hệ Admin để cập nhật." }, 200, { "Cache-Control": "no-store" });
    const hs = hours(st.seed, 0), cur = hs[t.h]!;
    return json({ ...base, status: "ok", today: summary(hs), current: { time: new Date(now).toISOString(), temperature_c: Math.round(cur.temp), condition: condition(cur.code), weather_code: cur.code,
      precipitation_mm: cur.rain, precipitation_probability: cur.prob, wind_speed_kmh: Math.round(cur.wind), wind_direction: dir(cur.dir), humidity_pct: cur.hum } }, 200, { "Cache-Control": "no-store" });
  } },
  { method: "GET", pattern: /^\/v1\/weather\/calendar$/, handler: (ctx) => {
    const s = scope(ctx); if (s instanceof Response) return s;
    const now = Date.now(), t = vnParts(now), st = site(s.coop);
    if (!st.found) return problem(404, "not_found", "Không tìm thấy HTX");
    const month = ctx.url.searchParams.get("month") ?? `${t.y}-${String(t.m).padStart(2, "0")}`;
    const mm = /^(\d{4})-(\d{2})$/.exec(month);
    if (!mm) return validation([{ field: "month", reason: "Tháng dạng YYYY-MM" }]);
    const y = Number(mm[1]), m = Number(mm[2]), count = new Date(Date.UTC(y, m, 0)).getUTCDate();
    const cfg = configs.get(ctx.persona.me.tenant_id) ?? { ...DEFAULT };
    const todayIdx = Date.UTC(t.y, t.m - 1, t.d) / 86400_000;
    const days: Calendar["days"] = Array.from({ length: count }, (_, i) => {
      const d = i + 1, idx = Date.UTC(y, m - 1, d) / 86400_000 - todayIdx;
      let weather: Calendar["days"][number]["weather"] = null;
      if (st.located && idx >= 0 && idx < cfg.forecast_days) { const hs = hours(st.seed, idx); weather = { ...summary(hs), kind: "forecast", label: "", hourly_available: true, periods: periods(hs) }; }
      else if (st.located && idx < 0 && idx >= -10) { const hs = hours(st.seed, idx); weather = { ...summary(hs), kind: "final_forecast", label: "Dự báo cuối ngày", hourly_available: true, periods: periods(hs) }; }
      return { date: iso(y, m, d), lunar: lunarDto(y, m, d), weather };
    });
    const body: Calendar = { cooperative_ref: s.coop, month, status: st.located ? "ok" : "no_location", forecast_days: cfg.forecast_days, days, source: source(ctx.persona.me.tenant_id, now) };
    if (!st.located) body.message = "Chưa có vị trí để xem thời tiết. Liên hệ Admin để cập nhật.";
    return json(body, 200, { "Cache-Control": "no-store" });
  } },
];

function adminGuard(ctx: Ctx) {
  if (!hasRole(ctx.persona, "platform_admin")) return problem(403, "forbidden", "Chỉ Admin cấu hình kết nối thời tiết");
  return null;
}
function view(tenant: string): Config {
  const c = configs.get(tenant);
  const cfg = c ?? { ...DEFAULT, version: 0, updated_at: "" };
  return { configured: !!c, provider: cfg.provider, providers: PROVIDERS, refresh_minutes: cfg.refresh_minutes, refresh_options: [30, 60, 120, 180, 360],
    forecast_days: cfg.forecast_days, max_forecast_days: 14, api_key_set: cfg.last4 !== "", api_key_masked: cfg.last4 ? `••••${cfg.last4}` : "", version: cfg.version, updated_at: c ? cfg.updated_at : null };
}
const REASONS: Record<string, string> = { auth: "Khóa API không đúng hoặc đã hết hạn.", quota: "Đã vượt hạn mức gọi của nhà cung cấp.", bad_response: "Nhà cung cấp trả số liệu không đúng định dạng.", unavailable: "Không kết nối được nhà cung cấp thời tiết." };
function check(body: unknown): { input: { provider: string; refresh_minutes: number; forecast_days: number; api_key?: string } } | Response {
  const b = (body ?? {}) as Record<string, unknown>;
  if (!PROVIDERS.some((p) => p.code === b.provider)) return validation([{ field: "provider", reason: "Chọn nhà cung cấp được hỗ trợ" }]);
  if (![30, 60, 120, 180, 360].includes(b.refresh_minutes as number)) return validation([{ field: "refresh_minutes", reason: "Tần suất cập nhật không hợp lệ" }]);
  if (typeof b.forecast_days !== "number" || b.forecast_days < 1 || b.forecast_days > 14) return validation([{ field: "forecast_days", reason: "Số ngày dự báo từ 1 đến 14" }]);
  if (failure) return problem(422, "weather.connection_failed", "Kiểm tra kết nối thất bại", { detail: REASONS[failure] ?? REASONS.unavailable, reason: failure });
  return { input: b as { provider: string; refresh_minutes: number; forecast_days: number; api_key?: string } };
}

export const weatherAdminRoutes: Route[] = [
  { method: "GET", pattern: /^\/v1\/weather\/config$/, handler: (ctx) => adminGuard(ctx) ?? json(view(ctx.persona.me.tenant_id), 200, { "Cache-Control": "no-store" }) },
  { method: "POST", pattern: /^\/v1\/weather\/config\/test$/, handler: (ctx) => {
    const denied = adminGuard(ctx); if (denied) return denied;
    const c = check(ctx.body); if (c instanceof Response) return c;
    return json({ ok: true, provider: c.input.provider, temperature_c: 30, condition: "Có mây", days: 1 });
  } },
  { method: "POST", pattern: /^\/v1\/weather\/config\/revisions$/, handler: (ctx) => {
    const denied = adminGuard(ctx); if (denied) return denied;
    const c = check(ctx.body); if (c instanceof Response) return c;
    const prev = configs.get(ctx.persona.me.tenant_id);
    const key = c.input.api_key;
    const last4 = key === undefined ? (prev?.last4 ?? "") : key === "" ? "" : key.length < 8 ? key.slice(-1) : key.slice(-4);
    configs.set(ctx.persona.me.tenant_id, { provider: c.input.provider, refresh_minutes: c.input.refresh_minutes, forecast_days: c.input.forecast_days, last4,
      version: (prev?.version ?? 0) + 1, updated_at: new Date().toISOString() });
    return json(view(ctx.persona.me.tenant_id), 201, { "Cache-Control": "no-store" });
  } },
];
