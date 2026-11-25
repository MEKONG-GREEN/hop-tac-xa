import type { components } from "../generated/application";
import { uuidv7 } from "../idempotency";
import { CATALOGS, findCoop } from "./data";
import type { MockPersona } from "./personas";
import { householdMockSOPs } from "./cropcycle";
import { householdMockRows } from "./htx-gaps";
import { allocateHouseholdMock, releaseHouseholdMock, scaledArea } from "./household-season-state";
import { initialEstimate, recomputeEstimate, tonnes, tonnesError, type YieldEstimate } from "./household-yield-estimate";
import { Decimal } from "@/lib/exactDecimal";
import { hasRole, json, nowIso, paginate, problem, problemOf, validation, type Route } from "./http";
import { applyPlanChange, applyPlanChanges, effectiveSteps, skippedSeqs, type PlanError } from "./season-plan-custom";

type Season = components["schemas"]["HouseholdSeason"];
type TaskRecord = components["schemas"]["HouseholdTaskRecord"];
const seasons: Season[] = [];
const taskRecords = new Map<string, TaskRecord[]>();
const journalDevices = new Map<string, string>();
const nativeJournalReceipts = new Map<string, string>();
const planHistory = new Map<string, { event_id: string; actor_ref: string; event_time: string; summary: string }[]>();
const planReceipts = new Map<string, { season: Season; summary: string }>();
// AH-126: các phiên bản của nhật ký việc theo log_ref (lịch sử chỉ ghi thêm).
const taskHistory = new Map<string, TaskRecord[]>();
const editReceipts = new Map<string, TaskRecord>();
const stopReceipts = new Map<string, Season>();
const estimateReceipts = new Map<string, { season: string; body: string; estimate: YieldEstimate }>();
const harvestReceipts = new Map<string, { body: string; result: components["schemas"]["HouseholdHarvestResult"] }>();
const planProblem = (e: PlanError) => e.field ? validation([{ field: e.field, reason: e.reason }]) : problem(e.status, e.code, e.reason);

const journalWriter = (p: MockPersona) => hasRole(p, "platform_admin") || (hasRole(p, "htx_member") && !hasRole(p, "coop_manager"));

function mockTaskDetail(p: MockPersona, coop: string, household: string, season: string, b: components["schemas"]["HouseholdTaskDetailInput"], id = uuidv7(), at = nowIso()): Response | { record: TaskRecord; checklist: components["schemas"]["HouseholdChecklist"] } {
  if (!journalWriter(p)) return problem(403, "cropcycle.read_only", "Vai của bạn chỉ được xem");
  const s = seasons.find((s) => s.cooperative_ref === coop && s.household_ref === household && s.season_ref === season);
  if (!s || (!hasRole(p, "platform_admin") && p.me.org?.ref !== coop)) return problem(404, "cropcycle.season_not_found", "Không tìm thấy vụ");
  const checklist = mockChecklist(s, true);
  const old = checklist.records.find((r) => r.id === id);
  const receipt = `${p.me.actor_ref}:${id}`; const fingerprint = JSON.stringify({ coop, household, season, b, at });
  if (old) return nativeJournalReceipts.get(receipt) === fingerprint ? { record: old, checklist } : problem(409, "idempotency_key_reused", "Nội dung ghi lại đã thay đổi");
  if (b.expected_season_event_id !== s.current_event_id) return problem(409, "cropcycle.task_season_changed", "Vụ đã thay đổi");
  const task = checklist.progress.stages.flatMap((s) => s.tasks).find((t) => t.seq === b.step_seq);
  if (!task) return validation([{ field: "step_seq", reason: "Việc không thuộc snapshot" }]);
  if (task.done_on) return problem(409, "cropcycle.task_already_resolved", "Việc đã xong");
  if (!task.can_complete) return problem(422, "cropcycle.task_season_state", "Vụ hoặc giai đoạn không cho ghi");
  const createdDay = new Intl.DateTimeFormat("en-CA", { timeZone: "Asia/Ho_Chi_Minh" }).format(new Date(at));
  if (!/^\d{4}-\d{2}-\d{2}$/.test(b.done_on) || !Number.isFinite(Date.parse(b.done_on)) || b.done_on > checklist.progress.as_of_date || b.done_on > createdDay || !["partial", "done"].includes(b.state)) return validation([{ field: "done_on", reason: "Ngày hoặc kết quả ghi việc không hợp lệ" }]);
  const backfilled = !task.undated && b.done_on < new Date(Date.parse(`${createdDay}T00:00:00Z`) - 3 * 86400000).toISOString().slice(0, 10);
  if (backfilled && !b.backfill_reason?.trim()) return validation([{ field: "backfill_reason", reason: "Nhập lý do ghi bù quá 3 ngày" }]);
  if (b.file_refs?.length) return problem(503, "sync.dependency_unavailable", "Chế độ giả lập chưa nối kho ảnh mã hóa");
  const step = effectiveSteps(s.sop_snapshot).find((t) => t.seq === b.step_seq)!;
  if ((step.materials?.length && !b.materials?.length) && !b.zero_confirmed) return problem(409, "cropcycle.task_zero_confirmation_required", "Xác nhận không dùng vật tư khuyến cáo");
  const actual: components["schemas"]["HouseholdTaskDetails"]["actual_materials"] = [];
  for (const m of b.materials ?? []) {
    if (!/^(0|[1-9][0-9]{0,8})(\.[0-9]{1,4})?$/.test(m.dose_per_ha)) return validation([{ field: "materials", reason: "Liều không âm, tối đa bốn chữ số lẻ" }]);
    if (scaledArea(m.dose_per_ha) === 0n && !b.zero_confirmed) return problem(409, "cropcycle.task_zero_confirmation_required", "Xác nhận liều bằng 0");
    const c = CATALOGS.material.find((c) => c.code === m.material_code);
    if (m.material_code && !c) return validation([{ field: "materials", reason: "Chọn vật tư trong danh mục" }]);
    if (!c && (!m.name?.trim() || !m.unit?.trim() || CATALOGS.material.some((c) => c.name.toLocaleLowerCase() === m.name?.trim().toLocaleLowerCase()))) return validation([{ field: "materials", reason: "Tên/đơn vị chưa hợp lệ hoặc đã có trong danh mục" }]);
    const total = scaledArea(m.dose_per_ha) * scaledArea(s.area_ha);
    const quantity = `${total / 100000000n}.${(total % 100000000n).toString().padStart(8, "0")}`.replace(/\.?0+$/, "");
    actual.push({ material_code: c?.code, name: c?.name ?? m.name!, unit: c?.attrs.unit ?? m.unit!, group: c?.attrs.group, fertilizer_type: c?.attrs.fertilizer_type, dose_per_ha: m.dose_per_ha, quantity });
  }
  const record: TaskRecord = { id, log_ref: id, step_seq: b.step_seq, state: b.state, done_on: b.done_on, sop_version_id: s.sop_snapshot.version_id, inputs: actual.map((m) => ({ name: m.name, unit: m.unit, qty: m.quantity })), gps_missing: b.gps?.lon == null || b.gps?.lat == null, gps: b.gps ?? { source: "no_device" }, actor_ref: p.me.actor_ref, event_time: at, recorded_at: nowIso(), note: b.note, file_refs: [], details: { actual_materials: actual, backfilled, backfill_reason: b.backfill_reason, zero_confirmed: b.zero_confirmed } };
  taskRecords.set(season, [...checklist.records, record]); nativeJournalReceipts.set(receipt, fingerprint);
  return { record, checklist: mockChecklist(s, true) };
}

