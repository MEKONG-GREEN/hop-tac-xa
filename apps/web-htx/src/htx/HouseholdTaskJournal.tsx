"use client";

import { useMemo, useRef, useState, type FormEvent } from "react";
import { unwrap } from "@/api/client";
import type { components } from "@/api/generated/application";
import { newIdempotencyKey } from "@/api/idempotency";
import type { ApiProblem } from "@/api/problem";
import { useAuth } from "@/auth/AuthProvider";
import { matrixAllows } from "@/auth/permissions";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Loading } from "@/components/ui";
import { formatDate } from "@/lib/format";
import { toApiProblem, useAsync } from "@/lib/useAsync";
import { activityLabel } from "./rules";

type S = components["schemas"];
type Record = S["HouseholdTaskRecord"];
type Path = { ref: string; household_ref: string; season_ref: string };
const PAGE = 20;
const STATE: { [k: string]: string } = { done: "Đã xong", partial: "Chưa xong, làm tiếp", skipped: "Bỏ qua", voided: "Đã xóa" };

/** Lý do sửa/xóa: bắt buộc, ít nhất 10 ký tự (máy chủ kiểm lại). */
export function reasonError(reason: string): string | null {
  return reason.trim().length < 10 ? "Nhập lý do (ít nhất 10 ký tự)." : null;
}

function History({ path, record, onClose }: { path: Path; record: Record; onClose: () => void }) {
  const { api, principal } = useAuth();
  const history = useAsync(() => unwrap(api.GET("/v1/cooperatives/{ref}/households/{household_ref}/seasons/{season_ref}/task-records/{log_ref}/history", {
    params: { path: { ...path, log_ref: record.log_ref } },
  })), [api, path.ref, path.household_ref, path.season_ref, record.log_ref]);
  return <section aria-label="Lịch sử bản ghi" className="card stack">
    <strong>Lịch sử bản ghi</strong>
    {history.loading ? <Loading /> : history.error ? <ProblemAlert problem={history.error} onRetry={history.reload} /> : <ol>{history.data?.items.map((v) => <li key={v.id}>
      <span className="small mute">Phiên bản {v.version} · {formatDate(v.recorded_at)} · {v.actor_ref === principal?.subject ? "Bạn" : "Người dùng khác"}</span>
      <div>{STATE[v.state] ?? v.state} · ngày làm {formatDate(v.done_on)}{v.inputs.map((i) => ` · ${i.name}: ${i.qty} ${i.unit}`).join("")}{v.note ? ` · ${v.note}` : ""}</div>
      {v.correction_reason ? <div className="small">Lý do: {v.correction_reason}</div> : null}
    </li>)}</ol>}
    <button type="button" className="btn btn-sm" onClick={onClose}>Đóng</button>
  </section>;
}

