"use client";

import { useRef, useState, type ChangeEvent } from "react";
import { unwrap } from "@/api/client";
import type { components } from "@/api/generated/admin";
import { newIdempotencyKey } from "@/api/idempotency";
import type { ApiProblem } from "@/api/problem";
import { parseProblemResponse } from "@/api/problem";
import { useAuth } from "@/auth/AuthProvider";
import { Dialog } from "@/components/Dialog";
import { ProblemAlert } from "@/components/ProblemAlert";
import { filenameFromDisposition, readUpload, saveBlob, type UploadFile } from "@/lib/files";
import { toApiProblem } from "@/lib/useAsync";

type ImportResult = components["schemas"]["CatalogImportResult"];

export function AdminUnitImport({ onClose, onImported }: { onClose: () => void; onImported: () => void }) {
  const { adminApi } = useAuth();
  const [file, setFile] = useState<UploadFile | null>(null);
  const [key, setKey] = useState(newIdempotencyKey);
  const [busy, setBusy] = useState(false); const [reading, setReading] = useState(false);
  const [error, setError] = useState<ApiProblem | null>(null); const [result, setResult] = useState<ImportResult | null>(null);
  const [validation, setValidation] = useState<string | null>(null);
  const selection = useRef(0);
  async function choose(e: ChangeEvent<HTMLInputElement>) {
    const current = ++selection.current; const selected = e.target.files?.[0];
    setFile(null); setError(null); setValidation(null); setResult(null); setKey(newIdempotencyKey()); setReading(!!selected);
    try {
      if (selected) {
        if (selected.size > 2 * 1024 * 1024) { setValidation("Tệp tối đa 2 MB."); return; }
        const upload = await readUpload(selected);
        if (current !== selection.current) return;
        if (upload.kind !== "xlsx" && upload.kind !== "csv") { setValidation("Chọn tệp Excel .xlsx hoặc CSV theo mẫu."); return; }
        setFile(upload);
      }
    } catch { if (current === selection.current) setValidation("Không đọc được tệp. Vui lòng chọn lại."); }
    finally { if (current === selection.current) setReading(false); }
  }
  async function template() {
    try {
      const response = await adminApi.GET("/v1/masterdata/admin-unit-template", { parseAs: "blob" });
      if (!response.response.ok || !response.data) throw await parseProblemResponse(response.response);
      saveBlob(response.data, filenameFromDisposition(response.response.headers.get("Content-Disposition"), "don-vi-hanh-chinh.xlsx"));
    } catch (e) { setError(toApiProblem(e)); }
  }
  async function submit() {
    if (!file || busy || reading) return; setBusy(true); setError(null);
    try {
      const imported = await unwrap(adminApi.POST("/v1/masterdata/admin-unit-imports", { params: { header: { "Idempotency-Key": key } }, headers: { "Content-Type": file.contentType }, body: file.body as unknown as string, bodySerializer: (body: unknown) => body as BodyInit }));
      setResult(imported); setFile(null); setKey(newIdempotencyKey()); onImported();
    } catch (e) { setError(toApiProblem(e)); } finally { setBusy(false); }
  }
  return <Dialog title="Nhập đơn vị hành chính từ Excel" onClose={() => { if (!busy) onClose(); }}>
    <div className="stack">
      <p>Mẫu gồm Tên, Cấp (Tỉnh hoặc Xã), Tỉnh / thành phố trực thuộc. Đặt tỉnh mới trước các xã thuộc tỉnh. Tối đa 500 dòng và 2 MB.</p>
      <button type="button" className="btn btn-o" onClick={() => void template()}>Tải file mẫu</button>
      <label className="field">Chọn tệp<input type="file" accept=".xlsx,.csv" disabled={busy} onChange={(e) => void choose(e)} /></label>
      <button type="button" className="btn btn-p" disabled={!file || busy || reading} onClick={() => void submit()}>{busy ? "Đang nhập…" : "Nhập"}</button>
      {error ? <ProblemAlert problem={error} /> : null}
      {validation ? <p role="alert">{validation}</p> : null}
      {result ? <section aria-label="Kết quả nhập"><p role="status">Đã thêm {result.created} dòng; {result.rejected} dòng lỗi.</p>
        {result.rejected ? <table className="table"><thead><tr><th>Dòng</th><th>Lý do</th></tr></thead><tbody>{result.rows.filter((r) => r.status === "rejected").map((r) => <tr key={r.row}><td>{r.row}</td><td>{r.reason}</td></tr>)}</tbody></table> : null}
      </section> : null}
    </div>
  </Dialog>;
}
