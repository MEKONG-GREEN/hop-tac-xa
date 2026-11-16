"use client";

// Màn "Danh mục mùa vụ" của Admin (AH-7, BRD FN-01 luồng phụ Mùa vụ, BR-07). Thêm, sửa, ẩn/hiện, xóa; mùa vụ đang được
// vụ dùng thì "xóa" chỉ ẩn. Áp mẫu danh sách UX-01 (AH-292: tab có số, tìm kiếm, lọc, sắp xếp, 10 dòng/trang). Theo mẫu màn Danh mục giống (AH-2): mỗi thao tác ghi dùng MỘT Idempotency-Key
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
import { FilterBar, FilterChip, SearchBox, type SortState } from "@/components/patterns/list";
import { ListScreen, clientList, type ClientTab } from "@/components/patterns/listScreen";
import { Loading, PageHead } from "@/components/ui";
import { normalizeVi } from "@/cgh/pins";
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

const TABS: ClientTab<SeasonItem>[] = [
  { id: "all", label: "Tất cả", match: () => true },
  { id: "active", label: "Đang hiện", match: (s) => s.active },
  { id: "hidden", label: "Đã ẩn", match: (s) => !s.active },
];

const USAGE_OPTIONS = [
  { value: "", label: "Tất cả" },
  { value: "used", label: "Có vụ đang dùng" },
  { value: "unused", label: "Chưa dùng" },
];

function sortValue(s: SeasonItem, key: string): string | number | undefined {
  switch (key) {
    case "code":
      return s.code;
    case "name":
      return s.name;
    case "usage":
      return s.seasons_in_use;
    case "status":
      return s.active ? 0 : 1;
    default:
      return undefined;
  }
}

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
  const [tab, setTab] = useState("all");
  const [q, setQ] = useState("");
  const [usage, setUsage] = useState("");
  const [sort, setSort] = useState<SortState | null>(null);
  const [page, setPage] = useState(1);
  const [dlg, setDlg] = useState<Dlg | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  const term = normalizeVi(q);
  const matches = (s: SeasonItem) => {
    if (term && !normalizeVi([s.code, s.name, s.attrs.description ?? ""].join(" ")).includes(term)) return false;
    // Mục chưa có số vụ đang dùng không khớp cả hai lựa chọn có giá trị (phương án chặt).
    if (usage === "used") return s.seasons_in_use !== undefined && s.seasons_in_use > 0;
    if (usage === "unused") return s.seasons_in_use === 0;
    return true;
  };
  const list = clientList(data.data ?? [], { tabs: TABS, tab, filter: matches, sort, sortValue, page });
  // Đổi tìm kiếm, bộ lọc, tab hoặc sắp xếp thì về trang 1.
  const reset = <V,>(set: (v: V) => void) => (v: V) => {
    set(v);
    setPage(1);
  };
  const clearFilters = () => {
    setQ("");
    setUsage("");
    setPage(1);
  };

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
      {data.loading && !data.data ? <Loading /> : null}
      {data.data ? (
        <ListScreen
          caption="Danh sách mùa vụ"
          tabsLabel="Trạng thái mùa vụ"
          tabs={list.tabs}
          tab={tab}
          onTab={reset(setTab)}
          filterBar={
            <FilterBar resultCount={list.total} activeCount={(q.trim() ? 1 : 0) + (usage ? 1 : 0)} onClear={clearFilters}>
              <SearchBox value={q} onChange={reset(setQ)} placeholder="Tìm theo mã, tên hoặc mô tả" />
              <FilterChip label="Đang dùng" value={usage} options={USAGE_OPTIONS} onChange={reset(setUsage)} />
            </FilterBar>
          }
          code={{ key: "code", header: "Mã", sortKey: "code", cell: (s) => s.code }}
          main={{
            key: "name",
            header: "Mùa vụ",
            sortKey: "name",
            title: (s) => s.name,
            sub: (s) => (monthsLabel(s.attrs) ? `Dự kiến: ${monthsLabel(s.attrs)}` : undefined),
            open: (s) => ({ onClick: () => setDlg({ kind: "season", item: s }) }),
          }}
          columns={[
            { key: "description", header: "Mô tả", cell: (s) => s.attrs.description ?? "—" },
            { key: "usage", header: "Đang dùng", sortKey: "usage", className: "num", cell: (s) => usageLabel(s.seasons_in_use) },
            { key: "status", header: "Trạng thái", sortKey: "status", cell: (s) => <Status item={s} /> },
          ]}
          actions={{
            cell: (s) => (
              <ItemActions
                item={s}
                onEdit={() => setDlg({ kind: "season", item: s })}
                onState={() => setDlg({ kind: "state", item: s, hide: s.active })}
                onDelete={() => setDlg({ kind: "delete", item: s })}
              />
            ),
          }}
          rows={list.rows}
          rowKey={(s) => s.code}
          total={list.total}
          page={list.page}
          onPage={setPage}
          sort={sort}
          onSort={reset(setSort)}
          empty={data.data.length === 0 ? "Chưa có mùa vụ nào." : "Không có mùa vụ phù hợp tìm kiếm và bộ lọc."}
        />
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
