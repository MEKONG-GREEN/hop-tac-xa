"use client";

import { useRef, useState, type FormEvent } from "react";
import { unwrap } from "@/api/client";
import type { components } from "@/api/generated/application";
import { newIdempotencyKey } from "@/api/idempotency";
import type { ApiProblem } from "@/api/problem";
import { useAuth } from "@/auth/AuthProvider";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Loading } from "@/components/ui";
import { formatDateVi, formatDecimalVi } from "@/lib/exactDecimal";
import { toApiProblem, useAsync } from "@/lib/useAsync";

type S = components["schemas"];
type Season = S["HouseholdSeason"];
type Summary = S["HouseholdSeasonSummary"];

const PII = /(?:\+?84|0)\d{9,10}|\b\d{12}\b/;

/** Kiểm sớm chi phí (AH-152): tùy chọn, số nguyên VND không âm, tối đa 12 chữ số (bỏ dấu chấm, phẩy, khoảng trắng). */
export function costError(raw: string): string | null {
  const v = normalizeCost(raw);
  if (!v) return null;
  return /^[0-9]{1,12}$/.test(v) ? null : "Chi phí là số nguyên VND không âm, tối đa 12 chữ số.";
}

export function normalizeCost(raw: string): string {
  return raw.replace(/[\s.,]/g, "");
}

/** Kiểm sớm ghi chú (AH-152): tùy chọn, tối đa 1000 ký tự, không chứa SĐT hoặc CCCD. */
export function noteError(raw: string): string | null {
  const v = raw.trim();
  if ([...v].length > 1000) return "Ghi chú tối đa 1000 ký tự.";
  if (PII.test(v)) return "Ghi chú không được chứa SĐT hoặc CCCD.";
  return null;
}

/** Dòng số việc: sớm, đúng hạn, trễ, bỏ qua, chưa làm (Q-42 hiển thị riêng). */
export function taskLine(t: Summary["tasks"]): string {
  return `Sớm ${t.early} · Đúng hạn ${t.on_time} · Trễ ${t.late} · Bỏ qua ${t.skipped} · Chưa làm ${t.not_done} (tổng ${t.total} việc)`;
}

const tonnes = (v: string) => `${formatDecimalVi(v, { minDecimals: 2, maxDecimals: 2 })} tấn`;

/**
 * AH-152 (FN-06.5, FN-06-B4): tổng kết "Ghi lại vụ" và Hoàn thành vụ (xác nhận hai bước). Vụ đang canh tác đã xong giai
 * đoạn cuối mà chưa khai báo sản lượng thì nhắc khai báo. Vụ đã hoàn thành hiển thị bản chụp cố định kèm sản lượng hiện
 * hành nếu đã sửa sau đó. Đại diện HTX chỉ xem. Một Idempotency-Key cho mỗi nội dung gửi.
 */
