import type { components } from "../generated/application";
import { HTX_ROLE_MATRIX } from "../generated/htx-roles";
import { uuidv7 } from "../idempotency";
import { CATALOGS, findCoop } from "./data";
import { json, nowIso, problem, validation, type Route } from "./http";
import { MOCK_PERSONAS, type MockPersona } from "./personas";

type Calendar = components["schemas"]["SeasonCalendar"];
type Draft = components["schemas"]["SeasonCalendarInput"];
const calendars: Calendar[] = [];
const role = (p: MockPersona) => HTX_ROLE_MATRIX.roles.find((r) => p.me.roles.includes(r.key))?.key;
function can(p: MockPersona, action: string) {
  const effective = role(p);
  return !!effective && !!HTX_ROLE_MATRIX.functions.find((f) => f.key === "KHMV-FN-03-LICH")?.grants[effective].includes(action);
}
function scope(p: MockPersona, coop: string) { return can(p, "R") && !!findCoop(coop) && (role(p) === "platform_admin" || p.me.org?.ref === coop); }
function members(coop: string, tenant: string) { return MOCK_PERSONAS.filter((p) => p.me.tenant_id === tenant && p.me.org?.ref === coop && role(p) === "htx_member"); }
function view(c: Calendar, p: MockPersona): Calendar {
  const result = { ...c, can_manage: can(p, "C") };
  if (!result.can_manage) delete result.recipients;
  return result;
}
function visible(c: Calendar, p: MockPersona) { return scope(p, c.cooperative_ref) && (can(p, "C") || (c.status === "published" && c.recipients?.includes(p.me.actor_ref))); }
function validDay(s: unknown): s is string {
  if (typeof s !== "string" || !/^\d{4}-\d{2}-\d{2}$/.test(s)) return false;
  const d = new Date(`${s}T00:00:00Z`); return !Number.isNaN(d.getTime()) && d.toISOString().slice(0, 10) === s;
}