function EditForm({ path, record, onDone, onCancel }: { path: Path; record: Record; onDone: (c: S["HouseholdChecklist"]) => void; onCancel: () => void }) {
  const { api } = useAuth();
  const [state, setState] = useState<"done" | "partial">(record.state === "partial" ? "partial" : "done");
  const [doneOn, setDoneOn] = useState(record.done_on);
  const [note, setNote] = useState(record.note ?? "");
  const [materials, setMaterials] = useState(() => (record.details?.actual_materials ?? []).map((m) => ({ material_code: m.material_code ?? "", name: m.name, unit: m.unit, dose_per_ha: m.dose_per_ha })));
  const [zero, setZero] = useState(record.details?.zero_confirmed ?? false);
  const [reason, setReason] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const key = useRef<{ body: string; key: string } | null>(null);
  const needsZero = materials.some((m) => /^0(?:\.0+)?$/.test(m.dose_per_ha));
  const submit = async (e: FormEvent) => {
    e.preventDefault(); if (busy) return;
    const invalid = reasonError(reason); if (invalid) { setError(invalid); return; }
    if (needsZero && !zero) { setError("Xác nhận liều bằng 0 trước khi lưu."); return; }
    const body = { expected_record_id: record.id, reason: reason.trim(), state, done_on: doneOn, note, materials: materials.map((m) => m.material_code ? { material_code: m.material_code, unit: m.unit, dose_per_ha: m.dose_per_ha } : m), zero_confirmed: zero, channel: "web" as const };
    const fingerprint = JSON.stringify(body);
    if (key.current?.body !== fingerprint) key.current = { body: fingerprint, key: newIdempotencyKey() };
    setBusy(true); setError(null); setProblem(null);
    try {
      const res = await unwrap(api.POST("/v1/cooperatives/{ref}/households/{household_ref}/seasons/{season_ref}/task-records/{log_ref}/corrections", {
        params: { path: { ...path, log_ref: record.log_ref }, header: { "Idempotency-Key": key.current.key } }, body,
      }));
      onDone(res.checklist);
    } catch (err) { setProblem(toApiProblem(err)); } finally { setBusy(false); }
  };
  return <form className="card stack" aria-label="Sửa bản ghi" onSubmit={(e) => void submit(e)}>
    <strong>Sửa bản ghi</strong>
    <p className="small mute">GPS, ảnh và thời điểm ghi giữ như bản đầu. Lịch sử giữ giá trị cũ.</p>
    <fieldset disabled={busy} className="stack" style={{ border: 0, padding: 0, margin: 0 }}>
      <label className="field">Kết quả<select className="select" value={state} onChange={(e) => setState(e.target.value as "done" | "partial")}><option value="done">Đã xong hoàn toàn</option><option value="partial">Chưa xong, làm tiếp</option></select></label>
      <label className="field">Ngày làm xong<input className="input" type="date" value={doneOn} onChange={(e) => setDoneOn(e.target.value)} /></label>
      {materials.map((m, i) => <label className="field" key={i}>Liều/ha {m.name} ({m.unit})<input className="input" inputMode="decimal" value={m.dose_per_ha} onChange={(e) => setMaterials((rows) => rows.map((r, n) => n === i ? { ...r, dose_per_ha: e.target.value } : r))} /></label>)}
      {needsZero ? <label><input type="checkbox" checked={zero} onChange={(e) => setZero(e.target.checked)} /> Xác nhận liều bằng 0</label> : null}
      <label className="field">Ghi chú<textarea className="input" maxLength={2000} value={note} onChange={(e) => setNote(e.target.value)} /></label>
      <label className="field">Lý do sửa *<textarea className="input" maxLength={500} value={reason} onChange={(e) => setReason(e.target.value)} /></label>
      <div className="row"><button className="btn btn-p btn-sm" type="submit">{busy ? "Đang lưu…" : "Lưu sửa"}</button><button className="btn btn-sm" type="button" onClick={onCancel}>Hủy</button></div>
    </fieldset>
    {error ? <p role="alert">{error}</p> : null}
    {problem ? <ProblemAlert problem={problem} /> : null}
  </form>;
}

function VoidForm({ path, record, untick, onDone, onCancel }: { path: Path; record: Record; untick: boolean; onDone: (c: S["HouseholdChecklist"]) => void; onCancel: () => void }) {
  const { api } = useAuth();
  const [reason, setReason] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const key = useRef<{ body: string; key: string } | null>(null);
  const title = untick ? "Bỏ tích việc đã xong" : "Xóa bản ghi";
  const submit = async (e: FormEvent) => {
    e.preventDefault(); if (busy) return;
    const invalid = reasonError(reason); if (invalid) { setError(invalid); return; }
    const body = { expected_record_id: record.id, reason: reason.trim(), channel: "web" as const };
    const fingerprint = JSON.stringify(body);
    if (key.current?.body !== fingerprint) key.current = { body: fingerprint, key: newIdempotencyKey() };
    setBusy(true); setError(null); setProblem(null);
    try {
      const res = await unwrap(api.POST("/v1/cooperatives/{ref}/households/{household_ref}/seasons/{season_ref}/task-records/{log_ref}/void", {
        params: { path: { ...path, log_ref: record.log_ref }, header: { "Idempotency-Key": key.current.key } }, body,
      }));
      onDone(res.checklist);
    } catch (err) { setProblem(toApiProblem(err)); } finally { setBusy(false); }
  };
  return <form className="card stack" aria-label={title} onSubmit={(e) => void submit(e)}>
    <strong>{title}</strong>
    <p className="small mute">{untick ? "Nếu đây là bản ghi duy nhất, việc trở về chưa xong. " : ""}Bản ghi bị ẩn, không xóa hẳn; lịch sử vẫn xem được.</p>
    <label className="field">Lý do *<textarea className="input" maxLength={500} value={reason} disabled={busy} onChange={(e) => setReason(e.target.value)} /></label>
    <div className="row"><button className="btn btn-p btn-sm" type="submit" disabled={busy}>{busy ? "Đang lưu…" : untick ? "Bỏ tích" : "Xóa"}</button><button className="btn btn-sm" type="button" disabled={busy} onClick={onCancel}>Hủy</button></div>
    {error ? <p role="alert">{error}</p> : null}
    {problem ? <ProblemAlert problem={problem} /> : null}
  </form>;
}

/**
 * AH-126 (FN-05.4): nhật ký của vụ — lọc theo giai đoạn, loại việc, ngày; tải dần; sửa, xóa mềm, bỏ tích kèm lý do;
 * xem lịch sử. Đại diện HTX chỉ xem (KHMV-FN-05 R).
 */
