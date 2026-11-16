"use client";

// Màn "Danh mục mùa vụ" của Admin (AH-7, BRD FN-01 luồng phụ Mùa vụ, BR-07). Thêm, sửa, ẩn/hiện, xóa; mùa vụ đang được
// vụ dùng thì "xóa" chỉ ẩn. Theo mẫu màn Danh mục giống (AH-2): mỗi thao tác ghi dùng MỘT Idempotency-Key
// (useIdempotentAction): bấm lại sau lỗi mạng gửi lại đúng khóa cũ. Lỗi giữ nguyên dữ liệu trên form. Máy chủ vẫn kiểm
// lại mọi luật.
import { useId, useState, type FormEvent } from "react";
import { unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import type { ApiProblem } from "@/api/problem";
import type { CatalogItem } from "@/api/types";
import { useIdempotentAction } from "@/api/useIdempotentAction";
import { useAuth } from "@/auth/AuthProvider";
import { Dialog } from "@/components/Dialog";
import { SelectField, TextField, validateForm } from "@/components/form";
import { MfaNotice } from "@/components/MfaNotice";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Empty, Loading, PageHead } from "@/components/ui";
import { toApiProblem, useAsync } from "@/lib/useAsync";
import { ConfirmDialog, ItemActions, SaveProblem, Status } from "./CropCatalogScreen";
import { listAll } from "./cropCatalog";
import {
  fieldErrorsFromProblem,
  MONTH_OPTIONS,
  monthPairError,
  monthsLabel,
  SEASON_RULES,
  seasonBody,
  usageLabel,
  type SeasonErrors,
  type SeasonForm,
  type SeasonItem,
} from "./seasonCatalog";

type Dlg = { kind: "season"; item?: CatalogItem } | { kind: "state"; item: CatalogItem; hide: boolean } | { kind: "delete"; item: CatalogItem };

function SeasonDialog({ item, onClose, onSaved }: { item?: CatalogItem; onClose: () => void; onSaved: (notice: string) => void }) {
  const { adminApi } = useAuth();
  const action = useIdempotentAction();
  const formId = useId();
  const [form, setForm] = useState<SeasonForm>({
    name: item?.name ?? "",
    description: item?.attrs.description ?? "",
    startMonth: item?.attrs.start_month ?? "",
    endMonth: item?.attrs.end_month ?? "",
  });
  const [errors, setErrors] = useState<SeasonErrors>({});
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const set = (k: keyof SeasonForm) => (v: string) => {
    setForm((f) => ({ ...f, [k]: v }));
    setErrors((e) => ({ ...e, [k]: undefined }));
  };
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const v = validateForm(form, SEASON_RULES);
    const pair = monthPairError(form);
    if (!v.valid || Object.keys(pair).length > 0) {
      setErrors({ ...v.errors, ...pair });
      return;
    }
    setBusy(true);
    setProblem(null);
    setErrors({});
    const body = seasonBody(form);
    try {
      await action.run(() =>
        item
          ? unwrap(adminApi.PUT("/v1/masterdata/catalogs/{catalog}/items/{code}", { params: { path: { catalog: "season", code: item.code } }, body }))
          : unwrap(
              adminApi.POST("/v1/masterdata/catalogs/{catalog}/items", {
                params: { path: { catalog: "season" }, header: { "Idempotency-Key": newIdempotencyKey() } },
                body,
              }),
            ),
      );
      onSaved(item ? `Đã lưu mùa vụ “${body.name}”.` : `Đã thêm mùa vụ “${body.name}”.`);
    } catch (err) {
      // Giữ nguyên mọi ô: bấm Lưu lại gửi cùng nội dung với cùng khóa (không tạo mùa vụ thứ hai).
      const p = toApiProblem(err);
      setProblem(p);
      setErrors(fieldErrorsFromProblem(p));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog
      title={item ? "Sửa mùa vụ" : "Thêm mùa vụ"}
      onClose={onClose}
      actions={
        <button type="submit" form={formId} className="btn btn-p btn-sm" disabled={busy}>
          {busy ? "Đang lưu…" : "Lưu"}
        </button>
      }
    >
      <form id={formId} className="stack" noValidate aria-label={item ? "Sửa mùa vụ" : "Thêm mùa vụ"} onSubmit={(e) => void submit(e)}>
        <SaveProblem problem={problem} fieldErrors={errors} />
        <TextField label="Tên mùa vụ" required value={form.name} onChange={set("name")} maxLength={200} error={errors.name} />
        <TextField label="Mô tả" multiline value={form.description} onChange={set("description")} maxLength={500} error={errors.description} />
        <SelectField label="Tháng bắt đầu" value={form.startMonth} onChange={set("startMonth")} options={MONTH_OPTIONS} placeholder="Không chọn" error={errors.startMonth} />
        <SelectField
          label="Tháng kết thúc"
          value={form.endMonth}
          onChange={set("endMonth")}
          options={MONTH_OPTIONS}
          placeholder="Không chọn"
          error={errors.endMonth}
          hint="Chỉ để tham khảo, không ràng buộc mốc của vụ."
        />
      </form>
    </Dialog>
  );
}

