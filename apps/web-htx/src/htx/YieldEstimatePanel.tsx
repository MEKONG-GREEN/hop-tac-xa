"use client";

import { useRef, useState, type FormEvent } from "react";
import { unwrap } from "@/api/client";
import type { components } from "@/api/generated/application";
import { newIdempotencyKey } from "@/api/idempotency";
import type { ApiProblem } from "@/api/problem";
import { useAuth } from "@/auth/AuthProvider";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Decimal, formatDateVi, formatDecimalVi, normalizeNumberInput } from "@/lib/exactDecimal";
import { toApiProblem } from "@/lib/useAsync";

type Season = components["schemas"]["HouseholdSeason"];
type Estimate = components["schemas"]["HouseholdYieldEstimate"];

/** Kiểm sớm phía client (KH-26); máy chủ kiểm lại. Ô trống = dùng giá trị hệ thống tính. */
export function tonnesInputError(raw: string): string | null {
  const v = normalizeNumberInput(raw);
  if (v === "") return null;
  if (!/^[0-9]+(\.[0-9]{1,2})?$/.test(v)) return "Nhập số tấn không âm, tối đa 2 chữ số thập phân.";
  return null;
}

/** AH-100 AC2: chênh lệch thực tế − dự kiến (tấn, decimal, có dấu). */
export function actualVsExpected(actual: string, expected: string): string {
  const a = Decimal.parse(actual), e = Decimal.parse(expected);
  if (!a || !e) return "";
  const d = a.sub(e).round(2);
  return `${d.sign() > 0 ? "+" : ""}${formatDecimalVi(d.toString(), { minDecimals: 2, maxDecimals: 2 })}`;
}

const t = (v: string | null | undefined) => (v == null ? "—" : `${formatDecimalVi(v, { minDecimals: 2, maxDecimals: 2 })} tấn`);

/**
 * AH-87 (FN-04.2, FN-04.3, KH-05): sản lượng lúa, rơm dự kiến và ngày thu hoạch dự kiến của vụ theo hộ. Giá trị hệ thống
 * tính = diện tích × năng suất quy ước; rơm chỉ có ở vụ lúa. Thành viên HTX nhập giá trị của mình khi vụ đang canh tác;
 * Đại diện HTX chỉ xem.
 */
export function YieldEstimatePanel({ coopRef, season, canEdit, onChanged }: {
  coopRef: string; season: Season; canEdit: boolean; onChanged: (e: Estimate) => void;
}) {
  const { api } = useAuth();
  const e = season.yield_estimate;
  const [editing, setEditing] = useState(false);
  const [rice, setRice] = useState("");
  const [straw, setStraw] = useState("");
  const [errors, setErrors] = useState<{ rice?: string; straw?: string }>({});
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const key = useRef<{ body: string; key: string } | null>(null);
  const harvest = season.expected_harvest_date ? formatDateVi(season.expected_harvest_date) : "Chưa có (quy trình không có việc Thu hoạch)";
  const editable = canEdit && season.status === "open" && !!e;
  const start = () => { if (!e) return; setRice(e.entered_rice_t ?? ""); setStraw(e.entered_straw_t ?? ""); setErrors({}); setProblem(null); setEditing(true); };
  const submit = async (ev: FormEvent) => {
    ev.preventDefault(); if (busy || !e) return;
    const next = { rice: tonnesInputError(rice) ?? undefined, straw: e.is_rice ? tonnesInputError(straw) ?? undefined : undefined };
    setErrors(next); if (next.rice || next.straw) return;
    const body = { expected_seq: e.seq, rice_t: normalizeNumberInput(rice) || null, straw_t: e.is_rice ? normalizeNumberInput(straw) || null : null };
    const fingerprint = JSON.stringify(body);
    if (key.current?.body !== fingerprint) key.current = { body: fingerprint, key: newIdempotencyKey() };
    setBusy(true); setProblem(null);
    try {
      const r = await unwrap(api.POST("/v1/cooperatives/{ref}/households/{household_ref}/seasons/{season_ref}/yield-estimates", {
        params: { path: { ref: coopRef, household_ref: season.household_ref, season_ref: season.season_ref }, header: { "Idempotency-Key": key.current.key } }, body,
      }));
      key.current = null; setEditing(false); onChanged(r.yield_estimate);
    } catch (err) { setProblem(toApiProblem(err)); } finally { setBusy(false); }
  };
  return <section className="card stack" aria-label="Sản lượng dự kiến">
    <strong>Sản lượng dự kiến</strong>
    {e ? <dl className="stack">
      <div><dt>Lúa dự kiến</dt><dd>{t(e.rice_t)} <span className="small mute">{e.entered_rice_t ? `(Thành viên nhập; hệ thống tính ${t(e.computed_rice_t)})` : `(hệ thống tính: ${formatDecimalVi(e.area_ha)} ha × ${formatDecimalVi(e.conventional_yield_t_per_ha)} tấn/ha)`}</span></dd></div>
      {e.is_rice ? <div><dt>Rơm dự kiến</dt><dd>{t(e.straw_t)} <span className="small mute">{e.entered_straw_t ? `(Thành viên nhập; hệ thống tính ${t(e.computed_straw_t)})` : `(hệ thống tính, hệ số rơm ${formatDecimalVi(e.straw_pct ?? "0")} %)`}</span></dd></div> : null}
    </dl> : <p className="small mute">Chưa tính được sản lượng dự kiến (không đọc được tham số); thử tải lại sau.</p>}
    <p>Ngày thu hoạch dự kiến: {harvest}</p>
    {season.harvest ? <div role="status">
      <strong>Sản lượng thực tế: {t(season.harvest.mass_t)}</strong> · ngày thu hoạch {formatDateVi(season.harvest.harvest_date)} · năng suất {formatDecimalVi(season.harvest.yield_t_per_ha, { minDecimals: 2, maxDecimals: 2 })} tấn/ha
      {e ? <span> · so với dự kiến {t(e.rice_t)}: {actualVsExpected(season.harvest.mass_t, e.rice_t)} tấn</span> : null}
    </div> : null}
    {editable && !editing ? <button type="button" className="btn btn-o btn-sm" onClick={start}>Sửa sản lượng dự kiến</button> : null}
    {editable && editing ? <form className="stack" aria-label="Sửa sản lượng dự kiến" onSubmit={(ev) => void submit(ev)}>
      <p className="small mute">Để trống để dùng giá trị hệ thống tính.</p>
      <label className="field">Lúa dự kiến (tấn)<input className="input" inputMode="decimal" value={rice} disabled={busy} placeholder={e!.computed_rice_t} onChange={(x) => setRice(x.target.value)} /></label>
      {errors.rice ? <p role="alert">{errors.rice}</p> : null}
      {e!.is_rice ? <label className="field">Rơm dự kiến (tấn)<input className="input" inputMode="decimal" value={straw} disabled={busy} placeholder={e!.computed_straw_t ?? ""} onChange={(x) => setStraw(x.target.value)} /></label> : null}
      {errors.straw ? <p role="alert">{errors.straw}</p> : null}
      <div className="row">
        <button className="btn btn-p btn-sm" type="submit" disabled={busy}>{busy ? "Đang lưu…" : "Lưu"}</button>
        <button className="btn btn-sm" type="button" disabled={busy} onClick={() => { setEditing(false); setProblem(null); key.current = null; }}>Hủy</button>
      </div>
    </form> : null}
    {problem ? <ProblemAlert problem={problem} /> : null}
  </section>;
}
