"use client";

import { useRef, useState, type FormEvent } from "react";
import { unwrap } from "@/api/client";
import type { components } from "@/api/generated/application";
import { newIdempotencyKey } from "@/api/idempotency";
import type { ApiProblem } from "@/api/problem";
import { useAuth } from "@/auth/AuthProvider";
import { ProblemAlert } from "@/components/ProblemAlert";
import { normalizeNumberInput } from "@/lib/exactDecimal";
import { toApiProblem } from "@/lib/useAsync";
import { captureGPS } from "./HouseholdChecklist";

type S = components["schemas"];
type Season = S["HouseholdSeason"];

const vnToday = () => new Intl.DateTimeFormat("en-CA", { timeZone: "Asia/Ho_Chi_Minh" }).format(new Date());

/** Kiểm sớm phía client (AH-100 AC3, AC4; KH-26); máy chủ kiểm lại. */
export function harvestErrors(v: { mass: string; date: string }, anchor: string, today: string): string[] {
  const out: string[] = [];
  const m = normalizeNumberInput(v.mass);
  if (!/^[0-9]+(\.[0-9]{1,2})?$/.test(m)) out.push("Nhập tổng sản lượng (tấn) không âm, tối đa 2 chữ số thập phân.");
  if (!/^\d{4}-\d{2}-\d{2}$/.test(v.date)) out.push("Chọn ngày thu hoạch cuối.");
  else if (v.date < anchor || v.date > today) out.push(`Ngày thu hoạch từ mốc gốc (${anchor}) đến hôm nay.`);
  return out;
}

export const isZeroTonnes = (raw: string) => /^0+(\.0+)?$/.test(normalizeNumberInput(raw));

/**
 * AH-100 (FN-06.1): khai báo tổng sản lượng (tấn) và ngày thu hoạch cuối của vụ đang canh tác. Lưu thì vụ "Đã thu
 * hoạch", diện tích được giải phóng, việc Thu hoạch tự Xong. Sản lượng bằng 0 phải xác nhận lại (KH-19). Hủy thì không
 * lưu. Một Idempotency-Key cho mỗi nội dung gửi; GPS lấy một lần khi mở form (tối đa 2 giây) và giữ khi thử lại.
 */
export function HarvestDeclarationForm({ coopRef, season, onDeclared }: { coopRef: string; season: Season; onDeclared: (r: S["HouseholdHarvestResult"]) => void }) {
  const { api } = useAuth();
  const [open, setOpen] = useState(false);
  const [mass, setMass] = useState("");
  const [date, setDate] = useState(vnToday());
  const [confirmZero, setConfirmZero] = useState(false);
  const [errors, setErrors] = useState<string[]>([]);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const gps = useRef<Promise<S["CropGPS"]> | null>(null);
  const key = useRef<{ body: string; key: string } | null>(null);
  const reset = () => { setOpen(false); setMass(""); setDate(vnToday()); setConfirmZero(false); setErrors([]); setProblem(null); key.current = null; gps.current = null; };
  if (!open) return <button type="button" className="btn btn-p btn-sm" onClick={() => { gps.current = captureGPS(); setOpen(true); }}>Khai báo sản lượng</button>;
  const zero = isZeroTonnes(mass);
  const submit = async (e: FormEvent) => {
    e.preventDefault(); if (busy) return;
    const errs = harvestErrors({ mass, date }, season.anchor_date, vnToday());
    setErrors(errs); if (errs.length) return;
    if (zero && !confirmZero) { setConfirmZero(true); return; }
    const location = await (gps.current ?? captureGPS());
    const body = { expected_season_event_id: season.current_event_id, mass_t: normalizeNumberInput(mass), harvest_date: date, zero_confirmed: zero, gps: location, channel: "web" as const };
    const fingerprint = JSON.stringify({ ...body, gps: undefined });
    if (key.current?.body !== fingerprint) key.current = { body: fingerprint, key: newIdempotencyKey() };
    setBusy(true); setProblem(null);
    try {
      const r = await unwrap(api.POST("/v1/cooperatives/{ref}/households/{household_ref}/seasons/{season_ref}/harvest", {
        params: { path: { ref: coopRef, household_ref: season.household_ref, season_ref: season.season_ref }, header: { "Idempotency-Key": key.current.key } }, body,
      }));
      reset(); onDeclared(r);
    } catch (err) { setProblem(toApiProblem(err)); } finally { setBusy(false); }
  };
  return <form className="card stack" aria-label="Khai báo sản lượng" onSubmit={(e) => void submit(e)}>
    <strong>Khai báo sản lượng vụ {season.season_code} {season.season_year}</strong>
    <p className="small mute">Tổng sản lượng cả vụ trên {season.area_ha} ha. Lưu xong vụ chuyển &quot;Đã thu hoạch&quot;, diện tích được giải phóng, việc Thu hoạch tự Xong theo ngày thu hoạch.</p>
    <label className="field">Tổng sản lượng (tấn)<input className="input" inputMode="decimal" value={mass} disabled={busy} onChange={(e) => { setMass(e.target.value); setConfirmZero(false); }} /></label>
    <label className="field">Ngày thu hoạch cuối<input className="input" type="date" min={season.anchor_date} max={vnToday()} value={date} disabled={busy} onChange={(e) => setDate(e.target.value)} /></label>
    {errors.map((m) => <p key={m} role="alert">{m}</p>)}
    {zero && confirmZero ? <p role="status">Sản lượng bằng 0. Bấm &quot;Xác nhận lưu&quot; nếu đúng là vụ không có thu hoạch.</p> : null}
    <div className="row">
      <button className="btn btn-p btn-sm" type="submit" disabled={busy}>{busy ? "Đang lưu…" : zero && confirmZero ? "Xác nhận lưu" : "Lưu"}</button>
      <button className="btn btn-sm" type="button" disabled={busy} onClick={reset}>Hủy</button>
    </div>
    {problem ? <ProblemAlert problem={problem} /> : null}
  </form>;
}