export function SeasonSummaryPanel({ coopRef, season, canClose, onSeason }: {
  coopRef: string; season: Season; canClose: boolean; onSeason: (s: Season) => void;
}) {
  const { api } = useAuth();
  const [reload, setReload] = useState(0);
  const [mode, setMode] = useState(false);
  const [cost, setCost] = useState("");
  const [note, setNote] = useState("");
  const [confirm, setConfirm] = useState(false);
  const [errors, setErrors] = useState<string[]>([]);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const key = useRef<{ body: string; key: string } | null>(null);
  const path = { ref: coopRef, household_ref: season.household_ref, season_ref: season.season_ref };
  const loaded = useAsync(() => unwrap(api.GET("/v1/cooperatives/{ref}/households/{household_ref}/seasons/{season_ref}/summary", { params: { path } })),
    [api, coopRef, season.household_ref, season.season_ref, season.current_event_id, reload]);
  const load = () => setReload((v) => v + 1);
  if (season.status === "cancelled") return null;
  if (loaded.error) return <ProblemAlert problem={loaded.error} onRetry={load} />;
  const summary = loaded.data;
  if (!summary) return <Loading label="Đang tải tổng kết vụ…" />;
  const reset = () => { setMode(false); setCost(""); setNote(""); setConfirm(false); setErrors([]); setProblem(null); key.current = null; };
  const submit = async (e: FormEvent) => {
    e.preventDefault(); if (busy) return;
    const errs = [costError(cost), noteError(note)].filter((m): m is string => !!m);
    setErrors(errs); if (errs.length) return;
    if (!confirm) { setConfirm(true); return; }
    const body = { expected_season_event_id: season.current_event_id, cost_vnd: normalizeCost(cost) || undefined, note: note.trim() || undefined, gps: { source: "no_device" as const }, channel: "web" as const };
    const fingerprint = JSON.stringify([body.cost_vnd, body.note]);
    if (key.current?.body !== fingerprint) key.current = { body: fingerprint, key: newIdempotencyKey() };
    setBusy(true); setProblem(null);
    try {
      const r = await unwrap(api.POST("/v1/cooperatives/{ref}/households/{household_ref}/seasons/{season_ref}/close", { params: { path, header: { "Idempotency-Key": key.current.key } }, body }));
      reset(); onSeason(r.season);
    } catch (err) {
      const p = toApiProblem(err);
      setProblem(p); setConfirm(false);
      if (p.status === 409) load();
    } finally { setBusy(false); }
  };
  const closed = summary.status === "closed";
  const current = season.harvest;
  const changed = closed && current && summary.mass_t != null && current.mass_t !== summary.mass_t;
  return <section className="card stack" aria-label="Tổng kết vụ">
    <h4>Tổng kết vụ{closed ? " · Đã hoàn thành vụ" : ""}</h4>
    {summary.harvest_reminder ? <p role="status" className="c-warn">Đã xong việc giai đoạn cuối. Khai báo sản lượng để hoàn thành vụ.</p> : null}
    <dl className="stack">
      <div><dt>Sản lượng</dt><dd>{summary.mass_t != null ? `${tonnes(summary.mass_t)}${summary.harvest_date ? ` (thu hoạch ${formatDateVi(summary.harvest_date)})` : ""}` : "Chưa khai báo"}</dd></div>
      <div><dt>Năng suất</dt><dd>{summary.yield_t_per_ha != null ? `${formatDecimalVi(summary.yield_t_per_ha, { minDecimals: 2, maxDecimals: 2 })} tấn/ha trên ${formatDecimalVi(summary.area_ha, { minDecimals: 2, maxDecimals: 2 })} ha` : "—"}</dd></div>
      {changed ? <div><dt>Sản lượng hiện hành</dt><dd>{tonnes(current.mass_t)} (đã sửa sau khi hoàn thành vụ; tổng kết giữ số lúc hoàn thành)</dd></div> : null}
      <div><dt>Vật tư theo liều thực tế</dt><dd>{summary.materials.length ? <ul>{summary.materials.map((m) => <li key={`${m.name}:${m.unit}`}>{m.name}: {formatDecimalVi(m.qty)} {m.unit}</li>)}</ul> : "Chưa ghi vật tư"}</dd></div>
      <div><dt>Việc theo quy trình</dt><dd>{taskLine(summary.tasks)}</dd></div>
      {closed ? <>
        <div><dt>Chi phí</dt><dd>{summary.cost_vnd ? `${formatDecimalVi(summary.cost_vnd)} đ` : "Không nhập"}</dd></div>
        <div><dt>Ghi chú</dt><dd>{summary.note ?? "Không có"}</dd></div>
        {summary.closed_at ? <div><dt>Hoàn thành vụ</dt><dd>{formatDateVi(summary.closed_at.slice(0, 10))}</dd></div> : null}
      </> : null}
    </dl>
    {!closed && summary.status === "completed" && summary.blockers.includes("last_stage_unresolved") ? <p role="status">Còn việc giai đoạn cuối chưa Xong hoặc Bỏ qua (việc số {summary.unresolved_steps.join(", ")}). Hoàn tất các việc này trước khi hoàn thành vụ.</p> : null}
    {!closed && summary.status === "open" ? <p className="small mute">Khai báo sản lượng trước khi hoàn thành vụ.</p> : null}
    {canClose && summary.can_close && !mode ? <button type="button" className="btn btn-p btn-sm" onClick={() => setMode(true)}>Hoàn thành vụ</button> : null}
    {mode ? <form className="stack" aria-label="Hoàn thành vụ" onSubmit={(e) => void submit(e)}>
      <label className="field">Chi phí cả vụ (đồng, không bắt buộc)<input className="input" inputMode="numeric" value={cost} disabled={busy} onChange={(e) => { setCost(e.target.value); setConfirm(false); }} /></label>
      <label className="field">Ghi chú (không bắt buộc)<textarea className="input" maxLength={1000} value={note} disabled={busy} onChange={(e) => { setNote(e.target.value); setConfirm(false); }} /></label>
      {errors.map((m) => <p key={m} role="alert">{m}</p>)}
      {confirm ? <p role="status">Vụ sẽ chuyển &quot;Đã hoàn thành vụ&quot; và không quay lại; không ghi thêm việc. Bấm &quot;Xác nhận hoàn thành vụ&quot; nếu đúng.</p> : null}
      <div className="row">
        <button className="btn btn-p btn-sm" type="submit" disabled={busy}>{busy ? "Đang lưu…" : confirm ? "Xác nhận hoàn thành vụ" : "Hoàn thành vụ"}</button>
        <button className="btn btn-sm" type="button" disabled={busy} onClick={reset}>Thôi</button>
      </div>
    </form> : null}
    {problem ? <ProblemAlert problem={problem} /> : null}
  </section>;
}
