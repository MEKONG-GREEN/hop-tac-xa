"use client";

import { useRef, useState, type FormEvent } from "react";
import { unwrap } from "@/api/client";
import type { components } from "@/api/generated/application";
import { newIdempotencyKey } from "@/api/idempotency";
import type { ApiProblem } from "@/api/problem";
import { useAuth } from "@/auth/AuthProvider";
import { ProblemAlert } from "@/components/ProblemAlert";
import { toApiProblem } from "@/lib/useAsync";
import { householdAreaError, useSeasonMetadata, useSopChoices } from "./seasonOptions";

type Season = components["schemas"]["HouseholdSeason"];

/** Kiểm sớm phía client; máy chủ kiểm lại (AH-175 AC3, AC4). */
export function editSeasonErrors(season: Pick<Season, "anchor_date" | "area_ha" | "variety_code">, v: { anchor: string; area: string; variety: string; sop: string; reason: string }): string[] {
  const out: string[] = [];
  const r = v.reason.trim();
  if (r.length < 10 || r.length > 500) out.push("Nhập lý do sửa vụ (10–500 ký tự).");
  if (v.area !== season.area_ha && householdAreaError(v.area)) out.push(householdAreaError(v.area)!);
  if (!/^\d{4}-\d{2}-\d{2}$/.test(v.anchor)) out.push("Chọn mốc gốc hợp lệ.");
  if (v.variety !== season.variety_code && !v.sop) out.push("Đổi giống phải chọn SOP đang áp dụng theo giống mới.");
  if (v.anchor === season.anchor_date && v.area === season.area_ha && v.variety === season.variety_code && !v.sop) out.push("Chưa có nội dung thay đổi.");
  return out;
}

/**
 * AH-175 (FN-03.9): sửa mốc gốc, diện tích, giống của vụ đang canh tác kèm lý do. Đổi giống chỉ khi vụ chưa có nhật ký
 * (máy chủ từ chối nếu đã có — KH-10) và phải chọn SOP theo giống mới. Một Idempotency-Key cho mỗi nội dung gửi.
 */
export function EditSeasonForm({ coopRef, season, onSaved }: { coopRef: string; season: Season; onSaved: (s: Season) => void }) {
  const { api } = useAuth();
  const [open, setOpen] = useState(false);
  const [anchor, setAnchor] = useState(season.anchor_date);
  const [area, setArea] = useState(season.area_ha);
  const [variety, setVariety] = useState(season.variety_code);
  const [sop, setSop] = useState("");
  const [reason, setReason] = useState("");
  const [errors, setErrors] = useState<string[]>([]);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const key = useRef<{ body: string; key: string } | null>(null);
  const metadata = useSeasonMetadata(api);
  const choices = useSopChoices(api, open ? metadata.data : null, coopRef, season.crop_code, variety);
  if (!open) return <button type="button" className="btn btn-o btn-sm" onClick={() => setOpen(true)}>Sửa vụ</button>;
  const submit = async (e: FormEvent) => {
    e.preventDefault(); if (busy) return;
    const found = editSeasonErrors(season, { anchor, area, variety, sop, reason });
    setErrors(found); if (found.length) return;
    const body = { expected_season_event_id: season.current_event_id, reason: reason.trim(), channel: "web" as const,
      ...(anchor !== season.anchor_date ? { anchor_date: anchor } : {}),
      ...(area !== season.area_ha ? { area_ha: area } : {}),
      ...(variety !== season.variety_code ? { variety_code: variety } : {}),
      ...(sop ? { sop_version_id: sop } : {}) };
    const fingerprint = JSON.stringify(body);
    if (key.current?.body !== fingerprint) key.current = { body: fingerprint, key: newIdempotencyKey() };
    setBusy(true); setProblem(null);
    try {
      const saved = await unwrap(api.POST("/v1/cooperatives/{ref}/households/{household_ref}/seasons/{season_ref}/corrections", {
        params: { path: { ref: coopRef, household_ref: season.household_ref, season_ref: season.season_ref }, header: { "Idempotency-Key": key.current.key } }, body,
      }));
      setOpen(false); setReason(""); setSop("");
      onSaved(saved);
    } catch (err) { setProblem(toApiProblem(err)); } finally { setBusy(false); }
  };
  return <form className="card stack" aria-label="Sửa vụ" onSubmit={(e) => void submit(e)}>
    <strong>Sửa vụ {season.season_code} {season.season_year}</strong>
    <p className="small mute">Việc đã xong giữ ngày làm; việc chưa xong tính lại theo mốc gốc mới. Đổi giống chỉ khi vụ chưa có nhật ký.</p>
    <fieldset disabled={busy} className="stack" style={{ border: 0, padding: 0, margin: 0 }}>
      <label className="field">{season.sop_snapshot.anchor_name || "Mốc gốc"}<input className="input" type="date" value={anchor} onChange={(e) => setAnchor(e.target.value)} /></label>
      <label className="field">Diện tích vụ (ha)<input className="input" inputMode="decimal" value={area} onChange={(e) => setArea(e.target.value)} /></label>
      <label className="field">Giống<select className="select" value={variety} onChange={(e) => { setVariety(e.target.value); setSop(""); }}>
        {metadata.data ? metadata.data.varieties.filter((v) => v.attrs.crop_code === season.crop_code).map((v) => <option key={v.code} value={v.code}>{v.name}</option>) : <option value={season.variety_code}>{season.variety_code}</option>}
      </select></label>
      {variety !== season.variety_code ? <label className="field">SOP theo giống mới<select className="select" value={sop} onChange={(e) => setSop(e.target.value)}>
        <option value="">Chọn SOP</option>{choices.data?.map((v) => <option key={v.id} value={v.id}>{v.label}</option>)}
      </select></label> : null}
      <label className="field">Lý do sửa<textarea className="input" maxLength={500} value={reason} onChange={(e) => setReason(e.target.value)} /></label>
      <div className="row">
        <button className="btn btn-p btn-sm" type="submit">{busy ? "Đang lưu…" : "Lưu thay đổi"}</button>
        <button className="btn btn-sm" type="button" onClick={() => { setOpen(false); setErrors([]); setProblem(null); }}>Hủy</button>
      </div>
    </fieldset>
    {errors.length ? <ul role="alert">{errors.map((x) => <li key={x}>{x}</li>)}</ul> : null}
    {problem ? <ProblemAlert problem={problem} /> : null}
  </form>;
}
