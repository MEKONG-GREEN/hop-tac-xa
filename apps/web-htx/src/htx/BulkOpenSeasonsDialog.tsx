"use client";

import { useState, type FormEvent } from "react";
import { unwrap } from "@/api/client";
import type { components } from "@/api/generated/application";
import { newIdempotencyKey } from "@/api/idempotency";
import type { ApiProblem } from "@/api/problem";
import type { HouseholdListItem } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { Dialog } from "@/components/Dialog";
import { ProblemAlert } from "@/components/ProblemAlert";
import { formatDecimal } from "@/lib/decimal";
import { toApiProblem } from "@/lib/useAsync";
import { householdAreaError, requestGPS, useSeasonMetadata, useSopChoices } from "./seasonOptions";

type GPS = components["schemas"]["CropGPS"];

interface Row {
  household: HouseholdListItem;
  area: string;
  /** Một Idempotency-Key cho mỗi hộ trong lượt; đổi khi sửa cấu hình chung hoặc diện tích của hộ. */
  key: string;
  result?: { ok: true } | { ok: false; reason: string };
}

const label = (h: HouseholdListItem) => (h.label_unavailable ? "Hộ (không hiển thị được nhãn)" : h.label || "Nông hộ");

/** Lý do bỏ qua một hộ, lấy từ máy chủ (AH-68 AC3). */
export function skipReason(p: ApiProblem): string {
  const remaining = p.problem.extensions?.remaining_area_ha;
  if (p.code === "cropcycle.household_area_exceeded" && typeof remaining === "string") {
    return `Vượt diện tích còn lại (còn ${formatDecimal(remaining, 2)} ha).`;
  }
  const field = p.fieldErrors[0];
  if (p.code === "validation_failed" && field?.reason) return field.reason;
  return p.message;
}

/**
 * AH-68 (FN-03.4, UX-09): mở vụ đồng loạt trên Web cho các hộ đã chọn với cấu hình chung (loại cây/giống, mùa vụ,
 * năm, SOP, mốc gốc). Diện tích mỗi hộ mặc định bằng diện tích còn lại, sửa được. Mỗi hộ là một yêu cầu mở vụ đơn
 * (AH-61) với Idempotency-Key riêng, gửi lần lượt nên máy chủ cộng dồn diện tích khi kiểm; một hộ lỗi không làm hỏng
 * các hộ khác — hộ bị từ chối được liệt kê kèm lý do và gửi lại được.
 */
