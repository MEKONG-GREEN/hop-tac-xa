"use client";

import { useRef, useState, type FormEvent } from "react";
import { unwrap } from "@/api/client";
import type { components } from "@/api/generated/application";
import { newIdempotencyKey } from "@/api/idempotency";
import type { ApiProblem } from "@/api/problem";
import { useAuth } from "@/auth/AuthProvider";
import { ProblemAlert } from "@/components/ProblemAlert";
import { toApiProblem } from "@/lib/useAsync";
import { ACTIVITIES, activityLabel } from "./rules";

type S = components["schemas"];
type Path = { ref: string; household_ref: string; season_ref: string };
interface MaterialRow { name: string; unit: string; dose: string }

const today = () => new Intl.DateTimeFormat("en-CA", { timeZone: "Asia/Ho_Chi_Minh" }).format(new Date());
const DOSE = /^(0|[1-9][0-9]{0,8})(\.[0-9]{1,4})?$/;

/** Kiểm sớm phía client; máy chủ kiểm lại (AH-163 AC3). */
export function adhocErrors(v: { name: string; activity: string; stage: string; doneOn: string; materials: MaterialRow[]; backfill: string }, now = today()): string[] {
  const out: string[] = [];
  if (!v.name.trim()) out.push("Nhập nội dung việc.");
  if (!v.activity) out.push("Chọn loại việc.");
  if (!v.stage) out.push("Chọn giai đoạn.");
  if (!/^\d{4}-\d{2}-\d{2}$/.test(v.doneOn) || v.doneOn > now) out.push("Ngày làm không được sau hôm nay.");
  v.materials.forEach((m, i) => {
    if (!m.name.trim() || !m.unit.trim()) out.push(`Vật tư ${i + 1}: nhập tên và đơn vị.`);
    if (!DOSE.test(m.dose)) out.push(`Vật tư ${i + 1}: liều không âm, tối đa 4 chữ số thập phân.`);
  });
  const limit = new Date(Date.parse(`${now}T00:00:00Z`) - 3 * 86400000).toISOString().slice(0, 10);
  if (v.doneOn && v.doneOn < limit && !v.backfill.trim()) out.push("Nhập lý do khi ghi bù quá 3 ngày.");
  return out;
}

/**
 * AH-163 (FN-05.3): ghi việc phát sinh không có trong quy trình — giai đoạn, loại việc, nội dung, vật tư – liều, ngày
 * làm. Bản ghi mang nhãn "Ngoài SOP" và không tính vào % tiến độ. Một Idempotency-Key cho mỗi lần mở form.
 */
