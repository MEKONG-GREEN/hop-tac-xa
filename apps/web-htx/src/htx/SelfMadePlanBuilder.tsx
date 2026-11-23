"use client";

import { useState } from "react";
import type { components } from "@/api/generated/application";
import { ACTIVITIES, activityLabel } from "./rules";

type S = components["schemas"];
export type SelfMadePlan = { stages: S["CropSOPStage"][]; steps: S["CropSOPStep"][] };

interface TaskDraft { name: string; activity: string; kind: "date_range" | "none"; from: string; to: string }
interface StageDraft { name: string; from: string; to: string; tasks: TaskDraft[] }

const INT = /^-?\d{1,4}$/;
const blankTask = (): TaskDraft => ({ name: "", activity: "", kind: "date_range", from: "", to: "" });

/** Kiểm sớm phía client; máy chủ kiểm lại bằng luật soạn SOP (AH-162 AC2). */
export function selfMadeErrors(stages: StageDraft[]): string[] {
  const out: string[] = [];
  if (!stages.length || !stages.some((s) => s.tasks.length)) out.push("Nhập ít nhất một giai đoạn và một việc.");
  stages.forEach((s, i) => {
    if (!s.name.trim()) out.push(`Giai đoạn ${i + 1}: nhập tên.`);
    if ((s.from || s.to) && (!INT.test(s.from) || !INT.test(s.to) || Number(s.from) > Number(s.to))) out.push(`Giai đoạn ${i + 1}: Từ, Đến là số ngày, Từ ≤ Đến.`);
    s.tasks.forEach((t, j) => {
      if (!t.name.trim() || !t.activity) out.push(`Giai đoạn ${i + 1}, việc ${j + 1}: nhập nội dung và loại việc.`);
      if (t.kind === "date_range" && (!INT.test(t.from) || !INT.test(t.to) || Number(t.from) > Number(t.to))) out.push(`Giai đoạn ${i + 1}, việc ${j + 1}: Từ, Đến là số ngày, Từ ≤ Đến.`);
    });
  });
  return out;
}

/** Đổi bản nháp thành nội dung quy trình (seq liên tục theo thứ tự nhập). */
export function toSelfMadePlan(stages: StageDraft[]): SelfMadePlan {
  const steps: S["CropSOPStep"][] = [];
  stages.forEach((s, i) => s.tasks.forEach((t, j) => steps.push({
    seq: steps.length + 1, stage_seq: i + 1, task_seq: j + 1, name: t.name.trim(), activity_code: t.activity as S["CropSOPStep"]["activity_code"],
    window: t.kind === "none" ? { kind: "none" } : { kind: "date_range", anchor: "nxg", from: Number(t.from), to: Number(t.to) },
    day_offset: 0, window_days: 0, mandatory: false,
  })));
  return {
    stages: stages.map((s, i) => ({ seq: i + 1, name: s.name.trim(), from_day: s.from ? Number(s.from) : null, to_day: s.to ? Number(s.to) : null })),
    steps,
  };
}

/**
 * AH-162 (FN-03-EX-03, KH-12): soạn quy trình tự tạo khi loại cây chưa có SOP đang áp dụng — giai đoạn và việc cùng
 * cấu trúc SOP, chỉ gắn vụ này. Không có việc Thu hoạch thì không có ngày thu hoạch dự kiến.
 */
