"use client";

import { useState, type FormEvent } from "react";
import type { components } from "@/api/generated/application";
import type { ApiProblem } from "@/api/problem";
import { ProblemAlert } from "@/components/ProblemAlert";
import { formatDate } from "@/lib/format";
import { toApiProblem } from "@/lib/useAsync";
import { ACTIVITIES, activityLabel } from "./rules";

type S = components["schemas"];
export type PlanChange = S["HouseholdPlanChange"];
type Window = S["CropSOPWindow"];
type Material = S["CropSOPMaterial"];

/** Một việc trong quy trình hiệu lực của vụ (lịch xem trước hoặc checklist). */
export interface PlanTask {
  seq: number;
  stage_seq: number;
  name: string;
  custom?: boolean;
  from_date: string | null;
  to_date: string | null;
  undated: boolean;
  /** Việc đã xong: khóa, không sửa, đổi chỗ, bỏ (AH-113 AC3). */
  locked?: boolean;
  /** Việc của SOP đã bỏ: "Bỏ qua". */
  skipped?: boolean;
}
export interface PlanStage { seq: number; name: string; tasks: PlanTask[] }

/** Cửa sổ và vật tư hiện tại của việc theo seq, lấy từ bản chụp (đã áp tùy chỉnh). */
export type StepLookup = (seq: number) => { window?: Window | null; materials?: Material[] | null } | undefined;

/** Việc hiệu lực từ bản chụp SOP của vụ: việc gốc (áp edits) và việc tự thêm. */
export function snapshotLookup(snapshot: S["HouseholdSeasonSOPSnapshot"] | undefined): StepLookup {
  return (seq) => {
    if (!snapshot) return undefined;
    const custom = snapshot.custom;
    const added = custom?.added?.find((s) => s.seq === seq);
    if (added) return { window: added.window, materials: added.materials };
    const edit = custom?.edits?.find((e) => e.seq === seq);
    if (edit) return { window: edit.window, materials: edit.materials };
    const step = snapshot.steps.find((s) => s.seq === seq);
    return step ? { window: step.window, materials: step.materials } : undefined;
  };
}

interface MaterialRow { name: string; dose: string; unit: string }
interface Draft { name: string; activity: string; kind: "date_range" | "none"; from: string; to: string; materials: MaterialRow[] }

const blankDraft = (): Draft => ({ name: "", activity: "", kind: "date_range", from: "", to: "", materials: [] });
const INT = /^-?\d{1,4}$/;
const DOSE = /^(0|[1-9][0-9]{0,8})(\.[0-9]{1,4})?$/;

/** Kiểm sớm phía client; máy chủ kiểm lại theo luật của SOP. */
function draftErrors(d: Draft, needName: boolean): string[] {
  const out: string[] = [];
  if (needName && !d.name.trim()) out.push("Nhập nội dung việc.");
  if (needName && !d.activity) out.push("Chọn loại việc.");
  if (d.kind === "date_range") {
    if (!INT.test(d.from) || !INT.test(d.to)) out.push("Nhập Từ và Đến (số ngày so với mốc gốc).");
    else if (Number(d.from) > Number(d.to)) out.push("Đến phải lớn hơn hoặc bằng Từ.");
  }
  d.materials.forEach((m, i) => {
    if (!m.name.trim() || !m.unit.trim()) out.push(`Vật tư ${i + 1}: nhập tên và đơn vị.`);
    if (!DOSE.test(m.dose) || !/[1-9]/.test(m.dose)) out.push(`Vật tư ${i + 1}: liều phải lớn hơn 0.`);
  });
  return out;
}

function toWindow(d: Draft): Window {
  return d.kind === "none" ? { kind: "none" } : { kind: "date_range", anchor: "nxg", from: Number(d.from), to: Number(d.to) };
}
function toMaterials(d: Draft): Material[] {
  return d.materials.map((m) => ({ name: m.name.trim(), dose_per_ha: m.dose, unit: m.unit.trim() }));
}
function fromStep(step: ReturnType<StepLookup>): Draft {
  const w = step?.window;
  const ranged = w?.kind === "date_range" && w.anchor === "nxg";
  return {
    ...blankDraft(),
    kind: ranged ? "date_range" : "none",
    from: ranged && w?.from != null ? String(w.from) : "",
    to: ranged && w?.to != null ? String(w.to) : "",
    materials: (step?.materials ?? []).map((m) => ({ name: m.name || m.material_code || "", dose: m.dose_per_ha, unit: m.unit })),
  };
}