export function AdhocTaskForm({ path, event, stages, onSaved, onClose }: {
  path: Path; event: string; stages: { seq: number; name: string }[]; onSaved: (c: S["HouseholdChecklist"]) => void; onClose: () => void;
}) {
  const { api } = useAuth();
  const [name, setName] = useState("");
  const [activity, setActivity] = useState("");
  const [stage, setStage] = useState("");
  const [state, setState] = useState<"done" | "partial">("done");
  const [doneOn, setDoneOn] = useState(today);
  const [note, setNote] = useState("");
  const [backfill, setBackfill] = useState("");
  const [materials, setMaterials] = useState<MaterialRow[]>([]);
  const [zero, setZero] = useState(false);
  const [errors, setErrors] = useState<string[]>([]);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const key = useRef<{ body: string; key: string } | null>(null);
  const needsZero = materials.some((m) => /^0(?:\.0+)?$/.test(m.dose));
  const submit = async (e: FormEvent) => {
    e.preventDefault(); if (busy) return;
    const found = adhocErrors({ name, activity, stage, doneOn, materials, backfill });
    if (needsZero && !zero) found.push("Xác nhận liều bằng 0 trước khi lưu.");
    setErrors(found); if (found.length) return;
    const body = { expected_season_event_id: event, stage_seq: Number(stage), name: name.trim(), activity_code: activity as NonNullable<S["HouseholdPlanChange"]["step"]>["activity_code"],
      state, done_on: doneOn, note, backfill_reason: backfill.trim(), zero_confirmed: zero, gps: { source: "no_device" as const }, channel: "web" as const,
      materials: materials.map((m) => ({ name: m.name.trim(), unit: m.unit.trim(), dose_per_ha: m.dose })) };
    const fingerprint = JSON.stringify(body);
    if (key.current?.body !== fingerprint) key.current = { body: fingerprint, key: newIdempotencyKey() };
    setBusy(true); setProblem(null);
    try {
      const res = await unwrap(api.POST("/v1/cooperatives/{ref}/households/{household_ref}/seasons/{season_ref}/adhoc-records", {
        params: { path, header: { "Idempotency-Key": key.current.key } }, body,
      }));
      onSaved(res.checklist);
    } catch (err) { setProblem(toApiProblem(err)); } finally { setBusy(false); }
  };
  const setMaterial = (i: number, patch: Partial<MaterialRow>) => setMaterials((rows) => rows.map((m, n) => (n === i ? { ...m, ...patch } : m)));
  return <form className="card stack" aria-label="Ghi việc phát sinh" onSubmit={(e) => void submit(e)}>
    <strong>Ghi việc phát sinh (ngoài quy trình)</strong>
    <p className="small mute">Bản ghi mang nhãn &quot;Ngoài SOP&quot; và không tính vào tiến độ của vụ.</p>
    <fieldset disabled={busy} className="stack" style={{ border: 0, padding: 0, margin: 0 }}>
      <label className="field">Giai đoạn<select className="select" value={stage} onChange={(e) => setStage(e.target.value)}><option value="">Chọn giai đoạn</option>{stages.map((s) => <option key={s.seq} value={s.seq}>{s.name}</option>)}</select></label>
      <label className="field">Loại việc<select className="select" value={activity} onChange={(e) => setActivity(e.target.value)}><option value="">Chọn loại việc</option>{ACTIVITIES.map((a) => <option key={a} value={a}>{activityLabel(a)}</option>)}</select></label>
      <label className="field">Nội dung việc<input className="input" maxLength={200} value={name} onChange={(e) => setName(e.target.value)} /></label>
      <label className="field">Kết quả<select className="select" value={state} onChange={(e) => setState(e.target.value as "done" | "partial")}><option value="done">Đã xong hoàn toàn</option><option value="partial">Chưa xong, làm tiếp</option></select></label>
      <label className="field">Ngày làm<input className="input" type="date" value={doneOn} onChange={(e) => setDoneOn(e.target.value)} /></label>
      <label className="field">Lý do ghi bù (nếu quá 3 ngày)<input className="input" maxLength={500} value={backfill} onChange={(e) => setBackfill(e.target.value)} /></label>
      {materials.map((m, i) => <div className="row" key={i}>
        <label className="field">Vật tư {i + 1}<input className="input" value={m.name} onChange={(e) => setMaterial(i, { name: e.target.value })} /></label>
        <label className="field">Đơn vị {i + 1}<input className="input" value={m.unit} onChange={(e) => setMaterial(i, { unit: e.target.value })} /></label>
        <label className="field">Liều/ha {i + 1}<input className="input" inputMode="decimal" value={m.dose} onChange={(e) => setMaterial(i, { dose: e.target.value })} /></label>
        <button type="button" className="btn btn-o btn-sm" onClick={() => setMaterials((rows) => rows.filter((_, n) => n !== i))}>Bỏ vật tư {i + 1}</button>
      </div>)}
      <button type="button" className="btn btn-o btn-sm" onClick={() => setMaterials((rows) => [...rows, { name: "", unit: "", dose: "" }])}>Thêm vật tư</button>
      {needsZero ? <label><input type="checkbox" checked={zero} onChange={(e) => setZero(e.target.checked)} /> Xác nhận liều bằng 0</label> : null}
      <label className="field">Ghi chú<textarea className="input" maxLength={2000} value={note} onChange={(e) => setNote(e.target.value)} /></label>
      <div className="row"><button className="btn btn-p btn-sm" type="submit">{busy ? "Đang lưu…" : "Lưu việc phát sinh"}</button><button className="btn btn-sm" type="button" onClick={onClose}>Đóng</button></div>
    </fieldset>
    {errors.length ? <ul role="alert">{errors.map((x) => <li key={x}>{x}</li>)}</ul> : null}
    {problem ? <ProblemAlert problem={problem} /> : null}
  </form>;
}
