"use client";

import { useRef, useState, type FormEvent } from "react";
import { unwrap } from "@/api/client";
import type { components } from "@/api/generated/application";
import { newIdempotencyKey } from "@/api/idempotency";
import type { ApiProblem } from "@/api/problem";
import { useAuth } from "@/auth/AuthProvider";
import { ProblemAlert } from "@/components/ProblemAlert";
import { formatDateVi, formatDecimalVi, normalizeNumberInput } from "@/lib/exactDecimal";
import { toApiProblem } from "@/lib/useAsync";
import { harvestErrors, isZeroTonnes } from "./HarvestDeclarationForm";

type S = components["schemas"];
type Season = S["HouseholdSeason"];
type Version = S["HouseholdHarvestVersion"];

const vnToday = () => new Intl.DateTimeFormat("en-CA", { timeZone: "Asia/Ho_Chi_Minh" }).format(new Date());
const KIND: Record<Version["kind"], string> = { declared: "Khai báo", corrected: "Sửa", voided: "Hủy khai báo" };

/** Kiểm sớm lý do (AH-189): tùy chọn khi sửa vụ "Đã thu hoạch", bắt buộc khi hủy và khi sửa vụ đã hoàn thành (AH-152). */
export function reasonError(reason: string, required: boolean): string | null {
  const r = reason.trim();
  if (!r && !required) return null;
  if (r.length < 10 || r.length > 500) return "Nhập lý do (10–500 ký tự).";
  return null;
}

/** Dòng lịch sử cũ → mới: so phiên bản với phiên bản trước nó. */
export function historyLine(v: Version, prev?: Version): string {
  const t = (m: string) => `${formatDecimalVi(m, { minDecimals: 2, maxDecimals: 2 })} tấn`;
  if (v.kind === "voided") return `Hủy khai báo ${t(v.mass_t)} (${formatDateVi(v.harvest_date)})`;
  if (v.kind === "corrected" && prev) {
    const parts: string[] = [];
    if (prev.mass_t !== v.mass_t) parts.push(`${t(prev.mass_t)} → ${t(v.mass_t)}`);
    if (prev.harvest_date !== v.harvest_date) parts.push(`ngày ${formatDateVi(prev.harvest_date)} → ${formatDateVi(v.harvest_date)}`);
    return `Sửa ${parts.join(", ")}`;
  }
  return `Khai báo ${t(v.mass_t)} (${formatDateVi(v.harvest_date)})`;
}

/**
 * AH-189 (FN-06.3, FN-06.4): sửa sản lượng (lý do tùy chọn ở vụ "Đã thu hoạch") và hủy khai báo (lý do bắt buộc, xác
 * nhận hai bước) của vụ đã khai báo; lịch sử các phiên bản. Đại diện HTX chỉ xem lịch sử. Một Idempotency-Key cho mỗi
 * nội dung gửi.
 */