function StepForm({ title, needName, initial, onCancel, onSubmit }: {
  title: string; needName: boolean; initial: Draft; onCancel: () => void; onSubmit: (d: Draft) => Promise<void>;
}) {
  const [d, setD] = useState(initial);
  const [errors, setErrors] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const set = (patch: Partial<Draft>) => setD((v) => ({ ...v, ...patch }));
  const setMaterial = (i: number, patch: Partial<MaterialRow>) => set({ materials: d.materials.map((m, j) => (j === i ? { ...m, ...patch } : m)) });
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const found = draftErrors(d, needName);
    setErrors(found);
    if (found.length || busy) return;
    setBusy(true);
    try { await onSubmit(d); } finally { setBusy(false); }
  };
  return <form className="stack card" aria-label={title} onSubmit={(e) => void submit(e)}>
    <strong>{title}</strong>
    {needName ? <>
      <label className="field">Nội dung việc<input className="input" value={d.name} maxLength={200} onChange={(e) => set({ name: e.target.value })} /></label>
      <label className="field">Loại việc<select className="select" value={d.activity} onChange={(e) => set({ activity: e.target.value })}><option value="">Chọn loại việc</option>{ACTIVITIES.map((a) => <option key={a} value={a}>{activityLabel(a)}</option>)}</select></label>
    </> : null}
    <label className="field">Thời gian<select className="select" value={d.kind} onChange={(e) => set({ kind: e.target.value as Draft["kind"] })}><option value="date_range">Theo ngày (so với mốc gốc)</option><option value="none">Không ngày</option></select></label>
    {d.kind === "date_range" ? <div className="row">
      <label className="field">Từ (ngày)<input className="input" inputMode="numeric" value={d.from} onChange={(e) => set({ from: e.target.value })} /></label>
      <label className="field">Đến (ngày)<input className="input" inputMode="numeric" value={d.to} onChange={(e) => set({ to: e.target.value })} /></label>
    </div> : null}
    <fieldset className="stack" style={{ border: 0, padding: 0, margin: 0 }}>
      <legend>Vật tư (liều theo ha)</legend>
      {d.materials.map((m, i) => <div className="row" key={i}>
        <label className="field">Tên vật tư {i + 1}<input className="input" value={m.name} onChange={(e) => setMaterial(i, { name: e.target.value })} /></label>
        <label className="field">Liều/ha {i + 1}<input className="input" inputMode="decimal" value={m.dose} onChange={(e) => setMaterial(i, { dose: e.target.value })} /></label>
        <label className="field">Đơn vị {i + 1}<input className="input" value={m.unit} onChange={(e) => setMaterial(i, { unit: e.target.value })} /></label>
        <button type="button" className="btn btn-o btn-sm" onClick={() => set({ materials: d.materials.filter((_, j) => j !== i) })}>Bỏ vật tư {i + 1}</button>
      </div>)}
      <button type="button" className="btn btn-o btn-sm" onClick={() => set({ materials: [...d.materials, { name: "", dose: "", unit: "" }] })}>Thêm vật tư</button>
    </fieldset>
    {errors.length ? <ul role="alert">{errors.map((e) => <li key={e}>{e}</li>)}</ul> : null}
    <div className="row">
      <button className="btn btn-p btn-sm" type="submit" disabled={busy}>{busy ? "Đang lưu…" : "Lưu"}</button>
      <button className="btn btn-sm" type="button" disabled={busy} onClick={onCancel}>Hủy</button>
    </div>
  </form>;
}

const dates = (t: PlanTask) => t.undated ? "Không ngày" : !t.from_date ? "Chờ mốc" : t.to_date ? `${formatDate(t.from_date)} – ${formatDate(t.to_date)}` : `Từ ${formatDate(t.from_date)}`;

