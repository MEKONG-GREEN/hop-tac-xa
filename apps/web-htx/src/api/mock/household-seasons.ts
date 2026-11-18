import type { components } from "../generated/application";
import { uuidv7 } from "../idempotency";
import { findCoop } from "./data";
import { householdMockSOPs } from "./cropcycle";
import { householdMockRows } from "./htx-gaps";
import { allocateHouseholdMock, scaledArea } from "./household-season-state";
import { hasRole, json, nowIso, paginate, problem, validation, type Route } from "./http";

type Season = components["schemas"]["HouseholdSeason"];
const seasons: Season[] = [];
export const householdSeasonRoutes: Route[] = [{
  method: "GET", pattern: /^\/v1\/cooperatives\/([\w-]+)\/households\/([\w-]+)\/seasons(?:\/([\w-]+))?$/,
  handler: ({ params, persona, url }) => {
    if (!hasRole(persona, "platform_admin", "htx_member", "coop_manager") || (!hasRole(persona, "platform_admin") && persona.me.org?.ref !== params[0])) return problem(404, "cropcycle.household_not_found", "Không tìm thấy nông hộ");
    const rows = seasons.filter((s) => s.cooperative_ref === params[0] && s.household_ref === params[1]);
    if (params[2]) return rows.find((s) => s.season_ref === params[2]) ? json(rows.find((s) => s.season_ref === params[2])) : problem(404, "cropcycle.season_not_found", "Không tìm thấy vụ");
    return json(paginate(rows, url));
  },
}, {
  method: "POST", pattern: /^\/v1\/cooperatives\/([\w-]+)\/households\/([\w-]+)\/seasons$/,
  handler: ({ params, persona, body }) => {
    if (!hasRole(persona, "platform_admin") && (!hasRole(persona, "htx_member") || hasRole(persona, "coop_manager"))) return problem(403, "cropcycle.read_only", "Vai của bạn chỉ được xem");
    const coop = findCoop(params[0]!);
    if (!coop || (!hasRole(persona, "platform_admin") && persona.me.org?.ref !== coop.api.ref)) return problem(404, "cropcycle.household_not_found", "Không tìm thấy nông hộ");
    const household = householdMockRows(coop).find((h) => h.household_ref === params[1]);
    if (!household) return problem(404, "cropcycle.household_not_found", "Không tìm thấy nông hộ");
    const b = body as components["schemas"]["HouseholdSeasonOpenInput"];
    if (typeof b.area_ha !== "string" || !/^(0|[1-9][0-9]{0,9})(\.[0-9]{1,2})?$/.test(b.area_ha) || scaledArea(b.area_ha) <= 0n) return validation([{ field: "area_ha", reason: "Diện tích dương, tối đa hai chữ số thập phân" }]);
    if (household.remaining_area_ha == null) return problem(422, "cropcycle.household_land_unknown", "Chưa có diện tích đất");
    if (scaledArea(b.area_ha) > scaledArea(household.remaining_area_ha)) return problem(409, "cropcycle.household_area_exceeded", "Diện tích vượt", { remaining_area_ha: household.remaining_area_ha });
    const choices = householdMockSOPs(coop.api.ref, b.crop_code, b.variety_code);
    if (!choices.length) return problem(422, "cropcycle.no_active_sop_for_crop", "Chưa có SOP đang áp dụng; liên hệ Admin");
    const chosen = b.sop_version_id ? choices.find((s) => s.version.id === b.sop_version_id) : choices.length === 1 ? choices[0] : undefined;
    if (!chosen) return validation([{ field: "sop_version_id", reason: "Chọn SOP đang áp dụng phù hợp" }]);
    const s: Season = {
      season_ref: uuidv7(), entry_kind: "household", household_ref: household.household_ref, cooperative_ref: coop.api.ref, commune_code: household.commune_code,
      crop_code: b.crop_code, season_code: b.season_code, season_year: b.season_year, variety_code: b.variety_code, area_ha: b.area_ha, anchor_date: b.anchor_date,
      expected_harvest_date: null, harvest_open_date: null, status: "open", current_event_id: uuidv7(), version: 1, opened_by: persona.me.actor_ref, opened_at: nowIso(),
      gps: b.gps ?? { source: "denied" }, gps_missing: b.gps?.lon == null || b.gps?.lat == null,
      sop_snapshot: structuredClone({ template_ref: chosen.template.ref, version_id: chosen.version.id, version_no: chosen.version.version_no, crop_code: b.crop_code, name: chosen.template.name, anchor_name: chosen.template.anchor_name, stages: chosen.version.stages, steps: chosen.version.steps }),
    };
    seasons.push(s); allocateHouseholdMock(coop.api.ref, household.household_ref, b.area_ha);
    return json({ season: s }, 201);
  },
}];