export function HouseholdTaskJournal({ path, checklist, writable, onChanged }: {
  path: Path; checklist: S["HouseholdChecklist"]; writable: boolean; onChanged: (c: S["HouseholdChecklist"]) => void;
}) {
  const { principal } = useAuth();
  const canEdit = writable && matrixAllows(principal, "KHMV-FN-05", "U");
  const canDelete = writable && matrixAllows(principal, "KHMV-FN-05", "D");
  const [stage, setStage] = useState("");
  const [activity, setActivity] = useState("");
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");
  const [shown, setShown] = useState(PAGE);
  const [open, setOpen] = useState<{ kind: "edit" | "void" | "history"; record: Record } | null>(null);
  const tasks = useMemo(() => new Map(checklist.progress.stages.flatMap((s) => s.tasks.map((t) => [t.seq, { ...t, stageName: s.name, stageSeq: s.seq }] as const))), [checklist]);
  const activities = useMemo(() => Array.from(new Set(Array.from(tasks.values()).map((t) => t.activity_code))), [tasks]);
  const rows = checklist.records
    .filter((r) => (!stage || String(tasks.get(r.step_seq)?.stageSeq) === stage) && (!activity || tasks.get(r.step_seq)?.activity_code === activity) && (!from || r.done_on >= from) && (!to || r.done_on <= to))
    .sort((a, b) => b.done_on.localeCompare(a.done_on) || b.recorded_at.localeCompare(a.recorded_at));
  const done = (c: S["HouseholdChecklist"]) => { setOpen(null); onChanged(c); };
  return <section aria-label="Nhật ký của vụ" className="stack">
    <h4>Nhật ký của vụ</h4>
    <div className="row">
      <label className="field">Giai đoạn<select className="select" value={stage} onChange={(e) => { setStage(e.target.value); setShown(PAGE); }}><option value="">Mọi giai đoạn</option>{checklist.progress.stages.map((s) => <option key={s.seq} value={s.seq}>{s.name}</option>)}</select></label>
      <label className="field">Loại việc<select className="select" value={activity} onChange={(e) => { setActivity(e.target.value); setShown(PAGE); }}><option value="">Mọi loại việc</option>{activities.map((a) => <option key={a} value={a}>{activityLabel(a)}</option>)}</select></label>
      <label className="field">Từ ngày<input className="input" type="date" value={from} onChange={(e) => { setFrom(e.target.value); setShown(PAGE); }} /></label>
      <label className="field">Đến ngày<input className="input" type="date" value={to} onChange={(e) => { setTo(e.target.value); setShown(PAGE); }} /></label>
    </div>
    {!rows.length ? <p className="small mute">Chưa có bản ghi phù hợp.</p> : <ul className="stack">{rows.slice(0, shown).map((r) => {
      const task = tasks.get(r.step_seq);
      return <li key={r.id}>
        <div><strong>{task?.name ?? `Việc ${r.step_seq}`}</strong> · {task?.stageName} · {formatDate(r.done_on)} · {STATE[r.state] ?? r.state}{r.inputs.map((i) => ` · ${i.name}: ${i.qty} ${i.unit}`).join("")}{r.note ? ` · ${r.note}` : ""}{(r.version ?? 1) > 1 ? " · Đã sửa" : ""}</div>
        <div className="row">
          {canEdit ? <button type="button" className="btn btn-o btn-sm" onClick={() => setOpen({ kind: "edit", record: r })}>Sửa</button> : null}
          {canDelete ? <button type="button" className="btn btn-o btn-sm" onClick={() => setOpen({ kind: "void", record: r })}>{r.state === "done" ? "Bỏ tích" : "Xóa"}</button> : null}
          <button type="button" className="btn btn-o btn-sm" onClick={() => setOpen({ kind: "history", record: r })}>Lịch sử</button>
        </div>
        {open?.record.id === r.id && open.kind === "edit" ? <EditForm path={path} record={r} onDone={done} onCancel={() => setOpen(null)} /> : null}
        {open?.record.id === r.id && open.kind === "void" ? <VoidForm path={path} record={r} untick={r.state === "done"} onDone={done} onCancel={() => setOpen(null)} /> : null}
        {open?.record.id === r.id && open.kind === "history" ? <History path={path} record={r} onClose={() => setOpen(null)} /> : null}
      </li>;
    })}</ul>}
    {rows.length > shown ? <button type="button" className="btn btn-o btn-sm" onClick={() => setShown((n) => n + PAGE)}>Tải thêm</button> : null}
  </section>;
}