// AH-176 (KH-17): vụ cùng HTX, cùng SOP gốc, có việc cùng tên và loại; mã lý do khi việc không ghi được.
const batchReceipts = new Map<string, string>();
function batchCandidates(src: Season, step: Season["sop_snapshot"]["steps"][number]) {
  const norm = (x: string) => x.trim().toLowerCase().replace(/\s+/g, " ");
  return seasons.filter((c) => c.cooperative_ref === src.cooperative_ref && c.sop_snapshot.template_ref === src.sop_snapshot.template_ref && (c.status === "open" || c.status === "completed")).map((season) => {
    const same = effectiveSteps(season.sop_snapshot).filter((t) => norm(t.name) === norm(step.name) && t.activity_code === step.activity_code);
    const seq = same.find((t) => t.seq === step.seq)?.seq ?? (same.length === 1 ? same[0]!.seq : 0);
    if (!seq) return { season, seq: 0, code: "cropcycle.batch_task_mismatch", reason: "Vụ không dùng cùng SOP gốc hoặc không có việc này" };
    const task = mockChecklist(season, true).progress.stages.flatMap((st) => st.tasks).find((t) => t.seq === seq);
    if (!task || task.done_on || task.completion_status === "skipped") return { season, seq, code: "cropcycle.task_already_resolved", reason: "Việc đã xong hoặc bỏ qua" };
    if (!task.can_complete) return { season, seq, code: "cropcycle.task_stage_locked", reason: "Vụ đã thu hoạch chỉ ghi việc chưa xong của giai đoạn cuối" };
    return { season, seq, code: "", reason: "" };
  });
}

function mockChecklist(s: Season, write: boolean): components["schemas"]["HouseholdChecklist"] {
  const plan = mockPlan(s.sop_snapshot, s.anchor_date, s.current_event_id);
  const records = taskRecords.get(s.season_ref) ?? [];
  let total = 0, done = 0, skippedCount = 0, due = 0, overdue = 0;
  const skipped = skippedSeqs(s.sop_snapshot);
  const last = Math.max(...plan.stages.map((s) => s.seq));
  const stages = plan.stages.map((stage) => {
    let resolved = 0;
    const tasks = stage.steps.map((task) => {
      total++;
      const record = records.find((r) => r.step_seq === task.seq && r.state === "done");
      let status: components["schemas"]["HouseholdTaskProgress"]["completion_status"] = task.status;
      if (!record && skipped.includes(task.seq)) { skippedCount++; resolved++; return { ...task, completion_status: "skipped" as const, done_on: null, can_complete: false }; }
      if (!record && !task.undated && task.status === "due") due++;
      if (!record && !task.undated && task.status === "overdue") overdue++;
      if (record) { done++; resolved++; status = task.undated ? "done_on_time" : !task.from_date ? "done" : record.done_on < task.from_date ? "done_early" : task.to_date && record.done_on > task.to_date ? "done_late" : "done_on_time"; }
      return { ...task, completion_status: status, done_on: record?.done_on ?? null, can_complete: write && !record && (s.status === "open" || (s.status === "completed" && stage.seq === last)) };
    });
    const status: "done" | "in_progress" | "not_started" = tasks.length && resolved === tasks.length ? "done" : resolved || records.some((r) => tasks.some((t) => t.seq === r.step_seq)) ? "in_progress" : "not_started";
    return { seq: stage.seq, name: stage.name, status, tasks };
  });
  const denominator = total - skippedCount;
  const scaled = denominator ? (BigInt(done) * 10000n + BigInt(denominator) / 2n) / BigInt(denominator) : null;
  return { season_ref: s.season_ref, current_event_id: s.current_event_id, records, progress: { as_of_date: plan.as_of_date, total, done, skipped: skippedCount, due, overdue, percent: scaled == null ? null : `${scaled / 100n}.${(scaled % 100n).toString().padStart(2, "0")}`, stages } };
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
  const effective = effectiveSteps(snapshot);
  const lastHarvest = effective.filter((s) => s.activity_code === "thu_hoach").sort((a, b) => b.seq - a.seq)[0];
  const harvest = lastHarvest ? dates(lastHarvest, null)[0] : null;
  return { anchor_date: anchor, as_of_date: today, expected_harvest_date: harvest,
    sop_version_id: snapshot.version_id, current_event_id: event,
    stages: snapshot.stages.map((stage) => ({ seq: stage.seq, name: stage.name, from_date: add(anchor, stage.from_day), to_date: add(anchor, stage.to_day),
      steps: effective.filter((s) => s.stage_seq === stage.seq).map((s) => {
        const [from, to] = dates(s, harvest);
        const undated = s.window?.kind === "none";
        return { seq: s.seq, stage_seq: stage.seq, task_seq: s.task_seq ?? s.seq, name: s.name, activity_code: s.activity_code, ...(s.custom ? { custom: true } : {}),
          window_kind: s.window?.kind ?? "none", from_date: from, to_date: to, expected_date: from, undated,
          status: undated ? "due" : !from ? "waiting_anchor" : today < from ? "upcoming" : to && today > to ? "overdue" : "due" };
      }),
    })),
  };
}