export function SeasonCatalog() {
  const { adminApi } = useAuth();
  const data = useAsync((): Promise<SeasonItem[]> => listAll(adminApi, "season"), [adminApi]);
  const [showHidden, setShowHidden] = useState(true);
  const [dlg, setDlg] = useState<Dlg | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  const seasons = (data.data ?? []).filter((s) => showHidden || s.active);

  const done = (message: string) => {
    setDlg(null);
    setNotice(message);
    data.reload();
  };
  const call = (item: CatalogItem, op: "deactivate" | "reactivate") =>
    unwrap(
      adminApi.POST(`/v1/masterdata/catalogs/{catalog}/items/{code}/${op}`, {
        params: { path: { catalog: "season", code: item.code }, header: { "Idempotency-Key": newIdempotencyKey() } },
        body: {},
      }),
    );

  return (
    <>
      <PageHead
        title="Danh mục mùa vụ"
        subtitle="Mùa vụ dùng chung cho mọi HTX khi mở vụ và lập lịch thời vụ (chọn Vụ/Năm)"
        actions={
          <button type="button" className="btn btn-p btn-sm" onClick={() => setDlg({ kind: "season" })}>
            Thêm mùa vụ
          </button>
        }
      />
      <MfaNotice />
      {notice ? (
        <p className="alert alert-info" role="status">
          {notice}
        </p>
      ) : null}
      {data.error ? <ProblemAlert problem={data.error} onRetry={data.reload} /> : null}
      <label className="row small" style={{ margin: "8px 0" }}>
        <input type="checkbox" checked={showHidden} onChange={(e) => setShowHidden(e.target.checked)} />
        Hiện cả mục đã ẩn
      </label>
      {data.loading && !data.data ? <Loading /> : null}
      {seasons.length > 0 ? (
        <div className="tbl-wrap">
          <table className="tbl">
            <caption className="sr-only">Danh sách mùa vụ</caption>
            <thead>
              <tr>
                <th scope="col">Mùa vụ</th>
                <th scope="col">Mô tả</th>
                <th scope="col">Đang dùng</th>
                <th scope="col">Trạng thái</th>
                <th scope="col">Thao tác</th>
              </tr>
            </thead>
            <tbody>
              {seasons.map((s) => (
                <tr key={s.code}>
                  <td>
                    <div style={{ fontWeight: 600 }}>{s.name}</div>
                    {monthsLabel(s.attrs) ? <div className="small muted">Dự kiến: {monthsLabel(s.attrs)}</div> : null}
                  </td>
                  <td>{s.attrs.description ?? "—"}</td>
                  <td>{usageLabel(s.seasons_in_use)}</td>
                  <td>
                    <Status item={s} />
                  </td>
                  <td>
                    <ItemActions
                      item={s}
                      onEdit={() => setDlg({ kind: "season", item: s })}
                      onState={() => setDlg({ kind: "state", item: s, hide: s.active })}
                      onDelete={() => setDlg({ kind: "delete", item: s })}
                    />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : data.data ? (
        <Empty>Chưa có mùa vụ nào.</Empty>
      ) : null}
      {dlg?.kind === "season" ? <SeasonDialog item={dlg.item} onClose={() => setDlg(null)} onSaved={done} /> : null}
      {dlg?.kind === "state" ? (
        <ConfirmDialog
          title={dlg.hide ? "Ẩn mùa vụ" : "Hiện lại mùa vụ"}
          confirmLabel={dlg.hide ? "Ẩn" : "Hiện lại"}
          onClose={() => setDlg(null)}
          onDone={done}
          run={async () => {
            await call(dlg.item, dlg.hide ? "deactivate" : "reactivate");
            return dlg.hide ? `Đã ẩn “${dlg.item.name}”. Mùa vụ không còn trong danh sách chọn.` : `Đã hiện lại “${dlg.item.name}”.`;
          }}
        >
          {dlg.hide ? `Ẩn “${dlg.item.name}”? Vụ và lịch đã tạo vẫn hiển thị đúng tên.` : `Hiện lại “${dlg.item.name}” trong danh sách chọn?`}
        </ConfirmDialog>
      ) : null}
      {dlg?.kind === "delete" ? (
        <ConfirmDialog
          title="Xóa mùa vụ"
          confirmLabel="Xóa"
          onClose={() => setDlg(null)}
          onDone={done}
          run={async () => {
            const res = await unwrap(
              adminApi.POST("/v1/masterdata/catalogs/{catalog}/items/{code}/delete", {
                params: { path: { catalog: "season", code: dlg.item.code }, header: { "Idempotency-Key": newIdempotencyKey() } },
              }),
            );
            return res.outcome === "hidden" ? "Mùa vụ đang được dùng nên chỉ được ẩn, không xóa được." : `Đã xóa “${dlg.item.name}”.`;
          }}
        >
          {`Xóa “${dlg.item.name}”? Mùa vụ đang được dùng chỉ bị ẩn, dữ liệu cũ vẫn hiển thị đúng tên.`}
        </ConfirmDialog>
      ) : null}
    </>
  );
}
