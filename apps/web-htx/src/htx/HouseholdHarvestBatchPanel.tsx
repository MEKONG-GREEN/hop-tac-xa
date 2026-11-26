"use client";

import { useRef, useState, type ChangeEvent, type FormEvent } from "react";
import { unwrap } from "@/api/client";
import type { components } from "@/api/generated/application";
import { newIdempotencyKey } from "@/api/idempotency";
import { usePagedList } from "@/api/pagination";
import type { ApiProblem } from "@/api/problem";
import { useAuth } from "@/auth/AuthProvider";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Empty, LoadMore, Loading } from "@/components/ui";
import { formatDateVi, formatDecimalVi } from "@/lib/exactDecimal";
import { readUpload, saveBlob, type UploadFile } from "@/lib/files";
import { toApiProblem } from "@/lib/useAsync";
import { toCsv } from "./rules";

type BatchResult = components["responses"]["HouseholdHarvestBatch"]["content"]["application/json"];
type Draft = { mass: string; date: string; zero: boolean };

const vnToday = () => new Intl.DateTimeFormat("en-CA", { timeZone: "Asia/Ho_Chi_Minh" }).format(new Date());

/** Chuẩn hóa số tấn nhập tay ("9,75" → "9.75"); trả null nếu sai định dạng (âm, quá 2 chữ số lẻ, chữ). */
export function normalizeTonnes(raw: string): string | null {
  const v = raw.trim().replace(/\s/g, "").replace(",", ".");
  return /^\d+(\.\d{1,2})?$/.test(v) ? v : null;
}

/** Kiểm sớm một dòng (máy chủ kiểm lại): tấn không âm tối đa 2 chữ số lẻ, có ngày, ngày không sau hôm nay. */
export function draftError(d: Draft, today: string = vnToday()): string | null {
  if (!d.mass.trim()) return "Nhập sản lượng (tấn).";
  const t = normalizeTonnes(d.mass);
  if (t === null) return "Sản lượng là số tấn không âm, tối đa 2 chữ số lẻ.";
  if (!d.date) return "Nhập ngày thu hoạch cuối.";
  if (d.date > today) return "Ngày thu hoạch không được sau hôm nay.";
  if (/^0+(\.0+)?$/.test(t) && !d.zero) return "Sản lượng bằng 0: đánh dấu xác nhận.";
  return null;
}

/** CSV danh sách dòng lỗi để tải về (AC3). */
export function errorCsv(res: BatchResult, label: (ref: string | null) => string): string {
  return toCsv([["Dòng", "Mã vụ", "Vụ", "Lý do"], ...res.rows.filter((r) => r.status === "rejected").map((r) => [r.line, r.season_ref ?? "", label(r.season_ref), r.reason ?? ""])]);
}

/**
 * AH-177 (FN-06.2, EX-03, UX-11): Thành viên HTX, Admin khai báo sản lượng nhiều vụ nông hộ — bảng nhập nhanh "Lưu tất
 * cả" (mặc định vụ đã ghi việc Thu hoạch, công tắc hiện mọi vụ đang canh tác — KH-20) hoặc tệp Excel mẫu sinh sẵn.
 * Dòng lỗi giữ số đã nhập, tô màu kèm lý do; dòng hợp lệ vẫn lưu. Một Idempotency-Key cho mỗi lần gửi (giữ khi thử lại
 * cùng nội dung, đổi khi nội dung đổi).
 */
