import type { components } from "../generated/application";
import { uuidv7 } from "../idempotency";
import { findCoop } from "./data";
import { householdMockSOPs } from "./cropcycle";
import { householdMockRows } from "./htx-gaps";
import { allocateHouseholdMock, scaledArea } from "./household-season-state";
import { hasRole, json, nowIso, paginate, problem, validation, type Route } from "./http";

type Season = components["schemas"]["HouseholdSeason"];
type TaskRecord = components["schemas"]["HouseholdTaskRecord"];
const seasons: Season[] = [];
const taskRecords = new Map<string, TaskRecord[]>();

function mockChecklist(s: Season, write: boolean): components["schemas"]["HouseholdChecklist"] {
  const plan = mockPlan(s.sop_snapshot, s.anchor_date, s.current_event_id);
  const records = taskRecords.get(s.season_ref) ?? [];
  let total = 0, done = 0;
  const last = Math.max(...plan.stages.map((s) => s.seq));
  const stages = plan.stages.map((stage) => {
    let resolved = 0;
    const tasks = stage.steps.map((task) => {
      total++;
      const record = records.find((r) => r.step_seq === task.seq && r.state === "done");
      let status: components["schemas"]["HouseholdTaskProgress"]["completion_status"] = task.status;
      if (record) { done++; resolved++; status = task.undated ? "done_on_time" : !task.from_date ? "done" : record.done_on < task.from_date ? "done_early" : task.to_date && record.done_on > task.to_date ? "done_late" : "done_on_time"; }
      return { ...task, completion_status: status, done_on: record?.done_on ?? null, can_complete: write && !record && (s.status === "open" || (s.status === "completed" && stage.seq === last)) };
    });
    const status: "done" | "in_progress" | "not_started" = tasks.length && resolved === tasks.length ? "done" : resolved ? "in_progress" : "not_started";
    return { seq: stage.seq, name: stage.name, status, tasks };
  });
  const scaled = total ? (BigInt(done) * 10000n + BigInt(total) / 2n) / BigInt(total) : null;
  return { season_ref: s.season_ref, current_event_id: s.current_event_id, records, progress: { as_of_date: plan.as_of_date, total, done, skipped: 0, percent: scaled == null ? null : `${scaled / 100n}.${(scaled % 100n).toString().padStart(2, "0")}`, stages } };
}

function mockPlan(snapshot: Season["sop_snapshot"], anchor: string, event: string | null): components["schemas"]["SeasonSchedule"] {
  const today = new Intl.DateTimeFormat("en-CA", { timeZone: "Asia/Ho_Chi_Minh" }).format(new Date(nowIso()));
  const add = (base: string | null, offset: number | null | undefined) => {
    if (!base || offset == null) return null;
    const d = new Date(`${base}T00:00:00Z`); d.setUTCDate(d.getUTCDate() + offset);
    return d.toISOString().slice(0, 10);
  };
  const dates = (s: Season["sop_snapshot"]["steps"][number], harvest: string | null): [string | null, string | null] => {
    const w = s.window;
    if (!w || w.kind === "none") return [null, null];
    if (w.kind === "stage") {
      const stage = snapshot.stages.find((stage) => stage.seq === s.stage_seq);
      return [add(anchor, stage?.from_day), add(anchor, stage?.to_day)];
    }
    const base = w.anchor === "nxg" ? anchor : w.anchor === "nth" ? harvest : null;
    return [add(base, w.from), w.kind === "open" ? null : add(base, w.to)];
  };
  const lastHarvest = snapshot.steps.filter((s) => s.activity_code === "thu_hoach").sort((a, b) => b.seq - a.seq)[0];
  const harvest = lastHarvest ? dates(lastHarvest, null)[0] : null;
  return { anchor_date: anchor, as_of_date: today, expected_harvest_date: harvest,
    sop_version_id: snapshot.version_id, current_event_id: event,
    stages: snapshot.stages.map((stage) => ({ seq: stage.seq, name: stage.name, from_date: add(anchor, stage.from_day), to_date: add(anchor, stage.to_day),
      steps: snapshot.steps.filter((s) => s.stage_seq === stage.seq).map((s) => {
        const [from, to] = dates(s, harvest);
        const undated = s.window?.kind === "none";
        return { seq: s.seq, stage_seq: stage.seq, task_seq: s.task_seq ?? s.seq, name: s.name, activity_code: s.activity_code,
          window_kind: s.window?.kind ?? "none", from_date: from, to_date: to, expected_date: from, undated,
          status: undated ? "due" : !from ? "waiting_anchor" : today < from ? "upcoming" : to && today > to ? "overdue" : "due" };
      }),
    })),
  };
}

