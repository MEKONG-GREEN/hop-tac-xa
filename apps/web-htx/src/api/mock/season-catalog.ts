// GIẢ LẬP quản lý danh mục mùa vụ (AH-7, BRD FN-01 luồng phụ Mùa vụ) theo hợp đồng Admin
// /v1/masterdata/catalogs/season/items: thêm (mã do máy chủ sinh), sửa, ẩn/hiện, xóa (chưa dùng → xóa, đang dùng → chỉ ẩn).
// Cùng luật với máy chủ: vai masterdata_admin/platform_admin, so trùng tên theo nameKey trong toàn danh mục (kể cả đã ẩn),
// tháng 1–12 và đã nhập thì nhập cả hai, mô tả ≤ 500, lý do ẩn không bắt buộc. Gọi lặp cùng Idempotency-Key phát lại kết quả.
import { uuidv7 } from "../idempotency";
import type { CatalogItem } from "../types";
import { seasonReferenced } from "./cropcycle";
import { CATALOGS } from "./data";
import { hasRole, json, nowIso, problem, validation, type Ctx, type Route } from "./http";

const SYSTEM_HIDE = "Ẩn tại màn Danh mục mùa vụ";

type Out = { status: number; body: unknown } | Response;

const REPLAYS = new Map<string, { status: number; body: unknown }>();

const nameKey = (s: string) => s.trim().split(/\s+/).filter(Boolean).join(" ").toLowerCase();
const list = (): CatalogItem[] => CATALOGS.season;
const find = (code: string) => list().find((i) => i.code === code);

function run(ctx: Ctx, replayable: boolean, compute: () => Out): Response {
  if (!hasRole(ctx.persona, "masterdata_admin", "platform_admin")) return problem(403, "forbidden", "Forbidden");
  const key = replayable ? ctx.request.headers.get("Idempotency-Key") : null;
  const hit = key ? REPLAYS.get(key) : undefined;
  if (hit) return json(hit.body, hit.status);
  const out = compute();
  if (out instanceof Response) return out;
  if (key) REPLAYS.set(key, out);
  return json(out.body, out.status);
}

const dupName = () => problem(409, "masterdata.catalog_item_name_taken", "Name taken", { errors: [{ field: "name", reason: "Tên đã tồn tại" }] });

interface Input {
  code?: unknown;
  name?: unknown;
  attrs?: Record<string, unknown>;
}

function month(raw: unknown): string | null {
  const s = typeof raw === "string" ? raw.trim() : "";
  return /^\d+$/.test(s) && Number(s) >= 1 && Number(s) <= 12 ? String(Number(s)) : null;
}

/** Kiểm dữ liệu vào; trả tên và attrs đã chuẩn hóa (mùa vụ không có thuộc tính cũ phải giữ) hoặc lỗi 422. */
function check(input: Input): { name: string; attrs: Record<string, string> } | Response {
  const errors: { field: string; reason: string }[] = [];
  const name = typeof input.name === "string" ? input.name.trim() : "";
  if (!name || name.length > 200) errors.push({ field: "name", reason: "bắt buộc, tối đa 200 ký tự" });
  const raw = input.attrs ?? {};
  const attrs: Record<string, string> = {};
  const description = typeof raw.description === "string" ? raw.description.trim() : "";
  if (description.length > 500) errors.push({ field: "attrs.description", reason: "tối đa 500 ký tự" });
  else if (description) attrs.description = description;
  const hasStart = raw.start_month !== undefined && raw.start_month !== "";
  const hasEnd = raw.end_month !== undefined && raw.end_month !== "";
  if (hasStart && month(raw.start_month) === null) errors.push({ field: "attrs.start_month", reason: "phải là số nguyên từ 1 đến 12" });
  if (hasEnd && month(raw.end_month) === null) errors.push({ field: "attrs.end_month", reason: "phải là số nguyên từ 1 đến 12" });
  if (hasStart !== hasEnd && errors.length === 0) errors.push({ field: "attrs.end_month", reason: "start_month và end_month đi cùng nhau" });
  if (hasStart && hasEnd && errors.length === 0) {
    attrs.start_month = month(raw.start_month)!;
    attrs.end_month = month(raw.end_month)!;
  }
  return errors.length ? validation(errors) : { name, attrs };
}