export function HouseholdHarvestBatchPanel({ coopRef, householdLabel, onSaved }: { coopRef: string; householdLabel: (ref: string) => string; onSaved?: () => void }) {
  const { api } = useAuth();
  const [all, setAll] = useState(false);
  const [version, setVersion] = useState(0);
  const [drafts, setDrafts] = useState<Record<string, Draft>>({});
  const [rowErrors, setRowErrors] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [saved, setSaved] = useState<BatchResult | null>(null);
  const batchKey = useRef<{ body: string; key: string } | null>(null);
  const [file, setFile] = useState<UploadFile | null>(null);
  const [fileKey, setFileKey] = useState(newIdempotencyKey);
  const [imported, setImported] = useState<BatchResult | null>(null);
  const list = usePagedList(
    (cursor) => unwrap(api.GET("/v1/cooperatives/{ref}/household-yields", {
      params: { path: { ref: coopRef }, query: { state: "pending", harvest_recorded_only: !all, cursor, limit: 50 } },
    })),
    [api, coopRef, all, version],
  );
  const seasonLabel = (ref: string | null) => {
    const s = list.items.find((x) => x.season_ref === ref);
    return s ? `${householdLabel(s.household_ref)} · ${s.season_code} ${s.season_year}` : ref ? `Vụ ${ref.slice(-4)}` : "—";
  };
  const draftOf = (ref: string): Draft => drafts[ref] ?? { mass: "", date: "", zero: false };
  const setDraft = (ref: string, patch: Partial<Draft>) => {
    setDrafts((d) => ({ ...d, [ref]: { ...draftOf(ref), ...patch } }));
    setRowErrors((e) => {
      const next = { ...e };
      delete next[ref];
      return next;
    });
  };
  const saveAll = async (e: FormEvent) => {
    e.preventDefault();
    if (busy) return;
    const filled = list.items.filter((s) => draftOf(s.season_ref).mass.trim() || draftOf(s.season_ref).date);
    if (!filled.length) {
      setProblem(null);
      setRowErrors({});
      return;
    }
    const errs: Record<string, string> = {};
    for (const s of filled) {
      const m = draftError(draftOf(s.season_ref));
      if (m) errs[s.season_ref] = m;
    }
    setRowErrors(errs);
    if (Object.keys(errs).length) return;
    const rows = filled.map((s, i) => {
      const d = draftOf(s.season_ref);
      return { line: i + 1, season_ref: s.season_ref, expected_season_event_id: s.current_event_id, mass_t: normalizeTonnes(d.mass)!, harvest_date: d.date, zero_confirmed: d.zero };
    });
    const fingerprint = JSON.stringify(rows);
    if (batchKey.current?.body !== fingerprint) batchKey.current = { body: fingerprint, key: newIdempotencyKey() };
    setBusy(true);
    setProblem(null);
    try {
      const res = await unwrap(api.POST("/v1/cooperatives/{ref}/harvest-batches", {
        params: { path: { ref: coopRef }, header: { "Idempotency-Key": batchKey.current.key } }, body: { rows },
      }));
      const nextErrors: Record<string, string> = {};
      const done = new Set<string>();
      for (const r of res.rows) {
        if (!r.season_ref) continue;
        if (r.status === "rejected") nextErrors[r.season_ref] = r.reason ?? "Dòng bị bỏ qua";
        else done.add(r.season_ref);
      }
      setRowErrors(nextErrors);
      setDrafts((d) => Object.fromEntries(Object.entries(d).filter(([ref]) => !done.has(ref))));
      setSaved(res);
      batchKey.current = null;
      if (res.accepted) {
        setVersion((v) => v + 1);
        onSaved?.();
      }
    } catch (err) {
      setProblem(toApiProblem(err));
    } finally {
      setBusy(false);
    }
  };
  const downloadTemplate = async () => {
    setProblem(null);
    try {
      const res = await api.GET("/v1/cooperatives/{ref}/harvest-template", { params: { path: { ref: coopRef }, query: { scope: all ? "all" : "harvest_recorded" } }, parseAs: "blob" });
      if (!res.response.ok || !res.data) throw res.error ?? new Error("Không tải được tệp mẫu");
      const blob = res.data as Blob;
      saveBlob(blob, blob.type.includes("csv") ? "mau-khai-bao-san-luong.csv" : "mau-khai-bao-san-luong.xlsx");
    } catch (err) {
      setProblem(toApiProblem(err));
    }
  };
  const onFile = async (e: ChangeEvent<HTMLInputElement>) => {
    const f = e.target.files?.[0];
    setImported(null);
    setFile(f ? await readUpload(f) : null);
    setFileKey(newIdempotencyKey());
  };
  const fileOk = !!file && (file.kind === "xlsx" || file.kind === "csv");
  const importFile = async () => {
    if (!file || !fileOk || busy) return;
    setBusy(true);
    setProblem(null);
    try {
      const res = await unwrap(api.POST("/v1/cooperatives/{ref}/harvest-imports", {
        params: { path: { ref: coopRef }, header: { "Idempotency-Key": fileKey } },
        body: file.body as unknown as string,
        bodySerializer: (b: unknown) => b as BodyInit,
        headers: { "Content-Type": file.contentType },
      }));
      setImported(res);
      if (res.accepted) {
        setVersion((v) => v + 1);
        onSaved?.();
      }
    } catch (err) {
      setProblem(toApiProblem(err));
    } finally {
      setBusy(false);
    }
  };
  const today = vnToday();
  return <section className="card sec stack" aria-label="Khai báo sản lượng nhiều vụ">
    <h3>Khai báo sản lượng nhiều vụ (vụ nông hộ)</h3>
    <p className="small mute">Mỗi dòng áp quy tắc khai báo một vụ. Dòng lỗi được bỏ qua kèm lý do, dòng hợp lệ vẫn lưu. Sửa sản lượng đã khai báo làm trong chi tiết vụ.</p>
    <label className="row" style={{ gap: 6, alignItems: "center" }}>
      <input type="checkbox" checked={all} onChange={(e) => setAll(e.target.checked)} /> Hiện mọi vụ đang canh tác (mặc định chỉ vụ đã ghi việc Thu hoạch)
    </label>
    {problem ? <ProblemAlert problem={problem} /> : null}
    <form className="stack" aria-label="Bảng nhập nhanh" onSubmit={(e) => void saveAll(e)}>
      {list.error ? <ProblemAlert problem={list.error} onRetry={list.reload} /> : null}
      {list.loading && !list.items.length ? <Loading /> : null}
      {!list.loading && !list.items.length && !list.error ? <Empty>{all ? "Không có vụ đang canh tác." : "Chưa có vụ đã ghi việc Thu hoạch chờ khai báo."}</Empty> : null}
      {list.items.length ? <table className="tbl">
        <thead><tr><th>Nông hộ</th><th>Vụ</th><th>Diện tích</th><th>Ngày thu hoạch dự kiến</th><th>Sản lượng (tấn)</th><th>Ngày thu hoạch cuối</th><th>Xác nhận 0</th><th>Kết quả</th></tr></thead>
        <tbody>{list.items.map((s) => {
          const d = draftOf(s.season_ref), err = rowErrors[s.season_ref];
          const zero = normalizeTonnes(d.mass) !== null && /^0+(\.0+)?$/.test(normalizeTonnes(d.mass)!);
          return <tr key={s.season_ref} className={err ? "row-err" : undefined} style={err ? { background: "var(--c-warn-bg, #fff4e5)" } : undefined}>
            <td>{householdLabel(s.household_ref)}</td>
            <td>{s.season_code} {s.season_year} · {s.crop_code}{s.variety_code ? ` · ${s.variety_code}` : ""}</td>
            <td>{formatDecimalVi(s.area_ha, { minDecimals: 2, maxDecimals: 2 })} ha</td>
            <td>{s.expected_harvest_date ? formatDateVi(s.expected_harvest_date) : "—"}</td>
            <td><input className="input" inputMode="decimal" aria-label={`Sản lượng tấn ${householdLabel(s.household_ref)} ${s.season_code} ${s.season_year}`} value={d.mass} disabled={busy}
              onChange={(e) => setDraft(s.season_ref, { mass: e.target.value, zero: false })} /></td>
            <td><input className="input" type="date" max={today} aria-label={`Ngày thu hoạch cuối ${householdLabel(s.household_ref)} ${s.season_code} ${s.season_year}`} value={d.date} disabled={busy}
              onChange={(e) => setDraft(s.season_ref, { date: e.target.value })} /></td>
            <td>{zero ? <input type="checkbox" aria-label={`Xác nhận 0 ${householdLabel(s.household_ref)} ${s.season_code} ${s.season_year}`} checked={d.zero} disabled={busy}
              onChange={(e) => setDraft(s.season_ref, { zero: e.target.checked })} /> : null}</td>
            <td>{err ? <span role="alert" className="err small">{err}</span> : null}</td>
          </tr>;
        })}</tbody>
      </table> : null}
      <LoadMore hasMore={list.hasMore} loading={list.loading} onClick={list.loadMore} />
      {list.items.length ? <div className="row"><button type="submit" className="btn btn-p btn-sm" disabled={busy}>{busy ? "Đang lưu…" : "Lưu tất cả"}</button></div> : null}
      {saved ? <p role="status">{saved.accepted} vụ đã lưu · {saved.rejected} dòng bị bỏ qua{saved.duplicate ? ` · ${saved.duplicate} dòng đã lưu trước đó` : ""}</p> : null}
    </form>
    <div className="panel stack" aria-label="Nhập từ Excel" style={{ gap: 8 }}>
      <b>Nhập từ Excel (.xlsx) — tối đa 200 dòng</b>
      <span className="small mute">Tải tệp mẫu (sinh sẵn mã vụ theo công tắc ở trên), điền Sản lượng (tấn), Ngày thu hoạch cuối (dd/mm/yyyy), Xác nhận 0 (x) rồi nhập lại. Dòng để trống được bỏ qua.</span>
      <div className="row" style={{ alignItems: "flex-end" }}>
        <button type="button" className="btn btn-o btn-sm" onClick={() => void downloadTemplate()}>Tải tệp mẫu</button>
        <input type="file" aria-label="Tệp sản lượng vụ nông hộ" accept=".xlsx,.csv" onChange={(e) => void onFile(e)} />
        <button type="button" className="btn btn-p btn-sm" disabled={!fileOk || busy} onClick={() => void importFile()}>{busy ? "Đang nhập…" : "Nhập tệp"}</button>
      </div>
      {file && !fileOk ? <span className="err small">Chỉ nhận .xlsx hoặc CSV UTF-8 (không nhận .xls cũ).</span> : null}
      {imported ? <>
        <p role="status">{imported.accepted} dòng đã lưu · {imported.rejected} dòng lỗi{imported.duplicate ? ` · ${imported.duplicate} dòng đã lưu trước đó` : ""}</p>
        {imported.rejected ? <>
          <ul aria-label="Dòng lỗi">{imported.rows.filter((r) => r.status === "rejected").map((r) => <li key={r.line} className="err small">Dòng {r.line}: {r.reason}</li>)}</ul>
          <button type="button" className="btn btn-o btn-sm" onClick={() => saveBlob(new Blob([errorCsv(imported, seasonLabel)], { type: "text/csv;charset=utf-8" }), "loi-nhap-san-luong.csv")}>Tải danh sách lỗi</button>
        </> : null}
      </> : null}
    </div>
  </section>;
}