export function SelfMadePlanBuilder({ onChange }: { onChange: (plan: SelfMadePlan | null, anchor: string) => void }) {
  const [anchor, setAnchor] = useState("Ngày xuống giống");
  const [stages, setStages] = useState<StageDraft[]>([{ name: "", from: "", to: "", tasks: [blankTask()] }]);
  const errors = selfMadeErrors(stages);
  const update = (next: StageDraft[], nextAnchor = anchor) => {
    setStages(next);
    onChange(selfMadeErrors(next).length ? null : toSelfMadePlan(next), nextAnchor);
  };
  const setStage = (i: number, patch: Partial<StageDraft>) => update(stages.map((s, n) => (n === i ? { ...s, ...patch } : s)));
  const setTask = (i: number, j: number, patch: Partial<TaskDraft>) => setStage(i, { tasks: stages[i]!.tasks.map((t, n) => (n === j ? { ...t, ...patch } : t)) });
  return <section aria-label="Quy trình tự tạo" className="stack card">
    <strong>Quy trình tự tạo (chỉ dùng cho vụ này)</strong>
    <p className="small mute">Ngày tính theo số ngày so với mốc gốc (số âm là trước mốc). Không có việc loại Thu hoạch thì vụ không có ngày thu hoạch dự kiến.</p>
    <label className="field">Tên mốc gốc<input className="input" maxLength={100} value={anchor} onChange={(e) => { setAnchor(e.target.value); update(stages, e.target.value); }} /></label>
    {stages.map((s, i) => <fieldset key={i} className="stack" style={{ border: "1px solid var(--line)", padding: 8 }}>
      <legend>Giai đoạn {i + 1}</legend>
      <div className="row">
        <label className="field">Tên giai đoạn {i + 1}<input className="input" value={s.name} onChange={(e) => setStage(i, { name: e.target.value })} /></label>
        <label className="field">Từ ngày (giai đoạn {i + 1})<input className="input" inputMode="numeric" value={s.from} onChange={(e) => setStage(i, { from: e.target.value })} /></label>
        <label className="field">Đến ngày (giai đoạn {i + 1})<input className="input" inputMode="numeric" value={s.to} onChange={(e) => setStage(i, { to: e.target.value })} /></label>
      </div>
      {s.tasks.map((t, j) => <div className="row" key={j}>
        <label className="field">Việc {i + 1}.{j + 1}<input className="input" value={t.name} onChange={(e) => setTask(i, j, { name: e.target.value })} /></label>
        <label className="field">Loại việc {i + 1}.{j + 1}<select className="select" value={t.activity} onChange={(e) => setTask(i, j, { activity: e.target.value })}><option value="">Chọn loại việc</option>{ACTIVITIES.map((a) => <option key={a} value={a}>{activityLabel(a)}</option>)}</select></label>
        <label className="field">Thời gian {i + 1}.{j + 1}<select className="select" value={t.kind} onChange={(e) => setTask(i, j, { kind: e.target.value as TaskDraft["kind"] })}><option value="date_range">Theo ngày</option><option value="none">Không ngày</option></select></label>
        {t.kind === "date_range" ? <>
          <label className="field">Từ {i + 1}.{j + 1}<input className="input" inputMode="numeric" value={t.from} onChange={(e) => setTask(i, j, { from: e.target.value })} /></label>
          <label className="field">Đến {i + 1}.{j + 1}<input className="input" inputMode="numeric" value={t.to} onChange={(e) => setTask(i, j, { to: e.target.value })} /></label>
        </> : null}
        <button type="button" className="btn btn-o btn-sm" onClick={() => setStage(i, { tasks: s.tasks.filter((_, n) => n !== j) })}>Bỏ việc {i + 1}.{j + 1}</button>
      </div>)}
      <div className="row">
        <button type="button" className="btn btn-o btn-sm" onClick={() => setStage(i, { tasks: [...s.tasks, blankTask()] })}>＋ Thêm việc vào giai đoạn {i + 1}</button>
        {stages.length > 1 ? <button type="button" className="btn btn-o btn-sm" onClick={() => update(stages.filter((_, n) => n !== i))}>Bỏ giai đoạn {i + 1}</button> : null}
      </div>
    </fieldset>)}
    <button type="button" className="btn btn-o btn-sm" onClick={() => update([...stages, { name: "", from: "", to: "", tasks: [blankTask()] }])}>＋ Thêm giai đoạn</button>
    {errors.length ? <ul className="small" role="status">{errors.map((e) => <li key={e}>{e}</li>)}</ul> : null}
  </section>;
}
