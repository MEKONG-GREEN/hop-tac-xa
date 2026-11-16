"use client";

// Màn "Danh mục cây trồng & giống" của Admin (AH-2, BRD FN-01 B1–B4, EX-01..04, BR-01, BR-02, BR-06).
// Hai tab: Giống (mặc định) và Loại cây. Thêm, sửa, ẩn/hiện, xóa; mục đang được dùng thì "xóa" chỉ ẩn.
// Mỗi thao tác ghi dùng MỘT Idempotency-Key (useIdempotentAction): bấm lại sau lỗi mạng gửi lại đúng khóa cũ.
// Lỗi mạng giữ nguyên dữ liệu trên form (EX-04). Máy chủ vẫn kiểm lại mọi luật.
import { useId, useState, type FormEvent, type ReactNode } from "react";
import { unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import type { ApiProblem } from "@/api/problem";
import type { CatalogItem } from "@/api/types";
import { useIdempotentAction } from "@/api/useIdempotentAction";
import { useAuth } from "@/auth/AuthProvider";
import { Dialog } from "@/components/Dialog";
import { NumberField, SelectField, TextField, validateForm } from "@/components/form";
import { MfaNotice } from "@/components/MfaNotice";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Tabs } from "@/components/Tabs";
import { Empty, Loading, PageHead } from "@/components/ui";
import { toApiProblem, useAsync } from "@/lib/useAsync";
import {
  CROP_RULES,
  fieldErrorsFromProblem,
  isCropType,
  listAll,
  VARIETY_RULES,
  type CropCatalogName,
  type CropErrors,
  type CropForm,
  type VarietyErrors,
  type VarietyForm,
} from "./cropCatalog";

const TABS = [
  { id: "varieties", label: "Giống" },
  { id: "crops", label: "Loại cây" },
];

type Dlg =
  | { kind: "variety"; item?: CatalogItem }
  | { kind: "crop"; item?: CatalogItem }
  | { kind: "state"; item: CatalogItem; hide: boolean }
  | { kind: "delete"; item: CatalogItem };

export function Status({ item }: { item: CatalogItem }) {
  return item.active ? <span className="chip c-ok">Đang hiện</span> : <span className="chip c-mute">Đã ẩn</span>;
}

/** Thân chung của hộp thoại ghi: lỗi chung của máy chủ (không gắn với ô nào) hiện trên cùng. */
export function SaveProblem({ problem, fieldErrors }: { problem: ApiProblem | null; fieldErrors: object }) {
  if (!problem || Object.keys(fieldErrors).length > 0) return null;
  return <ProblemAlert problem={problem} />;
}

function VarietyDialog({ crops, item, onClose, onSaved }: { crops: CatalogItem[]; item?: CatalogItem; onClose: () => void; onSaved: (notice: string) => void }) {
  const { adminApi } = useAuth();
  const action = useIdempotentAction();
  const formId = useId();
  const [form, setForm] = useState<VarietyForm>({ crop: item?.attrs.crop_code ?? "", name: item?.name ?? "", growthDays: item?.attrs.growth_days ?? "" });
  const [errors, setErrors] = useState<VarietyErrors>({});
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const set = (k: keyof VarietyForm) => (v: string) => {
    setForm((f) => ({ ...f, [k]: v }));
    setErrors((e) => ({ ...e, [k]: undefined }));
  };
  // Loại cây đang hiện; khi sửa giống của loại cây đã ẩn thì vẫn hiện loại cây đó để ô chọn không trống.
  const options = crops.filter((c) => c.active || c.code === item?.attrs.crop_code).map((c) => ({ value: c.code, label: c.active ? c.name : `${c.name} (đã ẩn)` }));
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const v = validateForm(form, VARIETY_RULES);
    if (!v.valid) {
      setErrors(v.errors);
      return;
    }
    setBusy(true);
    setProblem(null);
    setErrors({});
    const body = { name: v.values.name.trim(), attrs: { crop_code: v.values.crop, growth_days: v.values.growthDays } };
    try {
      await action.run(() =>
        item
          ? unwrap(adminApi.PUT("/v1/masterdata/catalogs/{catalog}/items/{code}", { params: { path: { catalog: "rice_variety", code: item.code } }, body }))
          : unwrap(
              adminApi.POST("/v1/masterdata/catalogs/{catalog}/items", {
                params: { path: { catalog: "rice_variety" }, header: { "Idempotency-Key": newIdempotencyKey() } },
                body,
              }),
            ),
      );
      onSaved(item ? `Đã lưu giống “${body.name}”.` : `Đã thêm giống “${body.name}”.`);
    } catch (err) {
      // Giữ nguyên mọi ô: bấm Lưu lại gửi cùng nội dung với cùng khóa (không tạo giống thứ hai).
      const p = toApiProblem(err);
      setProblem(p);
      setErrors(fieldErrorsFromProblem(p, "variety"));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog
      title={item ? "Sửa giống" : "Thêm giống"}
      onClose={onClose}
      actions={
        <button type="submit" form={formId} className="btn btn-p btn-sm" disabled={busy}>
          {busy ? "Đang lưu…" : "Lưu"}
        </button>
      }
    >
      <form id={formId} className="stack" noValidate aria-label={item ? "Sửa giống" : "Thêm giống"} onSubmit={(e) => void submit(e)}>
        <SaveProblem problem={problem} fieldErrors={errors} />
        <SelectField label="Loại cây" required value={form.crop} onChange={set("crop")} options={options} error={errors.crop} placeholder="Chọn loại cây" />
        <TextField label="Tên giống" required value={form.name} onChange={set("name")} maxLength={200} error={errors.name} />
        <NumberField
          label="Thời gian sinh trưởng"
          unit="ngày"
          required
          rule={{ integer: true, positive: true }}
          value={form.growthDays}
          onChange={set("growthDays")}
          error={errors.growthDays}
          hint="Số nguyên lớn hơn 0. Chỉ để tham khảo."
        />
      </form>
    </Dialog>
  );
}

