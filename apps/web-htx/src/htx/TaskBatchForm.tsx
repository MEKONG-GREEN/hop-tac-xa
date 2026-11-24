"use client";

import { useEffect, useRef, useState, type FormEvent } from "react";
import { unwrap } from "@/api/client";
import type { components } from "@/api/generated/application";
import { newIdempotencyKey } from "@/api/idempotency";
import { collectPages } from "@/api/pagination";
import type { ApiProblem } from "@/api/problem";
import { useAuth } from "@/auth/AuthProvider";
import { ProblemAlert } from "@/components/ProblemAlert";
import { toApiProblem } from "@/lib/useAsync";

type S = components["schemas"];
type Path = { ref: string; household_ref: string; season_ref: string };
type Candidate = S["HouseholdTaskBatchCandidate"];
type Result = S["HouseholdTaskBatchResult"];
interface MaterialRow { name: string; unit: string; dose: string }

const today = () => new Intl.DateTimeFormat("en-CA", { timeZone: "Asia/Ho_Chi_Minh" }).format(new Date());
const DOSE = /^(0|[1-9][0-9]{0,8})(\.[0-9]{1,4})?$/;

/** Kiểm sớm phía client; máy chủ kiểm lại từng vụ (AH-176). */
export function batchErrors(v: { selected: number; doneOn: string; materials: MaterialRow[]; backfill: string }, now = today()): string[] {
  const out: string[] = [];
  if (!v.selected) out.push("Chọn ít nhất một vụ.");
  if (v.selected > 50) out.push("Chọn tối đa 50 vụ.");
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
 * AH-176 (FN-05-BS-01, KH-17): chọn nhiều vụ cùng SOP gốc có cùng việc chưa xong và ghi một lần. Mỗi vụ một bản ghi
 * dùng chung ngày, liều, ghi chú, GPS; vụ không ghi được bị bỏ qua kèm lý do. Một Idempotency-Key cho mỗi nội dung gửi.
 */
export function TaskBatchForm({ path, task, onSaved, onClose }: {
  path: Path; task: { seq: number; name: string }; onSaved: () => void; onClose: () => void;
}) {
  const { api } = useAuth();
  const [candidates, setCandidates] = useState<Candidate[] | null>(null);
  const [labels, setLabels] = useState<Record<string, string>>({});
  const [chosen, setChosen] = useState<Set<string>>(new Set([path.season_ref]));
  const [state, setState] = useState<"done" | "partial">("done");
  const [doneOn, setDoneOn] = useState(today);
  const [note, setNote] = useState("");
  const [backfill, setBackfill] = useState("");
  const [materials, setMaterials] = useState<MaterialRow[]>([]);
  const [zero, setZero] = useState(false);
  const [errors, setErrors] = useState<string[]>([]);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [results, setResults] = useState<Result[] | null>(null);
  const [busy, setBusy] = useState(false);
  const key = useRef<{ body: string; key: string } | null>(null);
  useEffect(() => {
    let live = true;
    void (async () => {
      try {
        const res = await unwrap(api.GET("/v1/cooperatives/{ref}/households/{household_ref}/seasons/{season_ref}/task-batch-candidates", { params: { path, query: { step_seq: task.seq } } }));
        if (live) setCandidates(res.items);
        try {
          const households = await collectPages((cursor) => unwrap(api.GET("/v1/cooperatives/{ref}/households", { params: { path: { ref: path.ref }, query: { cursor, limit: 200 } } })));
          if (live) setLabels(Object.fromEntries(households.map((h) => [h.household_ref, h.label])));
        } catch { /* nhãn hộ chỉ để hiển thị */ }
      } catch (err) { if (live) setProblem(toApiProblem(err)); }
    })();
    return () => { live = false; };
  }, [api, path, task.seq]);
  const needsZero = materials.some((m) => /^0(?:\.0+)?$/.test(m.dose));
  const toggle = (ref: string) => setChosen((s) => { const n = new Set(s); if (n.has(ref)) n.delete(ref); else n.add(ref); return n; });
  const submit = async (e: FormEvent) => {
    e.preventDefault(); if (busy || !candidates) return;
    const seasons = candidates.filter((c) => c.eligible && chosen.has(c.season_ref));
    const found = batchErrors({ selected: seasons.length, doneOn, materials, backfill });
    setErrors(found); if (found.length) return;
    const body = { step_seq: task.seq, state, done_on: doneOn, note, backfill_reason: backfill.trim(), zero_confirmed: zero, gps: { source: "no_device" as const }, channel: "web" as const,
      materials: materials.map((m) => ({ name: m.name.trim(), unit: m.unit.trim(), dose_per_ha: m.dose })),
      seasons: seasons.map((c) => ({ household_ref: c.household_ref, season_ref: c.season_ref, expected_season_event_id: c.current_event_id })) };
    if (needsZero && !zero) { setErrors(["Xác nhận liều bằng 0 trước khi lưu."]); return; }
    const fingerprint = JSON.stringify(body);
    if (key.current?.body !== fingerprint) key.current = { body: fingerprint, key: newIdempotencyKey() };
    setBusy(true); setProblem(null);
    try {
      const res = await unwrap(api.POST("/v1/cooperatives/{ref}/households/{household_ref}/seasons/{season_ref}/task-batches", {
        params: { path, header: { "Idempotency-Key": key.current.key } }, body,
      }));
      setResults(res.results); onSaved();
    } catch (err) { setProblem(toApiProblem(err)); } finally { setBusy(false); }
  };
  const seasonLabel = (c: { household_ref: string; season_ref: string }) => {
    const row = candidates?.find((x) => x.season_ref === c.season_ref);
    return `${labels[c.household_ref] ?? "Nông hộ"} · vụ ${row?.season_code ?? ""} ${row?.season_year ?? ""}${row?.is_source ? " (vụ đang mở)" : ""}`;
  };
  const setMaterial = (i: number, patch: Partial<MaterialRow>) => setMaterials((rows) => rows.map((m, n) => (n === i ? { ...m, ...patch } : m)));
  if (results) return <section className="card stack" aria-label="Kết quả ghi nhiều vụ">
    <strong>Kết quả ghi &quot;{task.name}&quot;</strong>
    <p role="status">Đã ghi {results.filter((r) => r.status === "recorded").length} vụ, bỏ qua {results.filter((r) => r.status === "skipped").length} vụ.</p>
    <ul>{results.map((r) => <li key={r.season_ref}>{seasonLabel(r)}: {r.status === "recorded" ? "Đã ghi" : `Bỏ qua — ${r.reason ?? r.code}`}</li>)}</ul>
    <button className="btn btn-sm" type="button" onClick={onClose}>Đóng</button>
  </section>;
  return <form className="card stack" aria-label="Ghi nhiều vụ" onSubmit={(e) => void submit(e)}>
    <strong>Ghi &quot;{task.name}&quot; cho nhiều vụ</strong>
    <p className="small mute">Chỉ chọn được vụ cùng SOP gốc có việc này chưa xong. Ảnh không dùng chung giữa các hộ; đính kèm ảnh ở từng vụ.</p>
    {candidates == null && !problem ? <p role="status">Đang tải danh sách vụ…</p> : null}
    {candidates ? <fieldset className="stack" style={{ border: 0, padding: 0, margin: 0 }} disabled={busy}>
      <legend>Vụ</legend>
      {candidates.map((c) => <label key={c.season_ref}>
        <input type="checkbox" disabled={!c.eligible} checked={c.eligible && chosen.has(c.season_ref)} onChange={() => toggle(c.season_ref)} /> {seasonLabel(c)}
        {!c.eligible ? <span className="small mute"> — {c.reason}</span> : null}
      </label>)}
    </fieldset> : null}
    <fieldset disabled={busy || !candidates} className="stack" style={{ border: 0, padding: 0, margin: 0 }}>
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
      {needsZero || !materials.length ? <label><input type="checkbox" checked={zero} onChange={(e) => setZero(e.target.checked)} /> {materials.length ? "Xác nhận liều bằng 0" : "Không dùng vật tư (xác nhận khi quy trình có vật tư khuyến cáo)"}</label> : null}
      <label className="field">Ghi chú<textarea className="input" maxLength={2000} value={note} onChange={(e) => setNote(e.target.value)} /></label>
      <div className="row"><button className="btn btn-p btn-sm" type="submit">{busy ? "Đang lưu…" : "Ghi cho các vụ đã chọn"}</button><button className="btn btn-sm" type="button" onClick={onClose}>Đóng</button></div>
    </fieldset>
    {errors.length ? <ul role="alert">{errors.map((x) => <li key={x}>{x}</li>)}</ul> : null}
    {problem ? <ProblemAlert problem={problem} /> : null}
  </form>;
}