export function BulkOpenSeasonsDialog({ coopRef, households, onClose, onDone }: {
  coopRef: string; households: HouseholdListItem[]; onClose: () => void; onDone: () => void;
}) {
  const { api } = useAuth();
  const [rows, setRows] = useState<Row[]>(() => households.map((h) => ({ household: h, area: h.remaining_area_ha ?? "", key: newIdempotencyKey() })));
  const [crop, setCrop] = useState("");
  const [variety, setVariety] = useState("");
  const [seasonCode, setSeasonCode] = useState("");
  const [year, setYear] = useState(String(new Date().getFullYear()));
  const [anchor, setAnchor] = useState("");
  const [version, setVersion] = useState("");
  const [busy, setBusy] = useState(false);
  const [progress, setProgress] = useState<string | null>(null);
  const [inputError, setInputError] = useState<string | null>(null);
  const [gps, setGPS] = useState<GPS | null>(null);
  const metadata = useSeasonMetadata(api);
  const choices = useSopChoices(api, metadata.data, coopRef, crop, variety);
  const selected = version || (choices.data?.length === 1 ? choices.data[0]?.id ?? "" : "");
  const pending = rows.filter((r) => !r.result?.ok);
  const opened = rows.length - pending.length;

  // Sửa cấu hình chung: mọi hộ chưa mở đổi khóa (thao tác mới).
  const editCommon = () => { setRows((rs) => rs.map((r) => (r.result?.ok ? r : { ...r, key: newIdempotencyKey(), result: undefined }))); setInputError(null); };
  const editRow = (ref: string, patch: Partial<Row>) =>
    setRows((rs) => rs.map((r) => (r.household.household_ref === ref ? { ...r, ...patch, key: newIdempotencyKey(), result: undefined } : r)));

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (busy) return;
    if (!crop || !variety || !seasonCode || !/^\d{4}$/.test(year) || !/^\d{4}-\d{2}-\d{2}$/.test(anchor)) { setInputError("Chọn loại cây, giống, mùa vụ, năm và mốc gốc."); return; }
    if (!selected) { setInputError("Chọn SOP đang áp dụng. Nếu chưa có, vui lòng liên hệ Admin."); return; }
    if (!pending.length) return;
    if (typeof navigator !== "undefined" && navigator.onLine === false) { setInputError("Mở vụ cần mạng. Dữ liệu form được giữ lại để thử lại."); return; }
    setBusy(true); setInputError(null);
    let any = false;
    try {
      const point = gps ?? await requestGPS();
      setGPS(point);
      // Lần lượt từng hộ: máy chủ kiểm diện tích dưới khóa của hộ nên các vụ trong một lượt được cộng dồn.
      for (const [i, row] of pending.entries()) {
        setProgress(`Đang mở vụ ${i + 1}/${pending.length}…`);
        const invalid = householdAreaError(row.area);
        let result: Row["result"];
        if (invalid) {
          result = { ok: false, reason: invalid };
        } else {
          try {
            await unwrap(api.POST("/v1/cooperatives/{ref}/households/{household_ref}/seasons", {
              params: { path: { ref: coopRef, household_ref: row.household.household_ref }, header: { "Idempotency-Key": row.key } },
              body: { crop_code: crop, season_code: seasonCode, season_year: Number(year), variety_code: variety, area_ha: row.area, anchor_date: anchor, sop_version_id: selected, gps: point, channel: "web" },
            }));
            result = { ok: true };
            any = true;
          } catch (error) {
            result = { ok: false, reason: skipReason(toApiProblem(error)) };
          }
        }
        setRows((rs) => rs.map((r) => (r.household.household_ref === row.household.household_ref ? { ...r, result } : r)));
      }
    } finally {
      setBusy(false); setProgress(null);
      if (any) onDone();
    }
  };

  const failed = rows.filter((r) => r.result && !r.result.ok);
  const metaProblem: ApiProblem | null = metadata.error ?? choices.error ?? null;
  return <Dialog title={`Mở vụ đồng loạt (${rows.length} hộ)`} onClose={() => { if (!busy) onClose(); }}>
    <p>Cấu hình chung áp cho mọi hộ đã chọn. Diện tích mặc định bằng diện tích còn lại của hộ, sửa được. Máy chủ kiểm lại từng hộ; hộ không hợp lệ bị bỏ qua và liệt kê kèm lý do.</p>
    <form onSubmit={(e) => void submit(e)} className="stack">
      <fieldset disabled={busy} className="stack" style={{ border: 0, padding: 0, margin: 0, minWidth: 0 }}>
        <label className="field">Loại cây<select className="select" required value={crop} onChange={(e) => { setCrop(e.target.value); setVariety(""); setVersion(""); editCommon(); }}><option value="">Chọn loại cây</option>{metadata.data?.crops.filter((c) => !c.attrs.parent_code).map((c) => <option key={c.code} value={c.code}>{c.name}</option>)}</select></label>
        <label className="field">Giống<select className="select" required value={variety} onChange={(e) => { setVariety(e.target.value); setVersion(""); editCommon(); }}><option value="">Chọn giống</option>{metadata.data?.varieties.filter((v) => v.attrs.crop_code === crop).map((v) => <option key={v.code} value={v.code}>{v.name}</option>)}</select></label>
        <label className="field">Mùa vụ<select className="select" required value={seasonCode} onChange={(e) => { setSeasonCode(e.target.value); editCommon(); }}><option value="">Chọn mùa vụ</option>{metadata.data?.seasons.map((s) => <option key={s.code} value={s.code}>{s.name}</option>)}</select></label>
        <label className="field">Năm<input className="input" required type="number" min={2000} max={2100} value={year} onChange={(e) => { setYear(e.target.value); editCommon(); }} /></label>
        <label className="field">SOP đang áp dụng<select className="select" required value={selected} onChange={(e) => { setVersion(e.target.value); editCommon(); }}><option value="">Chọn SOP</option>{choices.data?.map((v) => <option key={v.id} value={v.id}>{v.label}</option>)}</select></label>
        <label className="field">{choices.data?.find((v) => v.id === selected)?.anchor || "Mốc gốc"}<input className="input" required type="date" value={anchor} onChange={(e) => { setAnchor(e.target.value); editCommon(); }} /></label>
        {!choices.loading && crop && variety && choices.data?.length === 0 ? <p role="status">Chưa có SOP đang áp dụng phù hợp. Vui lòng liên hệ Admin.</p> : null}
        <div className="tbl-wrap">
          <table className="tbl">
            <caption className="sr-only">Hộ được mở vụ</caption>
            <thead><tr><th scope="col">Nông hộ</th><th scope="col" className="num">Còn lại (ha)</th><th scope="col">Diện tích vụ (ha)</th><th scope="col">Kết quả</th><th scope="col"><span className="sr-only">Bỏ</span></th></tr></thead>
            <tbody>{rows.map((r) => <tr key={r.household.household_ref}>
              <td>{label(r.household)}</td>
              <td className="num">{formatDecimal(r.household.remaining_area_ha, 2)}</td>
              <td><input className="input" inputMode="decimal" aria-label={`Diện tích vụ của ${label(r.household)}`} value={r.area} disabled={r.result?.ok} onChange={(e) => editRow(r.household.household_ref, { area: e.target.value })} /></td>
              <td>{r.result?.ok ? <span className="chip c-ok">Đã mở vụ</span> : r.result ? <span className="chip c-warn">Bỏ qua: {r.result.reason}</span> : null}</td>
              <td>{!r.result?.ok && rows.length > 1 ? <button type="button" className="btn btn-o btn-sm" onClick={() => setRows((rs) => rs.filter((x) => x !== r))}>Bỏ hộ</button> : null}</td>
            </tr>)}</tbody>
          </table>
        </div>
        <p className="small">Khi lưu, trình duyệt hỏi quyền vị trí một lần cho cả lượt; từ chối vẫn mở vụ được và ghi “Thiếu GPS”.</p>
        {pending.length ? <button className="btn btn-p" type="submit" disabled={metadata.loading || choices.loading}>{busy ? progress ?? "Đang lưu…" : `Mở vụ đồng loạt (${pending.length} hộ)`}</button> : null}
      </fieldset>
      {inputError ? <p role="alert">{inputError}</p> : null}
      {metaProblem ? <ProblemAlert problem={metaProblem} onRetry={() => { metadata.reload(); choices.reload(); }} /> : null}
      {opened || failed.length ? <p role="status">Đã mở {opened}/{rows.length} vụ.{failed.length ? ` ${failed.length} hộ bị bỏ qua — sửa diện tích hoặc bỏ hộ rồi gửi lại.` : ""}</p> : null}
    </form>
  </Dialog>;
}
