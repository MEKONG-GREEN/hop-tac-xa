import type { components } from "../generated/admin";
import { uuidv7 } from "../idempotency";
import { headerKey, parseCsv } from "./admin-ops";
import { ADMIN_UNITS, COOPERATIVES, adminUnitSelectable } from "./data";
import { hasRole, json, problem, validation, type Route } from "./http";

type ManagedUnit = components["schemas"]["ManagedAdminUnit"];
type ImportResult = components["schemas"]["CatalogImportResult"];
const revisions = new Map<string, number>();
const nameKey = (name: string) => name.trim().replace(/\s+/g, " ").toLowerCase();
function view(u: (typeof ADMIN_UNITS)[number]): ManagedUnit {
  return { code: u.code, name: u.name, level: u.level as "province" | "commune", parent_code: u.parent_code ?? "", valid_from: u.valid_from, active: u.active !== false, selectable: adminUnitSelectable(u), revision: revisions.get(u.code) ?? 0, commune_count: ADMIN_UNITS.filter((c) => c.level === "commune" && c.parent_code === u.code).length, cooperative_count: u.level === "commune" ? COOPERATIVES.filter((c) => c.api.admin_unit_code === u.code).length : null };
}
function insert(name: string, level: "province" | "commune", parent: string, active: boolean) {
  const code = `${level === "province" ? "P" : "X"}-${uuidv7().replace(/-/g, "").toUpperCase()}`;
  const unit = { code, name: name.trim().replace(/\s+/g, " "), level, parent_code: parent || null, active, valid_from: new Date().toLocaleDateString("en-CA", { timeZone: "Asia/Ho_Chi_Minh" }), valid_to: null, successor_codes: [] };
  ADMIN_UNITS.push(unit); revisions.set(code, 1); return view(unit);
}
function check(name: unknown, level: unknown, parent: unknown): string | null {
  if (typeof name !== "string" || !nameKey(name) || [...name].length > 200) return "Tên bắt buộc, tối đa 200 ký tự";
  if (level !== "province" && level !== "commune") return "Cấp phải là Tỉnh hoặc Xã";
  if (level === "province" && parent) return "Tỉnh không có đơn vị cấp trên";
  if (level === "commune" && !ADMIN_UNITS.some((u) => u.code === parent && u.level === "province" && adminUnitSelectable(u))) return "Chọn tỉnh / thành phố đang sử dụng";
  return null;
}