/**
 * AH-113 (UX-07): tùy chỉnh quy trình của riêng vụ — thêm việc, đổi chỗ (▲▼), sửa ngày/vật tư (✎), bỏ việc (✕).
 * Khi mở vụ, thay đổi được gom lại gửi kèm yêu cầu mở vụ; khi vụ đang chạy, mỗi thay đổi gửi ngay (onChange).
 * Việc đã xong bị khóa; việc của SOP bị bỏ hiện "Bỏ qua"; việc tự thêm có nhãn.
 */
export function SeasonPlanEditor({ stages, lookup, onChange, disabled = false }: {
  stages: PlanStage[]; lookup?: StepLookup; onChange: (change: PlanChange) => Promise<void>; disabled?: boolean;
}) {
  const [form, setForm] = useState<{ kind: "add"; stage: number } | { kind: "edit"; task: PlanTask } | null>(null);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const run = async (change: PlanChange) => {
    setBusy(true); setProblem(null);
    try { await onChange(change); setForm(null); }
    catch (e) { setProblem(toApiProblem(e)); throw e; }
    finally { setBusy(false); }
  };
  const quick = (change: PlanChange) => { void run(change).catch(() => undefined); };
  const off = disabled || busy;
  return <section aria-label="Tùy chỉnh quy trình của vụ" className="stack">
    <h4>Tùy chỉnh quy trình của vụ</h4>
    <p className="small mute">Chỉ áp cho vụ này; SOP chuẩn không đổi. Việc đã xong không sửa, đổi chỗ hay bỏ được.</p>
    {problem ? <ProblemAlert problem={problem} /> : null}
    {stages.map((stage) => <section key={stage.seq} aria-label={`Giai đoạn ${stage.name}`}>
      <h5>{stage.seq}. {stage.name}</h5>
      <ul className="stack">{stage.tasks.map((t, i) => {
        const fixed = t.locked || t.skipped;
        return <li key={t.seq} className="row" style={{ alignItems: "center" }}>
          <span>{t.name}{t.custom ? <span className="chip c-info"> Tự thêm</span> : null}{t.locked ? <span className="chip c-ok"> Đã xong 🔒</span> : null}{t.skipped ? <span className="chip c-mute"> Bỏ qua</span> : null}</span>
          <span className="small mute">{dates(t)}</span>
          {!fixed ? <>
            <button type="button" className="btn btn-o btn-sm" aria-label={`Đưa ${t.name} lên`} disabled={off || i === 0} onClick={() => quick({ kind: "move", step_seq: t.seq, direction: "up" })}>▲</button>
            <button type="button" className="btn btn-o btn-sm" aria-label={`Đưa ${t.name} xuống`} disabled={off || i === stage.tasks.length - 1} onClick={() => quick({ kind: "move", step_seq: t.seq, direction: "down" })}>▼</button>
            <button type="button" className="btn btn-o btn-sm" aria-label={`Sửa ${t.name}`} disabled={off} onClick={() => setForm({ kind: "edit", task: t })}>✎</button>
            <button type="button" className="btn btn-o btn-sm" aria-label={`Bỏ ${t.name}`} disabled={off} onClick={() => quick({ kind: "remove", step_seq: t.seq })}>✕</button>
          </> : null}
        </li>;
      })}</ul>
      {form?.kind === "add" && form.stage === stage.seq
        ? <StepForm title={`Thêm việc vào ${stage.name}`} needName initial={blankDraft()} onCancel={() => setForm(null)}
          onSubmit={(d) => run({ kind: "add", step: { name: d.name.trim(), stage_seq: stage.seq, activity_code: d.activity as NonNullable<PlanChange["step"]>["activity_code"], window: toWindow(d), materials: toMaterials(d) } }).catch(() => undefined)} />
        : <button type="button" className="btn btn-o btn-sm" disabled={off} onClick={() => setForm({ kind: "add", stage: stage.seq })}>＋ Thêm việc vào {stage.name}</button>}
      {form?.kind === "edit" && form.task.stage_seq === stage.seq
        ? <StepForm key={form.task.seq} title={`Sửa ${form.task.name}`} needName={false} initial={fromStep(lookup?.(form.task.seq))} onCancel={() => setForm(null)}
          onSubmit={(d) => run({ kind: "edit", step_seq: form.task.seq, window: toWindow(d), materials: toMaterials(d) }).catch(() => undefined)} />
        : null}
    </section>)}
  </section>;
}