export const seasonCalendarRoutes: Route[] = [
  { method: "GET", pattern: /^\/v1\/cooperatives\/([\w-]+)\/season-calendar-recipients$/, handler: ({ params, persona, url }) => {
    if (!can(persona, "C")) return problem(403, "forbidden", "Không có quyền đọc danh bạ người nhận");
    const coop = params[0]!; if (!scope(persona, coop)) return problem(404, "cropcycle.calendar_not_found", "Không tìm thấy HTX");
    const offset = Number(url.searchParams.get("offset") ?? 0), limit = Number(url.searchParams.get("limit") ?? 50);
    if (!Number.isInteger(offset) || offset < 0 || !Number.isInteger(limit) || limit < 1 || limit > 100) return validation([{ field: "pagination", reason: "Phân trang không hợp lệ" }]);
    const people = members(coop, persona.me.tenant_id);
    const items = people.slice(offset, offset + limit).map((p) => ({ actor_ref: p.me.actor_ref, label: p.displayName }));
    return json({ items, next_offset: offset + limit < people.length ? offset + limit : 0 }, 200, { "Cache-Control": "no-store" });
  } },
  { method: "GET", pattern: /^\/v1\/cooperatives\/([\w-]+)\/season-calendars$/, handler: ({ params, persona, url }) => {
    const coop = params[0]!; if (!scope(persona, coop)) return problem(404, "cropcycle.calendar_not_found", "Không tìm thấy lịch");
    const limit = Number(url.searchParams.get("limit") ?? 50), after = url.searchParams.get("after");
    if (!Number.isInteger(limit) || limit < 1 || limit > 100) return validation([{ field: "limit", reason: "Từ 1 đến 100" }]);
    const rows = calendars.filter((c) => c.cooperative_ref === coop && visible(c, persona) && (!after || c.calendar_ref < after)).sort((a, b) => b.calendar_ref.localeCompare(a.calendar_ref));
    return json({ items: rows.slice(0, limit).map((c) => view(c, persona)), next_cursor: rows.length > limit ? rows[limit - 1]!.calendar_ref : null });
  } },
  { method: "GET", pattern: /^\/v1\/cropcycle\/calendars\/([\w-]+)$/, handler: ({ params, persona }) => {
    const c = calendars.find((c) => c.calendar_ref === params[0]);
    return c && visible(c, persona) ? json(view(c, persona)) : problem(404, "cropcycle.calendar_not_found", "Lịch đã hủy hoặc ngoài phạm vi");
  } },
  { method: "POST", pattern: /^\/v1\/cooperatives\/([\w-]+)\/season-calendars(?:\/([\w-]+)\/(revisions|cancel))?$/, handler: ({ params, persona, body }) => {
    const coop = params[0]!, ref = params[1], action = params[2];
    if (!can(persona, action === "cancel" ? "D" : ref ? "U" : "C")) return problem(403, "cropcycle.read_only", "Chỉ được xem lịch");
    if (!scope(persona, coop)) return problem(404, "cropcycle.calendar_not_found", "Không tìm thấy HTX");
    if (!body||typeof body!=="object"||Array.isArray(body)) return validation([{field:"body",reason:"Dữ liệu lịch không hợp lệ"}]);
    const b = body as Draft & { expected_event_id?: string };
    const allowed=new Set(action==="cancel"?["expected_event_id"]:["crop_code","season_code","season_year","sow_from","sow_to","recipients",...(ref?["expected_event_id"]:[])]);
    if(Object.keys(b).some((k)=>!allowed.has(k)))return validation([{field:"body",reason:"Trường không được hỗ trợ"}]);
    const current = ref ? calendars.find((c) => c.calendar_ref === ref && c.cooperative_ref === coop) : undefined;
    if (ref && !current) return problem(404, "cropcycle.calendar_not_found", "Không tìm thấy lịch");
    if (current && current.current_event_id !== b.expected_event_id) return problem(409, "cropcycle.calendar_changed", "Lịch đã thay đổi; tải lại");
    if (current?.status === "cancelled") return problem(409, "cropcycle.calendar_cancelled", "Lịch đã hủy");
    if (action === "cancel" && current) {
      if (Object.keys(b).some((k) => k !== "expected_event_id")) return validation([{ field: "body", reason: "Hủy chỉ cần phiên bản lịch" }]);
      current.status = "cancelled"; current.version++; current.current_event_id = uuidv7(); current.updated_at = nowIso(); return json(view(current, persona), 201);
    }
    const valid = members(coop, persona.me.tenant_id).map((p) => p.me.actor_ref);
    if(!CATALOGS.crop.some((c)=>c.code===b.crop_code&&c.active&&!c.attrs?.parent_code)||!CATALOGS.season.some((c)=>c.code===b.season_code&&c.active))return validation([{field:"catalog",reason:"Chọn loại cây và mùa vụ đang dùng"}]);
    if (!b.crop_code || !b.season_code || !Number.isInteger(b.season_year) || b.season_year < 2000 || b.season_year > 2100 || !validDay(b.sow_from) || !validDay(b.sow_to) || b.sow_from > b.sow_to || !Array.isArray(b.recipients) || !b.recipients.length || b.recipients.length > 500 || b.recipients.some((id) => !valid.includes(id))) return validation([{ field: "calendar", reason: "Kiểm loại cây, vụ/năm, khung ngày và Thành viên đúng HTX" }]);
    const c: Calendar = { crop_code:b.crop_code,season_code:b.season_code,season_year:b.season_year,sow_from:b.sow_from,sow_to:b.sow_to, recipients: [...new Set(b.recipients)].sort(), calendar_ref: ref ?? uuidv7(), cooperative_ref: coop, current_event_id: uuidv7(), version: (current?.version ?? 0) + 1, status: "published", can_manage: true, updated_at: nowIso() };
    if (current) calendars.splice(calendars.indexOf(current), 1, c); else calendars.push(c);
    return json(view(c, persona), 201);
  } },
];
