"use client";

// Hộp thoại "Nhập từ Excel" của màn Quy trình SOP (AH-38, BRD FN-01.4): tải file mẫu do máy chủ sinh, chọn tệp .xlsx,
// nhập. Máy chủ kiểm bằng đúng bộ luật soạn SOP; tệp có lỗi thì không tạo SOP và trả danh sách lỗi theo sheet/dòng/cột
// để người dùng sửa tệp rồi nhập lại. Thành công → SOP Nháp, màn soạn mở SOP vừa tạo (phát hành như SOP soạn tay).
// Một thao tác = MỘT Idempotency-Key: bấm lại sau lỗi mạng gửi lại đúng khóa cũ; đổi tệp hoặc nhập xong thì khóa mới.
import { useState, type ChangeEvent } from "react";
import { unwrap } from "@/api/client";
import { newIdempotencyKey, outcomeUnknown } from "@/api/idempotency";
import { ApiProblem, parseProblemResponse } from "@/api/problem";
import type { CropSOPImportRowError } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { Dialog } from "@/components/Dialog";
import { Icon } from "@/components/Icon";
import { ProblemAlert } from "@/components/ProblemAlert";
import { filenameFromDisposition, readUpload, saveBlob, type UploadFile } from "@/lib/files";
import { toApiProblem } from "@/lib/useAsync";

const ROWS_CODE = "cropcycle.sop_import_rows";

export function SopImportDialog({ onClose, onImported }: { onClose: () => void; onImported: (ref: string) => void }) {
  const { adminApi } = useAuth();
  const [file, setFile] = useState<UploadFile | null>(null);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [key, setKey] = useState(newIdempotencyKey);

  const template = async () => {
    setProblem(null);
    try {
      const res = await adminApi.GET("/v1/cropcycle/sop-imports/template.xlsx", { parseAs: "blob" });
      if (!res.response.ok || !res.data) throw await parseProblemResponse(res.response);
      saveBlob(res.data, filenameFromDisposition(res.response.headers.get("Content-Disposition"), "mau-nhap-sop.xlsx"));
    } catch (e) {
      setProblem(toApiProblem(e));
    }
  };
  const onFile = async (e: ChangeEvent<HTMLInputElement>) => {
    const f = e.target.files?.[0];
    setProblem(null);
    setFile(f ? await readUpload(f) : null);
    setKey(newIdempotencyKey());
  };
  const ok = file?.kind === "xlsx";
  const submit = async () => {
    if (!file || !ok) return;
    setBusy(true);
    setProblem(null);
    try {
      const sop = await unwrap(
        adminApi.POST("/v1/cropcycle/sop-imports", {
          params: { header: { "Idempotency-Key": key } },
          body: file.body as unknown as string,
          bodySerializer: (b: unknown) => b as BodyInit,
          headers: { "Content-Type": file.contentType },
        }),
      );
      setKey(newIdempotencyKey());
      onImported(sop.ref);
    } catch (err) {
      const p = err instanceof ApiProblem ? err : toApiProblem(err);
      setProblem(p);
      // Kết quả dứt khoát (vd tệp có lỗi): lần nhập sau là thao tác mới. Chưa rõ kết quả (mạng, 5xx): giữ khóa để phát lại.
      if (!outcomeUnknown({ status: p.status, code: p.code })) setKey(newIdempotencyKey());
    } finally {
      setBusy(false);
    }
  };

  const rows = problem?.code === ROWS_CODE ? ((problem.problem.rows ?? []) as unknown as CropSOPImportRowError[]) : [];
  return (
    <Dialog
      title="Nhập SOP từ Excel"
      onClose={onClose}
      actions={
        <button type="button" className="btn btn-p btn-sm" disabled={!ok || busy} onClick={() => void submit()}>
          {busy ? "Đang nhập…" : "Nhập"}
        </button>
      }
    >
      <div className="stack" style={{ gap: 8 }}>
        <span className="small mute">Mỗi tệp một SOP. Kết quả là SOP Nháp; kiểm tra rồi phát hành.</span>
        <div className="row" style={{ alignItems: "flex-end" }}>
          <button type="button" className="btn btn-o btn-sm" onClick={() => void template()}>
            <Icon name="download" size={15} /> Tải file mẫu
          </button>
          <input type="file" aria-label="Tệp SOP (.xlsx)" accept=".xlsx" onChange={(e) => void onFile(e)} />
        </div>
        {file && !ok ? <span className="err small">Sai định dạng file: chỉ nhận Excel .xlsx.</span> : null}
        {problem ? <ProblemAlert problem={problem} /> : null}
        {rows.length > 0 ? (
          <div className="tbl-wrap">
            <table className="tbl">
              <caption>Các dòng lỗi trong tệp ({rows.length})</caption>
              <thead>
                <tr>
                  <th scope="col">Sheet</th>
                  <th scope="col">Dòng</th>
                  <th scope="col">Cột</th>
                  <th scope="col">Lý do</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((r, i) => (
                  <tr key={`${r.sheet}:${r.row}:${r.column}:${i}`}>
                    <td>{r.sheet}</td>
                    <td>{r.row}</td>
                    <td>{r.column}</td>
                    <td>{r.reason}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : null}
      </div>
    </Dialog>
  );
}