function CropDialog({ item, onClose, onSaved }: { item?: CatalogItem; onClose: () => void; onSaved: (notice: string) => void }) {
  const { adminApi } = useAuth();
  const action = useIdempotentAction();
  const formId = useId();
  const [form, setForm] = useState<CropForm>({ name: item?.name ?? "", isRice: item?.attrs.is_rice ?? "false" });
  const [errors, setErrors] = useState<CropErrors>({});
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const v = validateForm(form, CROP_RULES);
    if (!v.valid) {
      setErrors(v.errors);
      return;
    }
    setBusy(true);
    setProblem(null);
    setErrors({});
    // Luôn gửi is_rice (máy chủ bắt buộc khi thêm; thuộc tính cũ của mục được máy chủ giữ nguyên khi sửa).
    const body = { name: v.values.name.trim(), attrs: { is_rice: v.values.isRice } };
    try {
      await action.run(() =>
        item
          ? unwrap(adminApi.PUT("/v1/masterdata/catalogs/{catalog}/items/{code}", { params: { path: { catalog: "crop", code: item.code } }, body }))
          : unwrap(
              adminApi.POST("/v1/masterdata/catalogs/{catalog}/items", {
                params: { path: { catalog: "crop" }, header: { "Idempotency-Key": newIdempotencyKey() } },
                body,
              }),
            ),
      );
      onSaved(item ? `Đã lưu loại cây “${body.name}”.` : `Đã thêm loại cây “${body.name}”.`);
    } catch (err) {
      const p = toApiProblem(err);
      setProblem(p);
      setErrors(fieldErrorsFromProblem(p, "crop"));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog
      title={item ? "Sửa loại cây" : "Thêm loại cây"}
      onClose={onClose}
      actions={
        <button type="submit" form={formId} className="btn btn-p btn-sm" disabled={busy}>
          {busy ? "Đang lưu…" : "Lưu"}
        </button>
      }
    >
      <form id={formId} className="stack" noValidate aria-label={item ? "Sửa loại cây" : "Thêm loại cây"} onSubmit={(e) => void submit(e)}>
        <SaveProblem problem={problem} fieldErrors={errors} />
        <TextField
          label="Tên loại cây"
          required
          value={form.name}
          onChange={(v) => {
            setForm((f) => ({ ...f, name: v }));
            setErrors((er) => ({ ...er, name: undefined }));
          }}
          maxLength={200}
          error={errors.name}
        />
        <label className="row small">
          <input type="checkbox" checked={form.isRice === "true"} onChange={(e) => setForm((f) => ({ ...f, isRice: e.target.checked ? "true" : "false" }))} />
          Là lúa
        </label>
        {errors.isRice ? (
          <span className="err" role="alert">
            {errors.isRice}
          </span>
        ) : null}
      </form>
    </Dialog>
  );
}

