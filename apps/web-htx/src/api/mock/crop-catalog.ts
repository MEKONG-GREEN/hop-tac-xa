// GIẢ LẬP quản lý loại cây và giống (AH-2, BRD FN-01) theo hợp đồng Admin /v1/masterdata/catalogs/{crop|rice_variety}/items:
// thêm (mã do máy chủ sinh), sửa, ẩn/hiện, xóa (chưa dùng → xóa, đang dùng → chỉ ẩn). Cùng luật với máy chủ:
// vai masterdata_admin/platform_admin, so trùng tên theo nameKey trong phạm vi, thời gian sinh trưởng số nguyên > 0,
// loại cây ẩn / còn giống đang hiện bị chặn, lý do ẩn không bắt buộc. Gọi lặp cùng Idempotency-Key phát lại kết quả.
import { uuidv7 } from "../idempotency";
import type { CatalogItem } from "../types";
import { varietyReferenced } from "./cropcycle";
import { CATALOGS } from "./data";
import { hasRole, json, nowIso, problem, validation, type Ctx, type Route } from "./http";

const SYSTEM_HIDE = "Ẩn tại màn Danh mục cây trồng & giống";
const GROWTH_DAYS_MSG = "Thời gian sinh trưởng phải là số nguyên lớn hơn 0 (ngày)";

type Out = { status: number; body: unknown } | Response;
type Catalog = "crop" | "rice_variety";

const REPLAYS = new Map<string, { status: number; body: unknown }>();

const nameKey = (s: string) => s.trim().split(/\s+/).filter(Boolean).join(" ").toLowerCase();
const isType = (c: CatalogItem) => !c.attrs.parent_code;
const list = (catalog: Catalog): CatalogItem[] => CATALOGS[catalog];
const find = (catalog: Catalog, code: string) => list(catalog).find((i) => i.code === code);
const nameTaken = (r: Out) => r instanceof Response;

function run(ctx: Ctx, replayable: boolean, compute: () => Out): Response {
  if (!hasRole(ctx.persona, "masterdata_admin", "platform_admin")) return problem(403, "forbidden", "Forbidden");
  const key = replayable ? ctx.request.headers.get("Idempotency-Key") : null;
  const hit = key ? REPLAYS.get(key) : undefined;
  if (hit) return json(hit.body, hit.status);
  const out = compute();
  if (nameTaken(out)) return out;
  if (key) REPLAYS.set(key, out);
  return json(out.body, out.status);
}

const dupName = () => problem(409, "masterdata.catalog_item_name_taken", "Name taken", { errors: [{ field: "name", reason: "Tên đã tồn tại" }] });
const readOnly = () => problem(409, "masterdata.catalog_item_read_only", "Read only");

interface Input {
  code?: unknown;
  name?: unknown;
  attrs?: Record<string, unknown>;
}

/** Kiểm dữ liệu vào; trả attrs đã chuẩn hóa (chỉ thuộc tính màn mới gửi) hoặc lỗi 422. */
function check(catalog: Catalog, input: Input, cur?: CatalogItem): { name: string; attrs: Record<string, string> } | Response {
  const errors: { field: string; reason: string }[] = [];
  const name = typeof input.name === "string" ? input.name.trim() : "";
  if (!name || name.length > 200) errors.push({ field: "name", reason: "bắt buộc, tối đa 200 ký tự" });
  const raw = input.attrs ?? {};
  const attrs: Record<string, string> = {};
  if (catalog === "crop") {
    if ("parent_code" in raw) errors.push({ field: "attrs.parent_code", reason: "Loại cây không có nhóm cha" });
    const rice = raw.is_rice;
    if (rice === undefined) errors.push({ field: "attrs.is_rice", reason: "bắt buộc" });
    else if (rice !== "true" && rice !== "false") errors.push({ field: "attrs.is_rice", reason: "phải là một trong true, false" });
    else attrs.is_rice = rice;
  } else {
    const crop = typeof raw.crop_code === "string" ? raw.crop_code : "";
    // Giống đã ẩn không đổi loại cây thì vẫn sửa được dù loại cây đang ẩn (giống máy chủ).
    const hiddenSameCrop = !!cur && !cur.active && cur.attrs.crop_code === crop;
    const target = crop ? find("crop", crop) : undefined;
    if (!crop) errors.push({ field: "attrs.crop_code", reason: "bắt buộc" });
    else if (!target || !isType(target) || (!target.active && !hiddenSameCrop)) errors.push({ field: "attrs.crop_code", reason: "Loại cây không tồn tại hoặc đang ẩn" });
    else attrs.crop_code = crop;
    const days = typeof raw.growth_days === "string" ? raw.growth_days.trim() : "";
    if (!/^\d+$/.test(days) || Number(days) <= 0 || Number(days) > 2147483647) errors.push({ field: "attrs.growth_days", reason: GROWTH_DAYS_MSG });
    else attrs.growth_days = String(Number(days));
  }
  return errors.length ? validation(errors) : { name, attrs };
}

/** Trùng tên: loại cây trong các loại cây, giống trong cùng loại cây (kể cả đã ẩn). */
function taken(catalog: Catalog, name: string, attrs: Record<string, string>, except: string): boolean {
  const same = (i: CatalogItem) =>
    i.code !== except && nameKey(i.name) === nameKey(name) && (catalog === "crop" ? isType(i) : i.attrs.crop_code === attrs.crop_code);
  return list(catalog).some(same);
}

