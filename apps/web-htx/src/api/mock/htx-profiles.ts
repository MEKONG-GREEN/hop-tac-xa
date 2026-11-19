import { uuidv7 } from "../idempotency";
import type { Cooperative } from "../types";
import { toScaled } from "@/lib/decimal";
import { ADMIN_UNITS, COOPERATIVES, COMMUNES, adminUnitSelectable, findCoop, type MockCooperative } from "./data";
import { householdMockRows } from "./htx-gaps";
import { hasRole, json, problem, validation, type Route } from "./http";
import { TENANT, type MockPersona } from "./personas";

const contacts = new Map<string, { name: string; phone: string; revision: number }>();
const nameKey = (name: string) => name.normalize("NFC").trim().replace(/\s+/g, " ").toLowerCase();
type Areas = { cultivated_area_ha?: string | null; livestock_area_ha?: string | null };

function sumArea(rows: Areas[], field: keyof Areas) {
  let value = 0n, missing = 0;
  for (const row of rows) {
    const area = row[field];
    const scaled = area == null ? null : toScaled(area);
    if (scaled === null) missing++; else value += scaled;
  }
  const fraction = (value % 1_000_000n).toString().padStart(6, "0").replace(/0+$/, "");
  return { missing, value: missing ? null : `${value / 1_000_000n}${fraction ? `.${fraction}` : ""}` };
}

export function withHTXSummary(coop: MockCooperative): Cooperative {
  const rows = householdMockRows(coop);
  const cultivated = sumArea(rows, "cultivated_area_ha"), livestock = sumArea(rows, "livestock_area_ha");
  return { ...coop.api, machine_count: coop.api.machine_count ?? null, machine_groups: coop.api.machine_groups ?? null,
    household_areas: { cultivated_ha: cultivated.value, livestock_ha: livestock.value, households: rows.length, missing_cultivated: cultivated.missing, missing_livestock: livestock.missing } };
}

function adminMfa(persona: MockPersona) {
  return hasRole(persona, "platform_admin") && !persona.amr.includes("otp") ? problem(403, "mfa_required", "MFA required") : null;
}

// Chạy trước receipt cache: thay vai/HTX không được phát lại quyền ghi cũ.
export function htxProfileWriteProblem(persona: MockPersona, method: string, path: string): Response | null {
  if (method !== "POST") return null;
  if (path === "/v1/cooperative-profiles") return !hasRole(persona, "platform_admin") ? problem(403, "forbidden", "Chỉ Admin được thêm HTX") : adminMfa(persona);
  const match = /^\/v1\/cooperatives\/([\w-]+)\/profile\/revisions$/.exec(path);
  if (!match) return null;
  if (hasRole(persona, "platform_admin")) return adminMfa(persona);
  if (!hasRole(persona, "coop_manager")) return problem(403, "forbidden", "Không có quyền sửa HTX");
  if (persona.me.org?.ref !== match[1]) return problem(404, "party.org_not_found", "Không tìm thấy HTX");
  return null;
}

function checkedBody(body: unknown, edit: boolean): Record<string, unknown> | Response {
  if (!body || typeof body !== "object" || Array.isArray(body)) return problem(400, "bad_request", "Hồ sơ không hợp lệ");
  const input = body as Record<string, unknown>;
  const keys = ["name", "commune_code", "head_office", "representative_name", "phone", ...(edit ? ["expected_revision"] : [])];
  if (Object.keys(input).some((key) => !keys.includes(key))) return problem(400, "bad_request", "Có trường không được sửa");
  for (const [key, max] of [["name", 200], ["commune_code", 40], ["head_office", 500]] as const) {
    if (typeof input[key] !== "string" || !input[key].trim() || [...input[key]].length > max) return validation([{ field: key, reason: `Bắt buộc, tối đa ${max} ký tự` }]);
  }
  if (input.representative_name !== undefined && (typeof input.representative_name !== "string" || [...input.representative_name].length > 200)) return validation([{ field: "representative_name", reason: "Tối đa 200 ký tự" }]);
  if (typeof input.phone !== "string" || !/^0[0-9]{9}$/.test(input.phone.trim())) return validation([{ field: "phone", reason: "SĐT gồm 10 chữ số, bắt đầu bằng 0" }]);
  if (edit && (!Number.isInteger(input.expected_revision) || Number(input.expected_revision) < 1)) return validation([{ field: "expected_revision", reason: "Cần phiên bản hồ sơ" }]);
  return input;
}