export const adminUnitRoutes: Route[] = [
  { method: "GET", pattern: /^\/v1\/masterdata\/admin-unit-profiles$/, handler: ({ persona, url }) => {
    if (!hasRole(persona, "platform_admin")) return problem(403, "forbidden", "Chỉ Admin quản lý danh mục");
    const parent = url.searchParams.get("parent") ?? "", query = nameKey(url.searchParams.get("q") ?? ""), state = url.searchParams.get("state"), after = url.searchParams.get("after") ?? "", limit = Number(url.searchParams.get("limit") ?? 50);
    if (!Number.isInteger(limit) || limit < 1 || limit > 200 || (state && !["active", "inactive"].includes(state))) return validation([{ field: "query", reason: "Bộ lọc không hợp lệ" }]);
    const rows = ADMIN_UNITS.filter((u) => u.level === (parent ? "commune" : "province") && (u.parent_code ?? "") === parent && u.code > after).map(view).filter((u) => (!query || nameKey(`${u.code} ${u.name}`).includes(query)) && (!state || u.active === (state === "active"))).sort((a, b) => a.code.localeCompare(b.code));
    return json({ items: rows.slice(0, limit), next_cursor: rows.length > limit ? rows[limit - 1]!.code : null });
  } },
  { method: "POST", pattern: /^\/v1\/masterdata\/admin-unit-profiles(?:\/([0-9A-Z.-]+)\/revisions)?$/, handler: ({ persona, params, body }) => {
    if (!hasRole(persona, "platform_admin")) return problem(403, "forbidden", "Chỉ Admin quản lý danh mục");
    if (!body || typeof body !== "object" || Array.isArray(body)) return validation([{ field: "body", reason: "Dữ liệu không hợp lệ" }]);
    const b = body as Record<string, unknown>, ref = params[0];
    if (b.active !== undefined && typeof b.active !== "boolean") return validation([{ field: "active", reason: "Trạng thái không hợp lệ" }]);
    const allowed = ref ? ["name", "active", "expected_revision"] : ["name", "active", "level", "parent_code"];
    if (Object.keys(b).some((k) => !allowed.includes(k))) return problem(400, "bad_request", "Không sửa mã, cấp hoặc tỉnh cha");
    const current = ref ? ADMIN_UNITS.find((u) => u.code === ref) : undefined;
    if (current?.level === "district") return problem(404, "masterdata.admin_unit_not_found", "Đơn vị lịch sử chỉ đọc");
    if (ref && !current) return problem(404, "masterdata.admin_unit_not_found", "Không tìm thấy đơn vị");
    if (current && b.expected_revision !== (revisions.get(current.code) ?? 0)) return problem(409, "masterdata.admin_unit_changed", "Đơn vị đã thay đổi, tải lại");
    const level = current?.level ?? b.level, parent = current?.parent_code ?? b.parent_code ?? "";
    const invalid = current ? (typeof b.name !== "string" || !nameKey(b.name) || [...b.name].length > 200 ? "Tên không hợp lệ" : null) : check(b.name, level, parent);
    if (invalid || (current && typeof b.active !== "boolean")) return validation([{ field: "name", reason: invalid ?? "Thiếu trạng thái" }]);
    if (current && current.active === false && b.active === true) { const error = check(b.name, level, parent); if (error) return validation([{ field: "parent_code", reason: error }]); }
    if (ADMIN_UNITS.some((u) => u !== current && u.level === level && (u.parent_code ?? "") === parent && nameKey(u.name) === nameKey(String(b.name)))) return problem(409, "masterdata.admin_unit_name_taken", "Tên đã có trong cùng đơn vị cấp trên");
    if (current) { current.name = String(b.name).trim().replace(/\s+/g, " "); current.active = Boolean(b.active); revisions.set(current.code, (revisions.get(current.code) ?? 0) + 1); return json(view(current)); }
    return json(insert(String(b.name), level as "province" | "commune", String(parent), b.active !== false), 201);
  } },
  { method: "GET", pattern: /^\/v1\/masterdata\/admin-unit-template$/, handler: ({ persona }) => {
    if (!hasRole(persona, "platform_admin")) return problem(403, "forbidden", "Chỉ Admin tải mẫu");
    return new Response("\uFEFFTên,Cấp,Tỉnh / thành phố trực thuộc\r\n", { headers: { "Content-Type": "text/csv; charset=utf-8", "Content-Disposition": 'attachment; filename="don-vi-hanh-chinh-gia-lap.csv"' } });
  } },
  { method: "POST", pattern: /^\/v1\/masterdata\/admin-unit-imports$/, handler: ({ persona, body, request }) => {
    if (!hasRole(persona, "platform_admin")) return problem(403, "forbidden", "Chỉ Admin nhập danh mục");
    if (!request.headers.get("Content-Type")?.startsWith("text/csv") || typeof body !== "string") return problem(422, "masterdata.admin_unit_import_invalid", "Giả lập nhận CSV theo file mẫu; API thật hỗ trợ Excel .xlsx");
    const rows = parseCsv(body), header = rows.shift()?.map((h) => headerKey(h).replace(/[^a-z0-9]+/g, "_").replace(/^_|_$/g, ""));
    if (!header || header.length !== 3 || !["ten", "cap", "tinh_thanh_pho_truc_thuoc"].every((h) => header.includes(h)) || !rows.length || rows.length > 500 || new TextEncoder().encode(body).length > 2 * 1024 * 1024) return problem(422, "masterdata.admin_unit_import_invalid", "Tệp không đúng mẫu, tối đa 500 dòng và 2 MB");
    const result: ImportResult = { import_id: uuidv7(), total: rows.length, created: 0, skipped: 0, rejected: 0, replayed: false, rows: [] };
    rows.forEach((row, index) => {
      const name = row[header.indexOf("ten")] ?? "", rawLevel = row[header.indexOf("cap")] ?? "", parentName = row[header.indexOf("tinh_thanh_pho_truc_thuoc")] ?? "";
      const level = ["tỉnh", "tinh"].includes(nameKey(rawLevel)) ? "province" : ["xã", "xa"].includes(nameKey(rawLevel)) ? "commune" : "";
      const parent = level === "commune" ? ADMIN_UNITS.find((u) => u.level === "province" && nameKey(u.name) === nameKey(parentName) && adminUnitSelectable(u))?.code ?? "" : parentName.trim();
      const error = check(name, level, parent) ?? (ADMIN_UNITS.some((u) => u.level === level && (u.parent_code ?? "") === parent && nameKey(u.name) === nameKey(name)) ? "Tên đã có trong cùng đơn vị cấp trên" : null);
      if (error) { result.rejected++; result.rows.push({ row: index + 2, status: "rejected", reason: error }); }
      else { const created = insert(name, level as "province" | "commune", parent, true); result.created++; result.rows.push({ row: index + 2, status: "created", code: created.code }); }
    });
    return json(result);
  } },
];