function mockTaskEdit(void_: boolean): Route["handler"] {
  return ({ params, persona, body, request }) => {
    if (!journalWriter(persona)) return problem(403, "cropcycle.read_only", "Vai của bạn chỉ được xem");
    const s = seasons.find((s) => s.cooperative_ref === params[0] && s.household_ref === params[1] && s.season_ref === params[2]);
    if (!s || (!hasRole(persona, "platform_admin") && persona.me.org?.ref !== s.cooperative_ref)) return problem(404, "cropcycle.household_not_found", "Không tìm thấy nông hộ");
    const receipt = `${persona.me.actor_ref}:${request.headers.get("Idempotency-Key")}`;
    const replay = editReceipts.get(receipt);
    if (replay) return json({ record: replay, checklist: mockChecklist(s, true) });
    const b = body as { expected_record_id: string; reason: string; state?: "done" | "partial"; done_on?: string; note?: string };
    if (!b.reason || b.reason.trim().length < 10) return validation([{ field: "reason", reason: "Nhập lý do sửa hoặc xóa (ít nhất 10 ký tự)" }]);
    const records = taskRecords.get(s.season_ref) ?? [];
    const cur = records.find((r) => r.log_ref === params[3]);
    if (!cur) return (taskHistory.get(params[3]!) ?? []).length ? problem(409, "cropcycle.task_record_voided", "Bản ghi đã bị xóa") : problem(404, "cropcycle.task_record_not_found", "Không tìm thấy bản ghi");
    if (cur.id !== b.expected_record_id) return problem(409, "cropcycle.task_record_changed", "Bản ghi đã được sửa ở nơi khác");
    const history = taskHistory.get(cur.log_ref) ?? [{ ...cur, version: cur.version ?? 1 }];
    const next: TaskRecord = { ...cur, id: uuidv7(), version: (cur.version ?? 1) + 1, recorded_at: nowIso(), actor_ref: persona.me.actor_ref, correction_reason: b.reason.trim(),
      ...(void_ ? { state: "voided" as const } : { state: b.state ?? cur.state, done_on: b.done_on ?? cur.done_on, note: b.note ?? cur.note }) };
    taskHistory.set(cur.log_ref, [...history, next]);
    taskRecords.set(s.season_ref, void_ ? records.filter((r) => r.log_ref !== cur.log_ref) : records.map((r) => (r.log_ref === cur.log_ref ? next : r)));
    editReceipts.set(receipt, next);
    return json({ record: next, checklist: mockChecklist(s, true) });
  };
}