const taken = (name: string, except: string): boolean => list().some((i) => i.code !== except && nameKey(i.name) === nameKey(name));

function create(body: unknown): Out {
  const input = (body ?? {}) as Input;
  if (input.code) return validation([{ field: "code", reason: "Mã do hệ thống sinh — bỏ trống" }]);
  const ok = check(input);
  if (ok instanceof Response) return ok;
  if (taken(ok.name, "")) return dupName();
  // Mã 32 ký tự HEX viết HOA: qua được luật mã vụ [A-Za-z0-9_]{1,40} của pf/cooperative.
  const item: CatalogItem = {
    catalog: "season",
    code: uuidv7().replace(/-/g, "").toUpperCase(),
    name: ok.name,
    attrs: ok.attrs,
    active: true,
    deactivation_reason: null,
    revision: 1,
    updated_at: nowIso(),
  };
  list().push(item);
  return { status: 201, body: item };
}

function update(code: string, body: unknown): Out {
  const cur = find(code);
  if (!cur) return problem(404, "masterdata.catalog_item_not_found", "Not found");
  const ok = check((body ?? {}) as Input);
  if (ok instanceof Response) return ok;
  if (taken(ok.name, code)) return dupName();
  // Đổi tên không đổi mã và không sửa bản ghi vụ đã tạo.
  cur.attrs = ok.attrs;
  cur.name = ok.name;
  cur.revision += 1;
  cur.updated_at = nowIso();
  return { status: 200, body: cur };
}

function hide(item: CatalogItem, reason: string): void {
  item.active = false;
  item.deactivation_reason = reason;
  item.revision += 1;
  item.updated_at = nowIso();
}

function setActive(code: string, active: boolean, body: unknown): Out {
  const cur = find(code);
  if (!cur) return problem(404, "masterdata.catalog_item_not_found", "Not found");
  const reason = typeof (body as { reason?: unknown } | undefined)?.reason === "string" ? (body as { reason: string }).reason.trim() : "";
  if (reason && (reason.length < 10 || reason.length > 500)) return validation([{ field: "reason", reason: "lý do bắt buộc, 10–500 ký tự" }]);
  if (cur.active === active) return problem(409, "masterdata.catalog_item_state", "State");
  if (!active) hide(cur, reason || SYSTEM_HIDE);
  else {
    cur.active = true;
    cur.deactivation_reason = null;
    cur.revision += 1;
    cur.updated_at = nowIso();
  }
  return { status: 200, body: cur };
}

function remove(code: string): Out {
  const cur = find(code);
  if (!cur) return problem(404, "masterdata.catalog_item_not_found", "Not found");
  if (seasonReferenced(code)) {
    if (cur.active) hide(cur, SYSTEM_HIDE);
    return { status: 200, body: { outcome: "hidden", item: cur } };
  }
  const rows = list();
  rows.splice(rows.indexOf(cur), 1);
  return { status: 200, body: { outcome: "deleted", item: { ...cur, active: false } } };
}

const BASE = /^\/v1\/masterdata\/catalogs\/season\/items/;
const CODE = "([A-Za-z0-9_.-]+)";

export const seasonCatalogAdminRoutes: Route[] = [
  { method: "POST", pattern: new RegExp(`${BASE.source}$`), handler: (ctx) => run(ctx, true, () => create(ctx.body)) },
  { method: "PUT", pattern: new RegExp(`${BASE.source}/${CODE}$`), handler: (ctx) => run(ctx, false, () => update(ctx.params[0]!, ctx.body)) },
  {
    method: "POST",
    pattern: new RegExp(`${BASE.source}/${CODE}/(deactivate|reactivate)$`),
    handler: (ctx) => run(ctx, true, () => setActive(ctx.params[0]!, ctx.params[1] === "reactivate", ctx.body)),
  },
  { method: "POST", pattern: new RegExp(`${BASE.source}/${CODE}/delete$`), handler: (ctx) => run(ctx, true, () => remove(ctx.params[0]!)) },
];