/** Hộp xác nhận ẩn/hiện và xóa: cùng một khung, khác lời gọi. */
export function ConfirmDialog({
  title,
  confirmLabel,
  run,
  children,
  onClose,
  onDone,
}: {
  title: string;
  confirmLabel: string;
  run: () => Promise<string>;
  children: ReactNode;
  onClose: () => void;
  onDone: (notice: string) => void;
}) {
  const action = useIdempotentAction();
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const confirm = async () => {
    setBusy(true);
    setProblem(null);
    try {
      onDone(await action.run(run));
    } catch (err) {
      setProblem(toApiProblem(err));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog
      title={title}
      onClose={onClose}
      actions={
        <button type="button" className="btn btn-p btn-sm" disabled={busy} onClick={() => void confirm()}>
          {busy ? "Đang xử lý…" : confirmLabel}
        </button>
      }
    >
      {problem ? <ProblemAlert problem={problem} /> : null}
      <p>{children}</p>
    </Dialog>
  );
}

export function ItemActions({ item, onEdit, onState, onDelete }: { item: CatalogItem; onEdit: () => void; onState: () => void; onDelete: () => void }) {
  return (
    <div className="row" style={{ gap: 4 }}>
      <button type="button" className="btn btn-o btn-sm" aria-label={`Sửa ${item.name}`} onClick={onEdit}>
        Sửa
      </button>
      <button type="button" className="btn btn-o btn-sm" aria-label={`${item.active ? "Ẩn" : "Hiện"} ${item.name}`} onClick={onState}>
        {item.active ? "Ẩn" : "Hiện"}
      </button>
      <button type="button" className="btn btn-o btn-sm" aria-label={`Xóa ${item.name}`} onClick={onDelete}>
        Xóa
      </button>
    </div>
  );
}

export function CropCatalog() {
  const { adminApi } = useAuth();
  const data = useAsync(async () => {
    const [crops, varieties] = await Promise.all([listAll(adminApi, "crop"), listAll(adminApi, "rice_variety")]);
    // Mục crop cũ có nhóm cha không còn là loại cây: không hiện ở màn này.
    return { crops: crops.filter(isCropType), varieties };
  }, [adminApi]);
  const [tab, setTab] = useState("varieties");
  const [showHidden, setShowHidden] = useState(true);
  const [dlg, setDlg] = useState<Dlg | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  const crops = data.data?.crops ?? [];
  const cropName = new Map(crops.map((c) => [c.code, c.name]));
  const varieties = (data.data?.varieties ?? []).filter((v) => showHidden || v.active);
  const shownCrops = crops.filter((c) => showHidden || c.active);
  const visibleCrops = crops.filter((c) => c.active);

  const done = (message: string) => {
    setDlg(null);
    setNotice(message);
    data.reload();
  };
  const call = (item: CatalogItem, op: "deactivate" | "reactivate") =>
    unwrap(
      adminApi.POST(`/v1/masterdata/catalogs/{catalog}/items/{code}/${op}`, {
        params: { path: { catalog: item.catalog as CropCatalogName, code: item.code }, header: { "Idempotency-Key": newIdempotencyKey() } },
        body: {},
      }),
    );

  return (
    <>
      <PageHead
        title="Danh mục cây trồng & giống"
        subtitle="Loại cây và giống kèm thời gian sinh trưởng, dùng chung cho mọi HTX khi mở vụ"
        actions={
          tab === "varieties" ? (
            <button type="button" className="btn btn-p btn-sm" disabled={visibleCrops.length === 0} onClick={() => setDlg({ kind: "variety" })}>
              Thêm giống
            </button>
          ) : (
            <button type="button" className="btn btn-p btn-sm" onClick={() => setDlg({ kind: "crop" })}>
              Thêm loại cây
            </button>
          )
        }
      />
      <MfaNotice />
      {notice ? (
        <p className="alert alert-info" role="status">
          {notice}
        </p>
      ) : null}
      {data.error ? <ProblemAlert problem={data.error} onRetry={data.reload} /> : null}
      <Tabs tabs={TABS} value={tab} onChange={setTab} label="Danh mục cây trồng và giống">
        <label className="row small" style={{ margin: "8px 0" }}>
          <input type="checkbox" checked={showHidden} onChange={(e) => setShowHidden(e.target.checked)} />
          Hiện cả mục đã ẩn
        </label>
        {data.loading && !data.data ? <Loading /> : null}
        {tab === "varieties" ? (
          varieties.length > 0 ? (
            <div className="tbl-wrap">
              <table className="tbl">
                <caption className="sr-only">Danh sách giống</caption>
                <thead>
                  <tr>
                    <th scope="col">Loại cây</th>
                    <th scope="col">Tên giống</th>
                    <th scope="col">Thời gian sinh trưởng (ngày)</th>
                    <th scope="col">Trạng thái</th>
                    <th scope="col">Thao tác</th>
                  </tr>
                </thead>
                <tbody>
                  {varieties.map((v) => (
                    <tr key={v.code}>
                      <td>{cropName.get(v.attrs.crop_code ?? "") ?? v.attrs.crop_code ?? "—"}</td>
                      <td style={{ fontWeight: 600 }}>{v.name}</td>
                      <td>{v.attrs.growth_days ?? "—"}</td>
                      <td>
                        <Status item={v} />
                      </td>
                      <td>
                        <ItemActions
                          item={v}
                          onEdit={() => setDlg({ kind: "variety", item: v })}
                          onState={() => setDlg({ kind: "state", item: v, hide: v.active })}
                          onDelete={() => setDlg({ kind: "delete", item: v })}
                        />
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : data.data ? (
            <Empty>Chưa có giống nào.</Empty>
          ) : null
        ) : shownCrops.length > 0 ? (
          <div className="tbl-wrap">
            <table className="tbl">
              <caption className="sr-only">Danh sách loại cây</caption>
              <thead>
                <tr>
                  <th scope="col">Tên</th>
                  <th scope="col">Là lúa</th>
                  <th scope="col">Trạng thái</th>
                  <th scope="col">Thao tác</th>
                </tr>
              </thead>
              <tbody>
                {shownCrops.map((c) => (
                  <tr key={c.code}>
                    <td style={{ fontWeight: 600 }}>{c.name}</td>
                    <td>{c.attrs.is_rice === "true" ? "Có" : "Không"}</td>
                    <td>
                      <Status item={c} />
                    </td>
                    <td>
                      <ItemActions
                        item={c}
                        onEdit={() => setDlg({ kind: "crop", item: c })}
                        onState={() => setDlg({ kind: "state", item: c, hide: c.active })}
                        onDelete={() => setDlg({ kind: "delete", item: c })}
                      />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : data.data ? (
          <Empty>Chưa có loại cây nào.</Empty>
        ) : null}
      </Tabs>
      {dlg?.kind === "variety" ? <VarietyDialog crops={crops} item={dlg.item} onClose={() => setDlg(null)} onSaved={done} /> : null}
      {dlg?.kind === "crop" ? <CropDialog item={dlg.item} onClose={() => setDlg(null)} onSaved={done} /> : null}
      {dlg?.kind === "state" ? (
        <ConfirmDialog
          title={dlg.hide ? "Ẩn mục" : "Hiện lại mục"}
          confirmLabel={dlg.hide ? "Ẩn" : "Hiện lại"}
          onClose={() => setDlg(null)}
          onDone={done}
          run={async () => {
            await call(dlg.item, dlg.hide ? "deactivate" : "reactivate");
            return dlg.hide ? `Đã ẩn “${dlg.item.name}”. Mục không còn trong danh sách chọn.` : `Đã hiện lại “${dlg.item.name}”.`;
          }}
        >
          {dlg.hide ? `Ẩn “${dlg.item.name}”? Dữ liệu cũ vẫn hiển thị đúng tên.` : `Hiện lại “${dlg.item.name}” trong danh sách chọn?`}
        </ConfirmDialog>
      ) : null}
      {dlg?.kind === "delete" ? (
        <ConfirmDialog
          title="Xóa mục"
          confirmLabel="Xóa"
          onClose={() => setDlg(null)}
          onDone={done}
          run={async () => {
            const res = await unwrap(
              adminApi.POST("/v1/masterdata/catalogs/{catalog}/items/{code}/delete", {
                params: { path: { catalog: dlg.item.catalog as CropCatalogName, code: dlg.item.code }, header: { "Idempotency-Key": newIdempotencyKey() } },
              }),
            );
            return res.outcome === "hidden" ? "Mục đang được sử dụng nên chỉ được ẩn, không xóa." : `Đã xóa “${dlg.item.name}”.`;
          }}
        >
          {`Xóa “${dlg.item.name}”? Mục đang được sử dụng chỉ bị ẩn, dữ liệu cũ vẫn hiển thị đúng tên.`}
        </ConfirmDialog>
      ) : null}
    </>
  );
}