export const householdSeasonRoutes: Route[] = [{
  // AH-140: vụ nông hộ của HTX kèm tiến độ (chỉ đọc), lọc trạng thái, người mở vụ.
  method: "GET", pattern: /^\/v1\/cooperatives\/([\w-]+)\/household-seasons$/,
  handler: ({ params, persona, url }) => {
    if (!hasRole(persona, "platform_admin", "htx_member", "coop_manager") || (!hasRole(persona, "platform_admin") && persona.me.org?.ref !== params[0])) return problem(404, "cropcycle.household_not_found", "Không tìm thấy HTX");
    const status = url.searchParams.get("status") ?? "", openedBy = url.searchParams.get("opened_by") ?? "";
    if (status && !["open", "completed", "cancelled"].includes(status)) return validation([{ field: "status", reason: "trạng thái không hợp lệ" }]);
    const rows = seasons.filter((x) => x.cooperative_ref === params[0] && (!status || x.status === status) && (!openedBy || x.opened_by === openedBy))
      .sort((a, b) => a.season_ref.localeCompare(b.season_ref))
      .map((x) => {
        const p = mockChecklist(x, false).progress;
        return { season_ref: x.season_ref, household_ref: x.household_ref, crop_code: x.crop_code, season_code: x.season_code, season_year: x.season_year,
          variety_code: x.variety_code, area_ha: x.area_ha, status: x.status, opened_by: x.opened_by, opened_at: x.opened_at,
          progress: { percent: p.percent, done: p.done, total: p.total, skipped: p.skipped, due: p.due, overdue: p.overdue } };
      });
    return json(paginate(rows, url));
  },
}, {
  method: "POST", pattern: /^\/v1\/cooperatives\/([\w-]+)\/households\/([\w-]+)\/seasons\/([\w-]+)\/task-records\/([\w-]+)\/corrections$/,
  handler: mockTaskEdit(false),
}, {
  method: "POST", pattern: /^\/v1\/cooperatives\/([\w-]+)\/households\/([\w-]+)\/seasons\/([\w-]+)\/task-records\/([\w-]+)\/void$/,
  handler: mockTaskEdit(true),
}, {
  method: "GET", pattern: /^\/v1\/cooperatives\/([\w-]+)\/households\/([\w-]+)\/seasons\/([\w-]+)\/task-records\/([\w-]+)\/history$/,
  handler: ({ params, persona }) => {
    const s = seasons.find((s) => s.cooperative_ref === params[0] && s.household_ref === params[1] && s.season_ref === params[2]);
    if (!s || !hasRole(persona, "platform_admin", "htx_member", "coop_manager") || (!hasRole(persona, "platform_admin") && persona.me.org?.ref !== s.cooperative_ref)) return problem(404, "cropcycle.household_not_found", "Không tìm thấy nông hộ");
    const current = (taskRecords.get(s.season_ref) ?? []).find((r) => r.log_ref === params[3]);
    const items = taskHistory.get(params[3]!) ?? (current ? [{ ...current, version: current.version ?? 1 }] : []);
    return items.length ? json({ items }) : problem(404, "cropcycle.task_record_not_found", "Không tìm thấy bản ghi");
  },
}, {
  // AH-175: sửa mốc gốc, diện tích, giống/SOP của vụ đang canh tác kèm lý do.
  method: "POST", pattern: /^\/v1\/cooperatives\/([\w-]+)\/households\/([\w-]+)\/seasons\/([\w-]+)\/corrections$/,
  handler: ({ params, persona, body }) => {
    if (!journalWriter(persona)) return problem(403, "forbidden", "Vai của bạn chỉ được xem");
    const s = seasons.find((s) => s.cooperative_ref === params[0] && s.household_ref === params[1] && s.season_ref === params[2]);
    if (!s || (!hasRole(persona, "platform_admin") && persona.me.org?.ref !== s.cooperative_ref)) return problem(404, "cropcycle.season_not_found", "Không tìm thấy vụ");
    const b = body as { expected_season_event_id: string; reason: string; anchor_date?: string; area_ha?: string; variety_code?: string; sop_version_id?: string };
    const reason = (b.reason ?? "").trim();
    if (reason.length < 10 || reason.length > 500) return validation([{ field: "reason", reason: "Nhập lý do sửa vụ (10–500 ký tự)" }]);
    if (s.status !== "open") return problem(409, "cropcycle.season_state", "Chỉ sửa được vụ đang canh tác");
    if (b.expected_season_event_id !== s.current_event_id) return problem(409, "cropcycle.concurrent_edit", "Vụ đã được sửa ở nơi khác");
    if (b.area_ha !== undefined && (!/^(0|[1-9][0-9]{0,9})(\.[0-9]{1,2})?$/.test(b.area_ha) || scaledArea(b.area_ha) <= 0n)) return validation([{ field: "area_ha", reason: "Diện tích phải lớn hơn 0" }]);
    const changeVariety = b.variety_code !== undefined && b.variety_code !== s.variety_code;
    if (changeVariety || b.sop_version_id) {
      if ((taskRecords.get(s.season_ref) ?? []).length) return problem(409, "cropcycle.season_has_logs", "Vụ đã có nhật ký: không đổi giống, SOP");
      if (changeVariety && !b.sop_version_id) return validation([{ field: "sop_version_id", reason: "Đổi giống phải chọn SOP theo giống mới" }]);
    }
    if (b.area_ha !== undefined && b.area_ha !== s.area_ha) { releaseHouseholdMock(s.cooperative_ref, s.household_ref, s.area_ha); allocateHouseholdMock(s.cooperative_ref, s.household_ref, b.area_ha); s.area_ha = b.area_ha; }
    if (b.anchor_date) s.anchor_date = b.anchor_date;
    if (changeVariety) s.variety_code = b.variety_code!;
    // AH-87: đổi diện tích thì tính lại sản lượng dự kiến (tham số đã chụp, giữ giá trị nhập).
    if (s.yield_estimate && Decimal.parse(s.yield_estimate.area_ha)?.eq(Decimal.parse(s.area_ha)!) === false) {
      s.yield_estimate = recomputeEstimate({ ...s.yield_estimate, seq: s.yield_estimate.seq + 1, area_ha: tonnes(Decimal.parse(s.area_ha)!), updated_at: nowIso(), updated_by: persona.me.actor_ref });
    }
    s.current_event_id = uuidv7(); s.version += 1;
    planHistory.set(s.season_ref, [{ event_id: s.current_event_id, actor_ref: persona.me.actor_ref, event_time: nowIso(), summary: reason }, ...(planHistory.get(s.season_ref) ?? [])]);
    return json(s);
  },
}, {
  // AH-87: Thành viên HTX nhập, sửa, xóa (null) sản lượng lúa, rơm dự kiến của vụ đang canh tác.
  method: "POST", pattern: /^\/v1\/cooperatives\/([\w-]+)\/households\/([\w-]+)\/seasons\/([\w-]+)\/yield-estimates$/,
  handler: ({ params, persona, body, request }) => {
    if (!journalWriter(persona)) return problem(403, "forbidden", "Vai của bạn chỉ được xem");
    const s = seasons.find((s) => s.cooperative_ref === params[0] && s.household_ref === params[1] && s.season_ref === params[2]);
    if (!s || (!hasRole(persona, "platform_admin") && persona.me.org?.ref !== s.cooperative_ref)) return problem(404, "cropcycle.season_not_found", "Không tìm thấy vụ");
    const b = (body ?? {}) as { expected_seq?: number; rice_t?: string | null; straw_t?: string | null };
    const receipt = `${persona.me.actor_ref}:estimate:${request.headers.get("Idempotency-Key")}`;
    const fingerprint = JSON.stringify([b.expected_seq, b.rice_t || null, b.straw_t || null]);
    const replay = estimateReceipts.get(receipt);
    if (replay) return replay.season === s.season_ref && replay.body === fingerprint ? json({ yield_estimate: replay.estimate }) : problem(422, "idempotency_conflict", "Khóa đã dùng cho nội dung khác");
    if (typeof b.expected_seq !== "number" || b.expected_seq < 0) return validation([{ field: "expected_seq", reason: "Bắt buộc" }]);
    const errors = (["rice_t", "straw_t"] as const).flatMap((f) => { const v = b[f]; const e = v ? tonnesError(v) : null; return e ? [{ field: f, reason: e }] : []; });
    if (errors.length) return validation(errors);
    if (s.status !== "open") return problem(409, "cropcycle.season_state", "Chỉ sửa sản lượng dự kiến khi vụ đang canh tác");
    const cur = s.yield_estimate;
    if (!cur || cur.seq !== b.expected_seq) return problem(409, "cropcycle.yield_estimate_changed", "Sản lượng dự kiến vừa thay đổi; tải lại trước khi sửa", { current_seq: cur?.seq ?? 0 });
    if (!cur.is_rice && b.straw_t) return validation([{ field: "straw_t", reason: "Vụ không phải lúa: không có sản lượng rơm dự kiến" }]);
    const norm = (v?: string | null) => v ? tonnes(Decimal.parse(v)!) : null;
    s.yield_estimate = recomputeEstimate({ ...cur, seq: cur.seq + 1, entered_rice_t: norm(b.rice_t), entered_straw_t: norm(b.straw_t), updated_at: nowIso(), updated_by: persona.me.actor_ref });
    estimateReceipts.set(receipt, { season: s.season_ref, body: fingerprint, estimate: structuredClone(s.yield_estimate) });
    return json({ yield_estimate: s.yield_estimate });
  },
}, {
  // AH-100: khai báo sản lượng của vụ theo hộ; vụ "Đã thu hoạch", việc Thu hoạch (kể cả Bỏ qua) tự Xong theo ngày đó.
  method: "POST", pattern: /^\/v1\/cooperatives\/([\w-]+)\/households\/([\w-]+)\/seasons\/([\w-]+)\/harvest$/,
  handler: ({ params, persona, body, request }) => {
    if (!journalWriter(persona)) return problem(403, "forbidden", "Vai của bạn chỉ được xem");
    const s = seasons.find((s) => s.cooperative_ref === params[0] && s.household_ref === params[1] && s.season_ref === params[2]);
    if (!s || (!hasRole(persona, "platform_admin") && persona.me.org?.ref !== s.cooperative_ref)) return problem(404, "cropcycle.season_not_found", "Không tìm thấy vụ");
    const b = (body ?? {}) as { expected_season_event_id?: string; mass_t?: string; harvest_date?: string; zero_confirmed?: boolean; gps?: Season["gps"]; channel?: string };
    const receipt = `${persona.me.actor_ref}:harvest:${request.headers.get("Idempotency-Key")}`;
    const fingerprint = JSON.stringify([s.season_ref, b.mass_t, b.harvest_date]);
    const replay = harvestReceipts.get(receipt);
    if (replay) return replay.body === fingerprint ? json(replay.result) : problem(422, "idempotency_conflict", "Khóa đã dùng cho nội dung khác");
    const massError = typeof b.mass_t === "string" ? tonnesError(b.mass_t) : "Nhập số tấn";
    if (massError) return validation([{ field: "mass_t", reason: massError }]);
    const today = new Intl.DateTimeFormat("en-CA", { timeZone: "Asia/Ho_Chi_Minh" }).format(new Date());
    if (!b.harvest_date || !/^\d{4}-\d{2}-\d{2}$/.test(b.harvest_date) || b.harvest_date < s.anchor_date || b.harvest_date > today) return validation([{ field: "harvest_date", reason: `Ngày thu hoạch từ mốc gốc (${s.anchor_date}) đến hôm nay` }]);
    if (s.status !== "open") return problem(409, "cropcycle.season_state", "Chỉ khai báo cho vụ đang canh tác; vụ đã khai báo thì dùng Sửa sản lượng");
    if (b.expected_season_event_id !== s.current_event_id) return problem(409, "cropcycle.task_season_changed", "Vụ đã thay đổi");
    const tonnesValue = Decimal.parse(b.mass_t!)!;
    if (tonnesValue.isZero() && !b.zero_confirmed) return problem(409, "cropcycle.harvest_zero_confirmation_required", "Sản lượng bằng 0: xác nhận lại trước khi lưu");
    const at = nowIso();
    const harvest: components["schemas"]["HouseholdHarvest"] = { harvest_id: uuidv7(), mass_t: tonnes(tonnesValue), mass_kg: tonnesValue.mul(Decimal.fromInt(1000)).toString(),
      harvest_date: b.harvest_date, yield_t_per_ha: tonnes(tonnesValue.divRound(Decimal.parse(s.area_ha)!, 8)), zero_confirmed: tonnesValue.isZero(), declared_by: persona.me.actor_ref, declared_at: at };
    const records = taskRecords.get(s.season_ref) ?? [];
    const auto = effectiveSteps(s.sop_snapshot).filter((t) => t.activity_code === "thu_hoach" && !records.some((r) => r.step_seq === t.seq && r.state === "done")).map((t) => t.seq);
    taskRecords.set(s.season_ref, [...records, ...auto.map((seq) => { const id = uuidv7(); return { id, log_ref: id, step_seq: seq, state: "done" as const, done_on: b.harvest_date!, sop_version_id: s.sop_snapshot.version_id,
      inputs: [], gps_missing: b.gps?.lon == null || b.gps?.lat == null, gps: b.gps ?? { source: "no_device" }, actor_ref: persona.me.actor_ref, event_time: at, recorded_at: at, file_refs: [] }; })]);
    s.status = "completed"; s.current_event_id = uuidv7(); s.version += 1; s.harvest = harvest;
    releaseHouseholdMock(s.cooperative_ref, s.household_ref, s.area_ha);
    const result = { season: structuredClone(s), harvest, auto_completed_steps: auto };
    harvestReceipts.set(receipt, { body: fingerprint, result });
    return json(result, 201);
  },
}, {
  // AH-70: dừng vụ đang canh tác kèm lý do; giải phóng diện tích.
  method: "POST", pattern: /^\/v1\/cooperatives\/([\w-]+)\/households\/([\w-]+)\/seasons\/([\w-]+)\/stop$/,
  handler: ({ params, persona, body, request }) => {
    if (!journalWriter(persona)) return problem(403, "forbidden", "Vai của bạn chỉ được xem");
    const s = seasons.find((s) => s.cooperative_ref === params[0] && s.household_ref === params[1] && s.season_ref === params[2]);
    if (!s || (!hasRole(persona, "platform_admin") && persona.me.org?.ref !== s.cooperative_ref)) return problem(404, "cropcycle.season_not_found", "Không tìm thấy vụ");
    const receipt = `${persona.me.actor_ref}:stop:${request.headers.get("Idempotency-Key")}`;
    const replay = stopReceipts.get(receipt);
    if (replay) return json(replay);
    const b = body as { expected_season_event_id: string; reason: string };
    const reason = (b.reason ?? "").trim().replace(/\s+/g, " ");
    if (reason.length < 10 || reason.length > 500) return validation([{ field: "reason", reason: "Nhập lý do dừng vụ (10–500 ký tự)" }]);
    if (s.status !== "open") return problem(409, "cropcycle.season_state", "Chỉ dừng được vụ đang canh tác");
    if (b.expected_season_event_id !== s.current_event_id) return problem(409, "cropcycle.task_season_changed", "Vụ đã thay đổi");
    s.status = "cancelled"; s.current_event_id = uuidv7(); s.version += 1; s.stop_reason = reason; s.stopped_at = nowIso();
    releaseHouseholdMock(s.cooperative_ref, s.household_ref, s.area_ha);
    stopReceipts.set(receipt, structuredClone(s));
    return json(s);
  },
}, {
  method: "POST", pattern: /^\/v1\/cooperatives\/([\w-]+)\/households\/([\w-]+)\/seasons\/([\w-]+)\/plan-changes$/,
  handler: ({ params, persona, body, request }) => {
    if (!journalWriter(persona)) return problem(403, "cropcycle.read_only", "Vai của bạn chỉ được xem");
    const s = seasons.find((s) => s.cooperative_ref === params[0] && s.household_ref === params[1] && s.season_ref === params[2]);
    if (!s || (!hasRole(persona, "platform_admin") && persona.me.org?.ref !== s.cooperative_ref)) return problem(404, "cropcycle.season_not_found", "Không tìm thấy vụ");
    const receipt = `${persona.me.actor_ref}:${request.headers.get("Idempotency-Key")}`;
    const old = planReceipts.get(receipt);
    if (old) return json({ season: old.season, checklist: mockChecklist(old.season, true), summary: old.summary });
    const b = body as { expected_season_event_id: string; change: components["schemas"]["HouseholdPlanChange"] };
    if (s.status !== "open") return problem(409, "cropcycle.plan_season_closed", "Chỉ tùy chỉnh quy trình của vụ đang canh tác");
    if (b.expected_season_event_id !== s.current_event_id) return problem(409, "cropcycle.task_season_changed", "Vụ đã thay đổi");
    const records = taskRecords.get(s.season_ref) ?? [];
    const r = applyPlanChange(s.sop_snapshot, b.change, new Set(records.filter((x) => x.state === "done").map((x) => x.step_seq)), new Set(records.map((x) => x.step_seq)));
    if ("status" in r) return planProblem(r);
    s.sop_snapshot = r.snapshot; s.current_event_id = uuidv7(); s.version += 1;
    planHistory.set(s.season_ref, [{ event_id: s.current_event_id, actor_ref: persona.me.actor_ref, event_time: nowIso(), summary: r.summary }, ...(planHistory.get(s.season_ref) ?? [])]);
    planReceipts.set(receipt, { season: structuredClone(s), summary: r.summary });
    return json({ season: s, checklist: mockChecklist(s, true), summary: r.summary });
  },
}, {
  method: "GET", pattern: /^\/v1\/cooperatives\/([\w-]+)\/households\/([\w-]+)\/seasons\/([\w-]+)\/plan-history$/,
  handler: ({ params, persona }) => {
    const s = seasons.find((s) => s.cooperative_ref === params[0] && s.household_ref === params[1] && s.season_ref === params[2]);
    if (!s || !hasRole(persona, "platform_admin", "htx_member", "coop_manager") || (!hasRole(persona, "platform_admin") && persona.me.org?.ref !== s.cooperative_ref)) return problem(404, "cropcycle.season_not_found", "Không tìm thấy vụ");
    return json({ items: planHistory.get(s.season_ref) ?? [] });
  },
}, {
  method: "POST", pattern: /^\/v1\/devices$/,
  handler: ({ persona, body }) => {
    if (!journalWriter(persona)) return problem(403, "forbidden", "Không được đăng ký thiết bị nhật ký Web");
    const b = body as components["schemas"]["SyncDeviceInput"];
    if (b.platform !== "web") return validation([{ field: "platform", reason: "Mock này chỉ dành cho nhật ký Web" }]);
    const previous = journalDevices.get(b.device_id);
    if (previous && previous !== persona.me.actor_ref) return problem(409, "sync.device_taken", "Thiết bị thuộc tài khoản khác");
    journalDevices.set(b.device_id, persona.me.actor_ref);
    return json({ device_id: b.device_id, registered_at: nowIso(), platform: "web", app_version: b.app_version, revoked: false }, previous ? 200 : 201);
  },
}, {
  method: "GET", pattern: /^\/v1\/households\/([\w-]+)\/photo-key$/,
  handler: ({ persona }) => !journalWriter(persona) ? problem(403, "forbidden", "Vai chỉ đọc") : problem(503, "sync.dependency_unavailable", "Chế độ giả lập chưa nối khóa hộ và kho ảnh mã hóa"),
}, {
  method: "POST", pattern: /^\/v1\/sync\/push$/,
  handler: ({ persona, body, request }) => {
    if (!journalWriter(persona)) return problem(403, "forbidden", "Vai chỉ đọc");
    const batch = body as components["schemas"]["SyncPushBatch"];
    if (request.headers.get("X-Device-Id") !== batch.device_id || journalDevices.get(batch.device_id) !== persona.me.actor_ref) return problem(403, "sync.device_unregistered", "Thiết bị chưa đăng ký cho tài khoản");
    return json({ results: batch.records.map((r) => {
      const p = r.payload as components["schemas"]["HouseholdTaskDetailInput"] & { recorded_by: string; tenant_id: string; cooperative_ref: string; household_ref: string; season_ref: string };
      if (r.entity !== "household_task_record" || r.op !== "create" || p.recorded_by !== persona.me.actor_ref || p.tenant_id !== persona.me.tenant_id) return { id: r.id, status: "rejected", code: "forbidden" };
      if (!Number.isFinite(Date.parse(r.event_time)) || Date.parse(r.event_time) > Date.now() + 300000) return { id: r.id, status: "rejected", code: "validation_failed" };
      const existed = taskRecords.get(p.season_ref)?.some((old) => old.id === r.id);
      const result = mockTaskDetail(persona, p.cooperative_ref, p.household_ref, p.season_ref, p, r.id, r.event_time);
      if (result instanceof Response) return { id: r.id, status: "rejected", code: problemOf(result)?.code ?? "validation_failed" };
      return { id: r.id, status: existed ? "duplicate" : "accepted" };
    }) });
  },
}, {
  // AH-176: các vụ cùng SOP gốc có thể ghi chung một việc.
  method: "GET", pattern: /^\/v1\/cooperatives\/([\w-]+)\/households\/([\w-]+)\/seasons\/([\w-]+)\/task-batch-candidates$/,
  handler: ({ params, persona, url }) => {
    if (!journalWriter(persona)) return problem(403, "cropcycle.read_only", "Vai của bạn chỉ được xem");
    const src = seasons.find((s) => s.cooperative_ref === params[0] && s.household_ref === params[1] && s.season_ref === params[2]);
    if (!src || (!hasRole(persona, "platform_admin") && persona.me.org?.ref !== src.cooperative_ref)) return problem(404, "cropcycle.season_not_found", "Không tìm thấy vụ");
    const seq = Number(url.searchParams.get("step_seq"));
    const step = effectiveSteps(src.sop_snapshot).find((t) => t.seq === seq);
    if (!step) return validation([{ field: "step_seq", reason: "Việc không thuộc quy trình của vụ" }]);
    return json({ items: batchCandidates(src, step).map(({ season: c, seq: n, code, reason }) => ({ household_ref: c.household_ref, season_ref: c.season_ref, season_code: c.season_code, season_year: c.season_year,
      variety_code: c.variety_code, status: c.status, current_event_id: c.current_event_id, eligible: !code, is_source: c.season_ref === src.season_ref, ...(n ? { step_seq: n } : {}), ...(code ? { code, reason } : {}) })) });
  },
}, {
  // AH-176: ghi một việc cho nhiều vụ; mỗi vụ một bản ghi, vụ không ghi được bị bỏ qua kèm lý do.
  method: "POST", pattern: /^\/v1\/cooperatives\/([\w-]+)\/households\/([\w-]+)\/seasons\/([\w-]+)\/task-batches$/,
  handler: ({ params, persona, body, request }) => {
    if (!journalWriter(persona)) return problem(403, "cropcycle.read_only", "Vai của bạn chỉ được xem");
    const src = seasons.find((s) => s.cooperative_ref === params[0] && s.household_ref === params[1] && s.season_ref === params[2]);
    if (!src || (!hasRole(persona, "platform_admin") && persona.me.org?.ref !== src.cooperative_ref)) return problem(404, "cropcycle.season_not_found", "Không tìm thấy vụ");
    const b = body as components["schemas"]["HouseholdTaskDetailInput"] & { seasons: { household_ref: string; season_ref: string; expected_season_event_id: string }[] };
    if (!b.seasons?.length || b.seasons.length > 50) return validation([{ field: "seasons", reason: "Chọn từ 1 đến 50 vụ" }]);
    const step = effectiveSteps(src.sop_snapshot).find((t) => t.seq === b.step_seq);
    if (!step) return validation([{ field: "step_seq", reason: "Việc không thuộc quy trình của vụ" }]);
    const key = request.headers.get("Idempotency-Key");
    const options = batchCandidates(src, step);
    const results = b.seasons.map((t) => {
      const match = options.find((o) => o.season.season_ref === t.season_ref && o.season.household_ref === t.household_ref);
      if (!match?.seq) return { household_ref: t.household_ref, season_ref: t.season_ref, status: "skipped" as const, code: "cropcycle.batch_task_mismatch", reason: "Vụ không dùng cùng SOP gốc hoặc không có việc này" };
      const id = batchReceipts.get(`${persona.me.actor_ref}:${key}:${t.season_ref}`);
      const old = id ? taskRecords.get(t.season_ref)?.find((r) => r.id === id) : undefined;
      if (old) return { household_ref: t.household_ref, season_ref: t.season_ref, status: "recorded" as const, step_seq: match.seq, record: old, replayed: true };
      const r = mockTaskDetail(persona, match.season.cooperative_ref, t.household_ref, t.season_ref, { ...b, step_seq: match.seq, expected_season_event_id: t.expected_season_event_id });
      if (r instanceof Response) {
        const p = problemOf(r);
        return { household_ref: t.household_ref, season_ref: t.season_ref, status: "skipped" as const, step_seq: match.seq, code: p?.code ?? "validation_failed", reason: p?.title ?? "Không ghi được" };
      }
      batchReceipts.set(`${persona.me.actor_ref}:${key}:${t.season_ref}`, r.record.id);
      return { household_ref: t.household_ref, season_ref: t.season_ref, status: "recorded" as const, step_seq: match.seq, record: r.record, replayed: false };
    });
    const recorded = results.filter((r) => r.status === "recorded").length;
    return json({ results, recorded, skipped: results.length - recorded });
  },
}, {
  // AH-163: việc phát sinh ngoài quy trình — số việc riêng > 1000, nhãn Ngoài SOP, không tính tiến độ.
  method: "POST", pattern: /^\/v1\/cooperatives\/([\w-]+)\/households\/([\w-]+)\/seasons\/([\w-]+)\/adhoc-records$/,
  handler: ({ params, persona, body, request }) => {
    if (!journalWriter(persona)) return problem(403, "cropcycle.read_only", "Vai của bạn chỉ được xem");
    const s = seasons.find((s) => s.cooperative_ref === params[0] && s.household_ref === params[1] && s.season_ref === params[2]);
    if (!s || (!hasRole(persona, "platform_admin") && persona.me.org?.ref !== s.cooperative_ref)) return problem(404, "cropcycle.season_not_found", "Không tìm thấy vụ");
    const receipt = `${persona.me.actor_ref}:adhoc:${request.headers.get("Idempotency-Key")}`;
    const replay = editReceipts.get(receipt);
    if (replay) return json({ record: replay, checklist: mockChecklist(s, true) }, 201);
    const b = body as { expected_season_event_id: string; stage_seq: number; name: string; activity_code: string; state: "done" | "partial"; done_on: string; note?: string; backfill_reason?: string; zero_confirmed?: boolean; materials: { name?: string; unit?: string; dose_per_ha: string }[] };
    const today = new Intl.DateTimeFormat("en-CA", { timeZone: "Asia/Ho_Chi_Minh" }).format(new Date(nowIso()));
    if (!b.name?.trim()) return validation([{ field: "name", reason: "Nhập nội dung việc (tối đa 200 ký tự)" }]);
    if (!/^\d{4}-\d{2}-\d{2}$/.test(b.done_on) || b.done_on > today) return validation([{ field: "done_on", reason: "Ngày làm không được sau hôm nay" }]);
    if (b.expected_season_event_id !== s.current_event_id) return problem(409, "cropcycle.task_season_changed", "Vụ đã thay đổi");
    const last = s.sop_snapshot.stages.length;
    if (b.stage_seq < 1 || b.stage_seq > last) return validation([{ field: "stage_seq", reason: "Chọn giai đoạn của vụ" }]);
    if (s.status === "completed" && b.stage_seq !== last) return problem(409, "cropcycle.task_season_locked", "Vụ đã thu hoạch chỉ ghi giai đoạn cuối");
    if (s.status !== "open" && s.status !== "completed") return problem(422, "cropcycle.task_season_state", "Vụ không cho ghi");
    if (b.done_on < new Date(Date.parse(`${today}T00:00:00Z`) - 3 * 86400000).toISOString().slice(0, 10) && !b.backfill_reason?.trim()) return validation([{ field: "backfill_reason", reason: "Bắt buộc nhập lý do khi ghi bù quá 3 ngày" }]);
    for (const m of b.materials ?? []) {
      if (!/^(0|[1-9][0-9]{0,8})(\.[0-9]{1,4})?$/.test(m.dose_per_ha)) return validation([{ field: "materials", reason: "Liều không âm, tối đa bốn chữ số lẻ" }]);
      if (scaledArea(m.dose_per_ha) === 0n && !b.zero_confirmed) return problem(409, "cropcycle.task_zero_confirmation_required", "Xác nhận liều bằng 0");
    }
    const records = taskRecords.get(s.season_ref) ?? [];
    const seq = Math.max(1000, ...records.filter((r) => r.adhoc).map((r) => r.step_seq)) + 1;
    const id = uuidv7();
    const record: TaskRecord = { id, log_ref: id, step_seq: seq, state: b.state, done_on: b.done_on, sop_version_id: s.sop_snapshot.version_id, adhoc: true, adhoc_name: b.name.trim(), stage_seq: b.stage_seq, activity_code: b.activity_code,
      inputs: (b.materials ?? []).map((m) => ({ name: m.name ?? "", unit: m.unit ?? "", qty: m.dose_per_ha })), gps_missing: true, gps: { source: "no_device" }, actor_ref: persona.me.actor_ref, event_time: nowIso(), recorded_at: nowIso(), note: b.note, file_refs: [] };
    taskRecords.set(s.season_ref, [...records, record]); editReceipts.set(receipt, record);
    return json({ record, checklist: mockChecklist(s, true) }, 201);
  },
}, {
  method: "POST", pattern: /^\/v1\/cooperatives\/([\w-]+)\/households\/([\w-]+)\/seasons\/([\w-]+)\/task-records$/,
  handler: ({ persona, params, body }) => {
    const result = mockTaskDetail(persona, params[0]!, params[1]!, params[2]!, body as components["schemas"]["HouseholdTaskDetailInput"]);
    return result instanceof Response ? result : json(result, 201);
  },
}, {
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
    const step = effectiveSteps(s.sop_snapshot).find((t) => t.seq === b.step_seq)!;
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
    if (b.self_made_plan) {
      if (householdMockSOPs(coop.api.ref, b.crop_code, b.variety_code).length) return problem(409, "cropcycle.sop_available", "Loại cây đã có SOP đang áp dụng");
      const own = applyPlanChanges({ template_ref: uuidv7(), version_id: uuidv7(), version_no: 1, crop_code: b.crop_code, anchor_name: "Ngày xuống giống", self_made: true, stages: b.self_made_plan.stages, steps: b.self_made_plan.steps }, b.plan_changes);
      return "status" in own ? planProblem(own) : json(mockPlan(own, b.anchor_date, null));
    }
    const chosen = householdMockSOPs(coop.api.ref, b.crop_code, b.variety_code).find((s) => s.version.id === b.sop_version_id);
    if (!chosen || !b.variety_code || typeof b.area_ha !== "string" || !/^(0|[1-9][0-9]{0,9})(\.[0-9]{1,2})?$/.test(b.area_ha) || scaledArea(b.area_ha) <= 0n || !/^\d{4}-\d{2}-\d{2}$/.test(b.anchor_date) || !Number.isFinite(Date.parse(b.anchor_date))) return validation([{ field: "sop_version_id", reason: "Chọn giống, SOP, diện tích và mốc gốc hợp lệ" }]);
    const snapshot = applyPlanChanges({ template_ref: chosen.template.ref, version_id: chosen.version.id, version_no: chosen.version.version_no, crop_code: b.crop_code, anchor_name: chosen.template.anchor_name, stages: chosen.version.stages, steps: chosen.version.steps }, b.plan_changes);
    return "status" in snapshot ? planProblem(snapshot) : json(mockPlan(snapshot, b.anchor_date, null));
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
    // AH-139: sao chép giống và quy trình (kèm tùy chỉnh) từ vụ trước của chính hộ.
    const source = b.copy_from_season_ref ? seasons.find((x) => x.season_ref === b.copy_from_season_ref && x.household_ref === household.household_ref && x.cooperative_ref === coop.api.ref) : undefined;
    if (b.copy_from_season_ref && !source) return problem(404, "cropcycle.season_not_found", "Không tìm thấy vụ nguồn");
    if (source && (source.crop_code !== b.crop_code || source.variety_code !== b.variety_code)) return validation([{ field: "variety_code", reason: "Sao chép giữ giống của vụ trước" }]);
    const choices = source ? [] : householdMockSOPs(coop.api.ref, b.crop_code, b.variety_code);
    // AH-162: quy trình tự tạo khi chưa có SOP đang áp dụng.
    if (b.self_made_plan) {
      if (choices.length) return problem(409, "cropcycle.sop_available", "Loại cây đã có SOP đang áp dụng");
      const own = applyPlanChanges({ template_ref: uuidv7(), version_id: uuidv7(), version_no: 1, crop_code: b.crop_code, name: "Quy trình tự tạo", anchor_name: b.self_made_anchor || "Ngày xuống giống", self_made: true, stages: b.self_made_plan.stages, steps: b.self_made_plan.steps }, b.plan_changes);
      if ("status" in own) return planProblem(own);
      const made: Season = { season_ref: uuidv7(), entry_kind: "household", household_ref: household.household_ref, cooperative_ref: coop.api.ref, commune_code: household.commune_code,
        crop_code: b.crop_code, season_code: b.season_code, season_year: b.season_year, variety_code: b.variety_code, area_ha: b.area_ha, anchor_date: b.anchor_date,
        expected_harvest_date: null, harvest_open_date: null, status: "open", current_event_id: uuidv7(), version: 1, opened_by: persona.me.actor_ref, opened_at: nowIso(),
        gps: b.gps ?? { source: "denied" }, gps_missing: b.gps?.lon == null || b.gps?.lat == null, sop_snapshot: own, copied_from: null,
        yield_estimate: initialEstimate(b.crop_code, b.area_ha, persona.me.actor_ref, nowIso()) };
      seasons.push(made); allocateHouseholdMock(coop.api.ref, household.household_ref, b.area_ha);
      return json({ season: made }, 201);
    }
    if (!source && !choices.length) return problem(422, "cropcycle.no_active_sop_for_crop", "Chưa có SOP đang áp dụng; liên hệ Admin");
    const chosen = source ? undefined : b.sop_version_id ? choices.find((s) => s.version.id === b.sop_version_id) : choices.length === 1 ? choices[0] : undefined;
    if (!source && !chosen) return validation([{ field: "sop_version_id", reason: "Chọn SOP đang áp dụng phù hợp" }]);
    const snapshot = applyPlanChanges(source ? structuredClone(source.sop_snapshot) : structuredClone({ template_ref: chosen!.template.ref, version_id: chosen!.version.id, version_no: chosen!.version.version_no, crop_code: b.crop_code, name: chosen!.template.name, anchor_name: chosen!.template.anchor_name, stages: chosen!.version.stages, steps: chosen!.version.steps }), b.plan_changes);
    if ("status" in snapshot) return planProblem(snapshot);
    const s: Season = {
      season_ref: uuidv7(), entry_kind: "household", household_ref: household.household_ref, cooperative_ref: coop.api.ref, commune_code: household.commune_code,
      crop_code: b.crop_code, season_code: b.season_code, season_year: b.season_year, variety_code: b.variety_code, area_ha: b.area_ha, anchor_date: b.anchor_date,
      expected_harvest_date: null, harvest_open_date: null, status: "open", current_event_id: uuidv7(), version: 1, opened_by: persona.me.actor_ref, opened_at: nowIso(),
      gps: b.gps ?? { source: "denied" }, gps_missing: b.gps?.lon == null || b.gps?.lat == null,
      sop_snapshot: snapshot, copied_from: source?.season_ref ?? null,
      yield_estimate: initialEstimate(b.crop_code, b.area_ha, persona.me.actor_ref, nowIso()),
    };
    seasons.push(s); allocateHouseholdMock(coop.api.ref, household.household_ref, b.area_ha);
    return json({ season: s }, 201);
  },
}];
