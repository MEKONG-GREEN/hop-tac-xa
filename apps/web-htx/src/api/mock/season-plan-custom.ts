import type { components } from "../generated/application";

/**
 * Mock của lớp tùy chỉnh quy trình của vụ (AH-113) — bản rút gọn của domain.SeasonSnapshot ở máy chủ: việc gốc giữ seq,
 * việc tự thêm cấp seq tiếp theo, sửa ngày/vật tư, bỏ (việc SOP → Bỏ qua, việc tự thêm → xóa), thứ tự hiển thị.
 */
type Snapshot = components["schemas"]["HouseholdSeasonSOPSnapshot"];
type Step = Snapshot["steps"][number];
type Change = components["schemas"]["HouseholdPlanChange"];
type Custom = NonNullable<Snapshot["custom"]>;

export interface PlanError { status: number; code: string; field?: string; reason: string }

const custom = (s: Snapshot): Custom => s.custom ?? {};

/** Việc hiệu lực (đã áp sửa, gồm việc tự thêm) theo giai đoạn rồi thứ tự hiển thị; custom = việc tự thêm. */
export function effectiveSteps(s: Snapshot): (Step & { custom?: boolean })[] {
  const c = custom(s);
  const all = [
    ...s.steps.map((st) => {
      const e = c.edits?.find((e) => e.seq === st.seq);
      return e ? { ...st, window: e.window ?? st.window, materials: e.materials ?? st.materials } : { ...st };
    }),
    ...(c.added ?? []).map((st) => ({ ...st, custom: true })),
  ];
  const pos = (st: Step) => {
    const order = c.order?.find((o) => o.stage_seq === st.stage_seq)?.steps ?? [];
    const i = order.indexOf(st.seq);
    return i < 0 ? 1000 + all.findIndex((x) => x.seq === st.seq) : i;
  };
  return all.sort((a, b) => (a.stage_seq ?? 0) - (b.stage_seq ?? 0) || pos(a) - pos(b))
    .map((st, i, list) => ({ ...st, task_seq: list.slice(0, i + 1).filter((x) => x.stage_seq === st.stage_seq).length }));
}

export const skippedSeqs = (s: Snapshot): number[] => custom(s).skipped ?? [];

function maxSeq(s: Snapshot): number {
  const c = custom(s);
  return Math.max(s.steps.length, ...s.steps.map((x) => x.seq), c.last_seq ?? 0, ...(c.added ?? []).map((x) => x.seq));
}

function windowError(w: Change["window"] | undefined): string | null {
  if (!w) return "Chọn thời gian";
  if (w.kind === "date_range" && (w.from == null || w.to == null || w.from > w.to)) return "Đến phải lớn hơn hoặc bằng Từ";
  return null;
}

const label = (s: Snapshot, seq: number) => s.stages.find((x) => x.seq === seq)?.name ?? String(seq);

/** Áp một thay đổi; trả bản chụp mới và câu mô tả, hoặc lỗi như máy chủ. */
export function applyPlanChange(s: Snapshot, ch: Change, done: Set<number>, logged: Set<number>): { snapshot: Snapshot; summary: string } | PlanError {
  const c: Custom = structuredClone(custom(s));
  const next: Snapshot = { ...s, custom: c };
  const steps = effectiveSteps(s);
  const target = steps.find((x) => x.seq === ch.step_seq);
  if (ch.kind !== "add") {
    if (!target) return { status: 422, code: "validation_failed", field: "step_seq", reason: "việc không thuộc quy trình của vụ" };
    if (done.has(target.seq)) return { status: 409, code: "cropcycle.plan_step_resolved", reason: "Việc đã xong; không sửa, đổi chỗ hay bỏ được" };
    if (skippedSeqs(s).includes(target.seq)) return { status: 409, code: "cropcycle.plan_step_skipped", reason: "Việc đã bỏ qua" };
  }
  switch (ch.kind) {
    case "add": {
      const st = ch.step;
      if (!st?.name.trim()) return { status: 422, code: "validation_failed", field: "step.name", reason: "bắt buộc" };
      if (!s.stages.some((x) => x.seq === st.stage_seq)) return { status: 422, code: "validation_failed", field: "step.stage_seq", reason: "chọn giai đoạn" };
      if (steps.some((x) => x.stage_seq === st.stage_seq && x.name.trim().toLowerCase() === st.name.trim().toLowerCase())) return { status: 422, code: "validation_failed", field: "step.name", reason: "nội dung việc trùng trong cùng giai đoạn" };
      const w = windowError(st.window);
      if (w) return { status: 422, code: "validation_failed", field: "step.window.to", reason: w };
      const seq = maxSeq(s) + 1;
      c.added = [...(c.added ?? []), { seq, name: st.name.trim(), stage_seq: st.stage_seq, activity_code: st.activity_code, window: st.window, materials: st.materials ?? [], day_offset: 0, window_days: 0, mandatory: false }];
      c.last_seq = seq;
      return { snapshot: next, summary: `Thêm việc "${st.name.trim()}" vào giai đoạn "${label(s, st.stage_seq)}"` };
    }
    case "edit": {
      const w = windowError(ch.window);
      if (w) return { status: 422, code: "validation_failed", field: "step.window.to", reason: w };
      if (target!.custom) c.added = (c.added ?? []).map((a) => (a.seq === target!.seq ? { ...a, window: ch.window, materials: ch.materials ?? [] } : a));
      else c.edits = [...(c.edits ?? []).filter((e) => e.seq !== target!.seq), { seq: target!.seq, window: ch.window, materials: ch.materials ?? [] }];
      return { snapshot: next, summary: `Sửa ngày/vật tư của việc "${target!.name}"` };
    }
    case "move": {
      const stage = steps.filter((x) => x.stage_seq === target!.stage_seq).map((x) => x.seq);
      const i = stage.indexOf(target!.seq), j = ch.direction === "down" ? i + 1 : i - 1;
      if (j < 0 || j >= stage.length) return { status: 422, code: "validation_failed", field: "direction", reason: "việc đã ở đầu hoặc cuối giai đoạn" };
      [stage[i], stage[j]] = [stage[j]!, stage[i]!];
      c.order = [...(c.order ?? []).filter((o) => o.stage_seq !== target!.stage_seq), { stage_seq: target!.stage_seq, steps: stage }];
      return { snapshot: next, summary: `Đổi chỗ việc "${target!.name}" ${ch.direction === "down" ? "xuống" : "lên"} trong giai đoạn "${label(s, target!.stage_seq ?? 0)}"` };
    }
    case "remove": {
      if (target!.custom) {
        if (logged.has(target!.seq)) return { status: 409, code: "cropcycle.plan_step_resolved", reason: "Việc đã có nhật ký" };
        c.added = (c.added ?? []).filter((a) => a.seq !== target!.seq);
        return { snapshot: next, summary: `Xóa việc tự thêm "${target!.name}"` };
      }
      c.skipped = [...(c.skipped ?? []), target!.seq];
      return { snapshot: next, summary: `Bỏ việc "${target!.name}" (Bỏ qua)` };
    }
  }
  return { status: 422, code: "validation_failed", field: "kind", reason: "chọn thêm, sửa, đổi chỗ hoặc bỏ việc" };
}

export function applyPlanChanges(s: Snapshot, changes: Change[] | undefined): Snapshot | PlanError {
  let cur = s;
  for (const [i, ch] of (changes ?? []).entries()) {
    const r = applyPlanChange(cur, ch, new Set(), new Set());
    if ("status" in r) return { ...r, field: r.field ? `plan_changes[${i}].${r.field}` : undefined };
    cur = r.snapshot;
  }
  return cur;
}