function create(catalog: Catalog, body: unknown): Out {
  const input = (body ?? {}) as Input;
  if (input.code) return validation([{ field: "code", reason: "Mã do hệ thống sinh — bỏ trống" }]);
  const ok = check(catalog, input);
  if (ok instanceof Response) return ok;
  if (taken(catalog, ok.name, ok.attrs, "")) return dupName();
  const item: CatalogItem = { catalog, code: uuidv7(), name: ok.name, attrs: ok.attrs, active: true, deactivation_reason: null, revision: 1, updated_at: nowIso() };
  list(catalog).push(item);
  return { status: 201, body: item };
}

function update(catalog: Catalog, code: string, body: unknown): Out {
  const cur = find(catalog, code);
  if (!cur) return problem(404, "masterdata.catalog_item_not_found", "Not found");
  if (catalog === "crop" && !isType(cur)) return readOnly();
  const ok = check(catalog, (body ?? {}) as Input, cur);
  if (ok instanceof Response) return ok;
  if (taken(catalog, ok.name, ok.attrs, code)) return dupName();
  // Thuộc tính cũ màn mới không gửi (năng suất, mô tả…) được giữ nguyên.
  cur.attrs = { ...cur.attrs, ...ok.attrs };
  cur.name = ok.name;
  cur.revision += 1;
  cur.updated_at = nowIso();
  return { status: 200, body: cur };
}

function visibleVarieties(cropCode: string): number {
  return list("rice_variety").filter((v) => v.attrs.crop_code === cropCode && v.active).length;
}

function blockedByVarieties(catalog: Catalog, item: CatalogItem): Response | null {
  if (catalog !== "crop") return null;
  const n = visibleVarieties(item.code);
  return n > 0 ? problem(409, "masterdata.crop_type_has_visible_varieties", "Visible varieties", { detail: `Loại cây còn ${n} giống đang hiển thị. Ẩn các giống đó trước.` }) : null;
}

function hide(item: CatalogItem, reason: string): void {
  item.active = false;
  item.deactivation_reason = reason;
  item.revision += 1;
  item.updated_at = nowIso();
}

function setActive(catalog: Catalog, code: string, active: boolean, body: unknown): Out {
  const cur = find(catalog, code);
  if (!cur) return problem(404, "masterdata.catalog_item_not_found", "Not found");
  if (catalog === "crop" && !isType(cur)) return readOnly();
  const reason = typeof (body as { reason?: unknown } | undefined)?.reason === "string" ? (body as { reason: string }).reason.trim() : "";
  if (reason && (reason.length < 10 || reason.length > 500)) return validation([{ field: "reason", reason: "lý do bắt buộc, 10–500 ký tự" }]);
  if (cur.active === active) return problem(409, "masterdata.catalog_item_state", "State");
  if (!active) {
    const blocked = blockedByVarieties(catalog, cur);
    if (blocked) return blocked;
    hide(cur, reason || SYSTEM_HIDE);
    return { status: 200, body: cur };
  }
  if (catalog === "rice_variety") {
    const crop = find("crop", cur.attrs.crop_code ?? "");
    if (crop && !crop.active) return problem(409, "masterdata.crop_type_hidden", "Crop hidden");
  }
  cur.active = true;
  cur.deactivation_reason = null;
  cur.revision += 1;
  cur.updated_at = nowIso();
  return { status: 200, body: cur };
}

/** Mục đang được dùng ở đâu đó (vụ, SOP; loại cây: có giống hoặc sâu bệnh trỏ tới) thì "xóa" chỉ ẩn. */
function inUse(catalog: Catalog, item: CatalogItem): boolean {
  if (catalog === "rice_variety") return varietyReferenced(item.code);
  const asPest = CATALOGS.pest.some((p) => (p.attrs.affected_crops ?? "").split(",").includes(item.code));
  return asPest || list("rice_variety").some((v) => v.attrs.crop_code === item.code);
}

function remove(catalog: Catalog, code: string): Out {
  const cur = find(catalog, code);
  if (!cur) return problem(404, "masterdata.catalog_item_not_found", "Not found");
  if (catalog === "crop" && !isType(cur)) return readOnly();
  const blocked = blockedByVarieties(catalog, cur);
  if (blocked) return blocked;
  if (inUse(catalog, cur)) {
    if (cur.active) hide(cur, SYSTEM_HIDE);
    return { status: 200, body: { outcome: "hidden", item: cur } };
  }
  const rows = list(catalog);
  rows.splice(rows.indexOf(cur), 1);
  return { status: 200, body: { outcome: "deleted", item: { ...cur, active: false } } };
}

const BASE = /^\/v1\/masterdata\/catalogs\/(crop|rice_variety)\/items/;
const CODE = "([A-Za-z0-9_.-]+)";

export const cropCatalogAdminRoutes: Route[] = [
  { method: "POST", pattern: new RegExp(`${BASE.source}$`), handler: (ctx) => run(ctx, true, () => create(ctx.params[0] as Catalog, ctx.body)) },
  { method: "PUT", pattern: new RegExp(`${BASE.source}/${CODE}$`), handler: (ctx) => run(ctx, false, () => update(ctx.params[0] as Catalog, ctx.params[1]!, ctx.body)) },
  {
    method: "POST",
    pattern: new RegExp(`${BASE.source}/${CODE}/(deactivate|reactivate)$`),
    handler: (ctx) => run(ctx, true, () => setActive(ctx.params[0] as Catalog, ctx.params[1]!, ctx.params[2] === "reactivate", ctx.body)),
  },
  { method: "POST", pattern: new RegExp(`${BASE.source}/${CODE}/delete$`), handler: (ctx) => run(ctx, true, () => remove(ctx.params[0] as Catalog, ctx.params[1]!)) },
];