export const householdSeasonRoutes: Route[] = [{
  method: "GET", pattern: /^\/v1\/cooperatives\/([\w-]+)\/households\/([\w-]+)\/seasons\/([\w-]+)\/checklist$/,
  handler: ({ params, persona }) => {
    const s = seasons.find((s) => s.cooperative_ref === params[0] && s.household_ref === params[1] && s.season_ref === params[2]);
    if (!s || (!hasRole(persona, "platform_admin") && persona.me.org?.ref !== s.cooperative_ref)) return problem(404, "cropcycle.season_not_found", "Không tìm thấy vụ");
    const write = hasRole(persona, "platform_admin") || (hasRole(persona, "htx_member") && !hasRole(persona, "coop_manager"));
    if (!write && !hasRole(persona, "coop_manager")) return problem(403, "forbidden", "Không có quyền đọc");
    return json(mockChecklist(s, write));
  },
}, {
  method: "POST", pattern: /^\/v1\/cooperatives\/([\w-]+)\/households\/([\w-]+)\/seasons\/([\w-]+)\/task-completions$/,
  handler: ({ params, persona, body }) => {
    if (!hasRole(persona, "platform_admin") && (!hasRole(persona, "htx_member") || hasRole(persona, "coop_manager"))) return problem(403, "cropcycle.read_only", "Vai của bạn chỉ được xem");
    const s = seasons.find((s) => s.cooperative_ref === params[0] && s.household_ref === params[1] && s.season_ref === params[2]);
    if (!s || (!hasRole(persona, "platform_admin") && persona.me.org?.ref !== s.cooperative_ref)) return problem(404, "cropcycle.season_not_found", "Không tìm thấy vụ");
    const b = body as components["schemas"]["HouseholdTaskCompleteInput"];
    if (b.expected_season_event_id !== s.current_event_id) return problem(409, "cropcycle.task_season_changed", "Vụ đã thay đổi");
    const plan = mockChecklist(s, true);
    const task = plan.progress.stages.flatMap((s) => s.tasks).find((t) => t.seq === b.step_seq);
    if (!task) return validation([{ field: "step_seq", reason: "Việc không thuộc snapshot" }]);
    if (task.done_on) return problem(409, "cropcycle.task_already_resolved", "Việc đã xong");
    if (!task.can_complete) return problem(422, "cropcycle.task_season_state", "Vụ hoặc giai đoạn không cho ghi");
    const step = s.sop_snapshot.steps.find((t) => t.seq === b.step_seq)!;
    const inputs = (step.materials ?? []).map((m) => {
      const value = scaledArea(m.dose_per_ha) * scaledArea(s.area_ha);
      const qty = `${value / 100000000n}.${(value % 100000000n).toString().padStart(8, "0")}`.replace(/\.?0+$/, "");
      return { name: m.name || m.material_code || "Vật tư", qty, unit: m.unit };
    });
    const id = uuidv7(); const record: TaskRecord = { id, log_ref: id, step_seq: b.step_seq, state: "done", done_on: plan.progress.as_of_date,
      sop_version_id: s.sop_snapshot.version_id, inputs, gps_missing: b.gps?.lon == null || b.gps?.lat == null, gps: b.gps ?? { source: "no_device" }, actor_ref: persona.me.actor_ref, event_time: nowIso(), recorded_at: nowIso() };
    taskRecords.set(s.season_ref, [...plan.records, record]);
    return json({ record, checklist: mockChecklist(s, true) }, 201);
  },
}, {
  method: "POST", pattern: /^\/v1\/cooperatives\/([\w-]+)\/households\/([\w-]+)\/season-plan-preview$/,
  handler: ({ params, persona, body }) => {
    if (!hasRole(persona, "platform_admin") && (!hasRole(persona, "htx_member") || hasRole(persona, "coop_manager"))) return problem(403, "cropcycle.read_only", "Vai của bạn chỉ được xem");
    const coop = findCoop(params[0]!);
    if (!coop || (!hasRole(persona, "platform_admin") && persona.me.org?.ref !== coop.api.ref) || !householdMockRows(coop).some((h) => h.household_ref === params[1])) return problem(404, "cropcycle.household_not_found", "Không tìm thấy nông hộ");
    const b = body as components["schemas"]["SeasonSchedulePreviewInput"];
    const chosen = householdMockSOPs(coop.api.ref, b.crop_code, b.variety_code).find((s) => s.version.id === b.sop_version_id);
    if (!chosen || !b.variety_code || typeof b.area_ha !== "string" || !/^(0|[1-9][0-9]{0,9})(\.[0-9]{1,2})?$/.test(b.area_ha) || scaledArea(b.area_ha) <= 0n || !/^\d{4}-\d{2}-\d{2}$/.test(b.anchor_date) || !Number.isFinite(Date.parse(b.anchor_date))) return validation([{ field: "sop_version_id", reason: "Chọn giống, SOP, diện tích và mốc gốc hợp lệ" }]);
    return json(mockPlan({ template_ref: chosen.template.ref, version_id: chosen.version.id, version_no: chosen.version.version_no, crop_code: b.crop_code, anchor_name: chosen.template.anchor_name, stages: chosen.version.stages, steps: chosen.version.steps }, b.anchor_date, null));
  },
}, {
  method: "GET", pattern: /^\/v1\/cooperatives\/([\w-]+)\/households\/([\w-]+)\/seasons\/([\w-]+)\/plan$/,
  handler: ({ params, persona }) => {
    if (!hasRole(persona, "platform_admin", "htx_member", "coop_manager")) return problem(403, "forbidden", "Không có quyền xem kế hoạch");
    const s = seasons.find((s) => s.cooperative_ref === params[0] && s.household_ref === params[1] && s.season_ref === params[2]);
    if (!s || (!hasRole(persona, "platform_admin") && persona.me.org?.ref !== s.cooperative_ref)) return problem(404, "cropcycle.season_not_found", "Không tìm thấy vụ");
    return json(mockPlan(s.sop_snapshot, s.anchor_date, s.current_event_id));
  },
}, {
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