export const htxProfileAppRoutes: Route[] = [
  { method: "GET", pattern: /^\/v1\/cooperatives\/([\w-]+)\/profile$/, handler: ({ params, persona }) => {
    if (!hasRole(persona, "platform_admin", "coop_manager", "htx_member")) return problem(403, "forbidden", "Không có quyền xem hồ sơ");
    const mfa = adminMfa(persona); if (mfa) return mfa;
    const coop = findCoop(params[0]);
    if (persona.me.tenant_id !== TENANT || !coop || (!hasRole(persona, "platform_admin") && (persona.me.org?.ref !== coop.api.ref || !coop.api.active))) return problem(404, "party.org_not_found", "Không tìm thấy HTX");
    const contact = contacts.get(coop.api.ref) ?? { name: "", phone: "", revision: 1 };
    const full = hasRole(persona, "platform_admin", "coop_manager");
    return json({ ...withHTXSummary(coop), revision: contact.revision, can_edit: full && coop.api.active, contact_masked: !full, contact_configured: contacts.has(coop.api.ref),
      representative_name: full ? contact.name : contact.name.split(/\s+/).filter(Boolean).map((word) => `${[...word][0]}.`).join(" "),
      phone: full || !contact.phone ? contact.phone : `${contact.phone.slice(0, 4)}•••${contact.phone.slice(-3)}` }, 200, { "Cache-Control": "no-store" });
  } },
  { method: "POST", pattern: /^\/v1\/cooperatives\/([\w-]+)\/profile\/revisions$/, handler: ({ params, persona, body, url }) => {
    const denied = htxProfileWriteProblem(persona, "POST", url.pathname); if (denied) return denied;
    const coop = findCoop(params[0]); if (!coop || persona.me.tenant_id !== TENANT) return problem(404, "party.org_not_found", "Không tìm thấy HTX");
    if (!coop.api.active) return problem(409, "party.org_state_conflict", "HTX đã ngừng hoạt động");
    const input = checkedBody(body, true); if (input instanceof Response) return input;
    const contact = contacts.get(coop.api.ref) ?? { name: "", phone: "", revision: 1 };
    if (input.expected_revision !== contact.revision) return problem(409, "party.cooperative_profile_changed", "Hồ sơ đã thay đổi");
    if (COOPERATIVES.some((other) => other.api.ref !== coop.api.ref && nameKey(other.api.name) === nameKey(String(input.name)))) return problem(409, "party.cooperative_name_taken", "Tên HTX đã tồn tại", { detail: "Tên HTX đã tồn tại" });
    const unit = ADMIN_UNITS.find((unit) => unit.code === input.commune_code && unit.level === "commune");
    if (!unit || (unit.code !== coop.api.admin_unit_code && !adminUnitSelectable(unit))) return validation([{ field: "commune_code", reason: "Chọn xã đang sử dụng" }]);
    const province = ADMIN_UNITS.find((parent) => parent.code === unit.parent_code);
    if (coop.api.admin_unit_code !== unit.code) coop.api.parent_ref = null;
    coop.api.name = String(input.name).trim(); coop.api.admin_unit_code = unit.code; coop.api.address = String(input.head_office).trim();
    coop.province_code = unit.parent_code ?? ""; coop.province_name = province?.name ?? ""; coop.commune_name = unit.name;
    coop.api.province_code = coop.province_code; coop.api.province_name = coop.province_name; coop.api.commune_name = unit.name;
    contacts.set(coop.api.ref, { name: String(input.representative_name ?? "").trim(), phone: String(input.phone).trim(), revision: contact.revision + 1 });
    return json({ ref: coop.api.ref, code: coop.api.code, revision: contact.revision + 1 }, 200, { "Cache-Control": "no-store" });
  } },
];

export const htxProfileAdminRoutes: Route[] = [
  { method: "POST", pattern: /^\/v1\/cooperative-profiles$/, handler: ({ persona, body, url }) => {
    const denied = htxProfileWriteProblem(persona, "POST", url.pathname); if (denied) return denied;
    if (persona.me.tenant_id !== TENANT) return problem(403, "forbidden", "Ngoài phạm vi");
    const input = checkedBody(body, false); if (input instanceof Response) return input;
    if (COOPERATIVES.some((coop) => nameKey(coop.api.name) === nameKey(String(input.name)))) return problem(409, "party.cooperative_name_taken", "Tên HTX đã tồn tại", { detail: "Tên HTX đã tồn tại" });
    const unit = ADMIN_UNITS.find((unit) => unit.code === input.commune_code && unit.level === "commune" && adminUnitSelectable(unit));
    if (!unit) return validation([{ field: "commune_code", reason: "Chọn xã đang sử dụng" }]);
    const province = ADMIN_UNITS.find((parent) => parent.code === unit.parent_code), ref = uuidv7();
    let sequence = 1; while (COOPERATIVES.some((coop) => coop.api.code === `HTX${String(sequence).padStart(6, "0")}`)) sequence++;
    const code = `HTX${String(sequence).padStart(6, "0")}`;
    const lnglat = COMMUNES.find((commune) => commune.code === unit.code)?.center ?? COOPERATIVES[0]!.lnglat;
    const coop: MockCooperative = { province_code: province?.code ?? "", province_name: province?.name ?? "", commune_name: unit.name, member_households: 0, plots: 0, lnglat,
      api: { ref, kind: "cooperative", code, name: String(input.name).trim(), admin_unit_code: unit.code, parent_ref: null, representative_ref: null, active: true,
        status_reason: null, tax_code: null, area_ha: null, established_on: null, location: null, location_provisional: true, address: String(input.head_office).trim(),
        confirmation_status: "unconfirmed", confirmation_label: "Chưa xác nhận", province_code: province?.code ?? null, province_name: province?.name ?? null, commune_name: unit.name, member_households: 0, digitized_plots: 0 } };
    COOPERATIVES.push(coop);
    contacts.set(ref, { name: String(input.representative_name ?? "").trim(), phone: String(input.phone).trim(), revision: 1 });
    return json({ ref, code, revision: 1 }, 201, { "Cache-Control": "no-store" });
  } },
];