export function HarvestEditPanel({ coopRef, season, canEdit, canVoid, onSeason }: {
  coopRef: string; season: Season; canEdit: boolean; canVoid: boolean; onSeason: (s: Season) => void;
}) {
  const { api } = useAuth();
  const harvest = season.harvest!;
  // AH-152: vụ "Đã hoàn thành vụ" — sửa bắt buộc lý do, không hủy khai báo.
  const closed = season.status === "closed";
  const path = { ref: coopRef, household_ref: season.household_ref, season_ref: season.season_ref };
  const [mode, setMode] = useState<"" | "edit" | "void">("");
  const [mass, setMass] = useState(harvest.mass_t);
  const [date, setDate] = useState(harvest.harvest_date);
  const [reason, setReason] = useState("");
  const [confirm, setConfirm] = useState(false);
  const [errors, setErrors] = useState<string[]>([]);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const [history, setHistory] = useState<Version[] | null>(null);
  const key = useRef<{ body: string; key: string } | null>(null);
  const reset = () => { setMode(""); setMass(harvest.mass_t); setDate(harvest.harvest_date); setReason(""); setConfirm(false); setErrors([]); setProblem(null); key.current = null; };
  const idem = (body: object) => {
    const fingerprint = JSON.stringify(body);
    if (key.current?.body !== fingerprint) key.current = { body: fingerprint, key: newIdempotencyKey() };
    return key.current.key;
  };
  const loadHistory = async () => {
    try { setHistory((await unwrap(api.GET("/v1/cooperatives/{ref}/households/{household_ref}/seasons/{season_ref}/harvest/history", { params: { path } }))).items); }
    catch (err) { setProblem(toApiProblem(err)); }
  };
  const submitEdit = async (e: FormEvent) => {
    e.preventDefault(); if (busy) return;
    const errs = harvestErrors({ mass, date }, season.anchor_date, vnToday());
    const r = reasonError(reason, closed);
    if (r) errs.push(r);
    setErrors(errs); if (errs.length) return;
    const zero = isZeroTonnes(mass);
    if (zero && !confirm) { setConfirm(true); return; }
    const body = { expected_harvest_id: harvest.harvest_id, mass_t: normalizeNumberInput(mass), harvest_date: date, zero_confirmed: zero, reason: reason.trim() || undefined, channel: "web" as const };
    setBusy(true); setProblem(null);
    try {
      const r = await unwrap(api.POST("/v1/cooperatives/{ref}/households/{household_ref}/seasons/{season_ref}/harvest/corrections", {
        params: { path, header: { "Idempotency-Key": idem(body) } }, body }));
      reset(); setHistory(null); onSeason(r.season);
    } catch (err) { setProblem(toApiProblem(err)); } finally { setBusy(false); }
  };
  const submitVoid = async (e: FormEvent) => {
    e.preventDefault(); if (busy) return;
    const r = reasonError(reason, true);
    setErrors(r ? [r] : []); if (r) return;
    if (!confirm) { setConfirm(true); return; }
    const body = { expected_harvest_id: harvest.harvest_id, reason: reason.trim(), channel: "web" as const };
    setBusy(true); setProblem(null);
    try {
      const s = await unwrap(api.POST("/v1/cooperatives/{ref}/households/{household_ref}/seasons/{season_ref}/harvest/void", {
        params: { path, header: { "Idempotency-Key": idem(body) } }, body }));
      reset(); setHistory(null); onSeason(s);
    } catch (err) { setProblem(toApiProblem(err)); } finally { setBusy(false); }
  };
  return <section className="card stack" aria-label="Sửa, hủy khai báo sản lượng">
    <div className="row">
      {canEdit && !mode ? <button type="button" className="btn btn-o btn-sm" onClick={() => setMode("edit")}>Sửa sản lượng</button> : null}
      {canVoid && !closed && !mode ? <button type="button" className="btn btn-o btn-sm" onClick={() => setMode("void")}>Hủy khai báo</button> : null}
      <button type="button" className="btn btn-sm" onClick={() => void loadHistory()}>Xem lịch sử khai báo</button>
    </div>
    {mode === "edit" ? <form className="stack" aria-label="Sửa sản lượng" onSubmit={(e) => void submitEdit(e)}>
      <label className="field">Tổng sản lượng (tấn)<input className="input" inputMode="decimal" value={mass} disabled={busy} onChange={(e) => { setMass(e.target.value); setConfirm(false); }} /></label>
      <label className="field">Ngày thu hoạch cuối<input className="input" type="date" min={season.anchor_date} max={vnToday()} value={date} disabled={busy} onChange={(e) => setDate(e.target.value)} /></label>
      <label className="field">{closed ? "Lý do sửa (bắt buộc với vụ đã hoàn thành)" : "Lý do sửa (không bắt buộc)"}<textarea className="input" maxLength={500} value={reason} disabled={busy} onChange={(e) => setReason(e.target.value)} /></label>
      {errors.map((m) => <p key={m} role="alert">{m}</p>)}
      {confirm ? <p role="status">Sản lượng bằng 0. Bấm &quot;Xác nhận lưu&quot; nếu đúng.</p> : null}
      <div className="row">
        <button className="btn btn-p btn-sm" type="submit" disabled={busy}>{busy ? "Đang lưu…" : confirm ? "Xác nhận lưu" : "Lưu"}</button>
        <button className="btn btn-sm" type="button" disabled={busy} onClick={reset}>Hủy</button>
      </div>
    </form> : null}
    {mode === "void" ? <form className="stack" aria-label="Hủy khai báo" onSubmit={(e) => void submitVoid(e)}>
      <p className="small mute">Vụ sẽ về &quot;Đang canh tác&quot;, việc Thu hoạch tự Xong trở về trạng thái trước. Lịch sử được giữ; có thể khai báo lại.</p>
      <label className="field">Lý do hủy<textarea className="input" maxLength={500} value={reason} disabled={busy} onChange={(e) => { setReason(e.target.value); setConfirm(false); }} /></label>
      {errors.map((m) => <p key={m} role="alert">{m}</p>)}
      {confirm ? <p role="status">Bấm &quot;Xác nhận hủy khai báo&quot; để hủy.</p> : null}
      <div className="row">
        <button className="btn btn-p btn-sm" type="submit" disabled={busy}>{busy ? "Đang hủy…" : confirm ? "Xác nhận hủy khai báo" : "Hủy khai báo"}</button>
        <button className="btn btn-sm" type="button" disabled={busy} onClick={reset}>Thôi</button>
      </div>
    </form> : null}
    {history ? <ol aria-label="Lịch sử khai báo">{history.map((v, i) => <li key={v.harvest_id}>
      <strong>{KIND[v.kind]}</strong> · {historyLine(v, history.slice(0, i).reverse().find((p) => p.kind !== "voided"))} · {formatDateVi(v.declared_at.slice(0, 10))}{v.reason ? ` · Lý do: ${v.reason}` : ""}
    </li>)}</ol> : null}
    {problem ? <ProblemAlert problem={problem} /> : null}
  </section>;
}
