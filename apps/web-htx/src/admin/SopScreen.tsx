"use client";

// Màn "Quy trình SOP" của Admin (AH-8, BRD FN-01 B3–B5, EX-05, BR-03..05): Thư viện SOP và Soạn SOP dạng checklist
// (giai đoạn → việc → cửa sổ thực hiện → vật tư, liều). Lưu nháp, phát hành một bước, phiên bản mới, ngừng áp dụng.
// Mỗi thao tác ghi dùng MỘT Idempotency-Key (useIdempotentAction): bấm lại sau lỗi mạng gửi lại đúng khóa cũ.
// Máy chủ kiểm lại mọi luật và trả lỗi theo trường (stages[i].name, steps[j].window.to, ...) — màn tô đỏ đúng ô.
import { useMemo, useState, type ReactNode } from "react";
import { unwrap } from "@/api/client";
import { newIdempotencyKey, withIdempotentOperation } from "@/api/idempotency";
import { collectPages } from "@/api/pagination";
import type { ApiProblem } from "@/api/problem";
import type { AdminCropSOP, CatalogItem, CropActivity, CropSOPTemplate } from "@/api/types";
import { useIdempotentAction } from "@/api/useIdempotentAction";
import { useAuth } from "@/auth/AuthProvider";
import { hasRole, ROLES } from "@/auth/principal";
import { Icon } from "@/components/Icon";
import { Dialog } from "@/components/Dialog";
import { MfaNotice } from "@/components/MfaNotice";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Empty, Loading, PageHead } from "@/components/ui";
import { ReasonDialog, useCatalog, useCatalogNames } from "@/htx/common";
import {
  ACTIVITIES,
  ACTIVITY_LABEL,
  ANCHORS,
  ANCHOR_LABEL,
  blankMaterial,
  blankSop,
  blankStage,
  blankTask,
  flatIndex,
  fromSopVersion,
  moveItem,
  parseDecimalInput,
  stageHasRange,
  toSopPayload,
  validateSop,
  WINDOW_KINDS,
  WINDOW_KIND_LABEL,
  type SopAnchor,
  type SopDraft,
  type SopIssue,
  type SopMaterialDraft,
  type SopStageDraft,
  type SopTaskDraft,
  type SopWindowKind,
} from "@/htx/rules";
import { formatDateTime } from "@/lib/format";
import { toApiProblem, useAsync } from "@/lib/useAsync";
import { ConfirmDialog } from "./CropCatalogScreen";
import { SopImportDialog } from "./SopImport";

const TPL_STATUS: Record<string, { text: string; cls: string }> = {
  draft: { text: "Nháp", cls: "c-warn" },
  published: { text: "Đang áp dụng", cls: "c-ok" },
  retired: { text: "Ngừng áp dụng", cls: "c-mute" },
};

/** Trạng thái hiển thị của phiên bản — do máy chủ suy ra (`state`), màn hình chỉ đọc lại và gắn nhãn. */
const VERSION_STATE: Record<string, { text: string; cls: string }> = {
  draft: TPL_STATUS.draft!,
  active: TPL_STATUS.published!,
  retired: TPL_STATUS.retired!,
};

const IDEM = () => ({ "Idempotency-Key": newIdempotencyKey() });

/** Lỗi của máy chủ theo trường: field → lý do đầu tiên. */
function serverIssues(p: ApiProblem | null): Record<string, string> {
  const out: Record<string, string> = {};
  for (const e of p?.fieldErrors ?? []) if (e.field && !(e.field in out)) out[e.field] = e.reason ?? "";
  return out;
}

function FieldError({ message }: { message?: string }) {
  return message ? (
    <span className="err small" role="alert">
      {message}
    </span>
  ) : null;
}

// --------------------------------------------------------------------------------------------------------------------
// Soạn SOP
// --------------------------------------------------------------------------------------------------------------------
type Dlg = "" | "retire" | "clone" | "delete";

function Editor({
  sop,
  onBack,
  onChanged,
  onDeleted,
}: {
  sop: AdminCropSOP | null;
  onBack: () => void;
  onChanged: (ref: string | null, notice?: string) => void;
  onDeleted: (notice: string) => void;
}) {
  const { adminApi, principal } = useAuth();
  const action = useIdempotentAction();
  const crops = useCatalog("crop");
  const varieties = useCatalog("rice_variety");
  const materials = useCatalog("material");
  const canAuthor = hasRole(principal, ROLES.sopAdmin, ROLES.platformAdmin);
  const latest = sop?.versions.at(-1);
  const isDraftVersion = !sop || latest?.status === "draft";
  const retired = sop?.status === "retired";
  const editable = canAuthor && isDraftVersion && !retired;
  const infoEditable = editable && (!sop || (sop.status === "draft" && sop.published_version == null));

  const [draft, setDraftRaw] = useState<SopDraft>(() => (sop && latest ? fromSopVersion(sop, latest) : blankSop()));
  const [saved, setSaved] = useState(() => JSON.stringify(draft));
  const [touched, setTouched] = useState(false);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [server, setServer] = useState<Record<string, string>>({});
  const [notice, setNotice] = useState<string | null>(null);
  const [dialog, setDialog] = useState<Dlg>("");
  const [cloneName, setCloneName] = useState("");
  const [cloneCrop, setCloneCrop] = useState("");
  const [cloneVariety, setCloneVariety] = useState("");
  const [cloneAnchor, setCloneAnchor] = useState("");
  const [viewNo, setViewNo] = useState<number | null>(null);
  const [publishIssues, setPublishIssues] = useState<SopIssue[]>([]);
  const dirty = JSON.stringify(draft) !== saved;

  const setDraft = (fn: (d: SopDraft) => SopDraft) => setDraftRaw((d) => fn(d));
  const issues = useMemo(() => (touched ? validateSop(draft) : []), [touched, draft]);
  const errOf = (path: string): string | undefined => issues.find((i) => i.path === path)?.message ?? publishIssues.find((i) => i.path === path)?.message ?? server[path];
  const attrs = (path: string) => ({ "aria-invalid": errOf(path) ? true : undefined });

  const cropOptions = crops.filter((c) => !c.attrs.parent_code && (c.active || c.code === draft.crop));
  const varietyOptions = varieties.filter((v) => v.attrs.crop_code === draft.crop && (v.active || v.code === draft.varietyCode));
  const cloneCropOptions = crops.filter((c) => !c.attrs.parent_code && (c.active || c.code === sop?.crop));
  const cloneVarietyOptions = varieties.filter((v) => v.attrs.crop_code === cloneCrop && (v.active || v.code === sop?.variety_code));
  const canDelete = canAuthor && !!sop && sop.status === "draft" && sop.published_version == null;

  const setStage = (si: number, patch: Partial<SopStageDraft>) => setDraft((d) => ({ ...d, stages: d.stages.map((s, i) => (i === si ? { ...s, ...patch } : s)) }));
  const setTask = (si: number, ti: number, patch: Partial<SopTaskDraft>) =>
    setStage(si, { tasks: draft.stages[si]!.tasks.map((t, i) => (i === ti ? { ...t, ...patch } : t)) });
  const setMaterial = (si: number, ti: number, k: number, patch: Partial<SopMaterialDraft>) =>
    setTask(si, ti, { materials: draft.stages[si]!.tasks[ti]!.materials.map((m, i) => (i === k ? { ...m, ...patch } : m)) });

  const guard = async (fn: () => Promise<void>) => {
    setBusy(true);
    setProblem(null);
    setServer({});
    setNotice(null);
    try {
      await fn();
    } catch (e) {
      const p = toApiProblem(e);
      setProblem(p);
      setServer(serverIssues(p));
    } finally {
      setBusy(false);
    }
  };

  const save = () => {
    setTouched(true);
    setPublishIssues([]);
    if (validateSop(draft).length > 0) return;
    const { stages, steps } = toSopPayload(draft);
    void guard(async () => {
      if (!sop) {
        const res = await action.run(() =>
          unwrap(
            adminApi.POST("/v1/cropcycle/sops", {
              params: { header: IDEM() },
              body: {
                ...(draft.code.trim() ? { code: draft.code.trim().toUpperCase() } : {}),
                name: draft.name.trim(),
                crop: draft.crop,
                ...(draft.varietyCode ? { variety_code: draft.varietyCode } : {}),
                anchor_name: draft.anchorName.trim(),
                scope: "system",
                stages,
                steps,
              },
            }),
          ),
        );
        onChanged(res.ref, "Đã lưu nháp. SOP chưa dùng cho vụ cho tới khi phát hành.");
        return;
      }
      const run = action.begin();
      if (infoEditable) {
        await withIdempotentOperation(run, () =>
          unwrap(
            adminApi.POST("/v1/cropcycle/sops/{ref}/info", {
              params: { path: { ref: sop.ref }, header: IDEM() },
              body: { name: draft.name.trim(), crop: draft.crop, variety_code: draft.varietyCode || null, anchor_name: draft.anchorName.trim() },
            }),
          ),
        );
      }
      await withIdempotentOperation(run, () =>
        unwrap(adminApi.POST("/v1/cropcycle/sop-versions/{id}/steps", { params: { path: { id: latest!.id }, header: IDEM() }, body: { stages, steps } })),
      );
      run.complete();
      setSaved(JSON.stringify(draft));
      onChanged(sop.ref, "Đã lưu nháp.");
    });
  };

  const publish = () => {
    setTouched(true);
    const block = validateSop(draft, true);
    setPublishIssues(block);
    if (block.length > 0) {
      setProblem(null);
      return;
    }
    void guard(async () => {
      await action.run(() => unwrap(adminApi.POST("/v1/cropcycle/sop-versions/{id}/publish", { params: { path: { id: latest!.id }, header: IDEM() } })));
      onChanged(sop!.ref, "Đã phát hành — SOP đang áp dụng, vụ mở mới dùng được.");
    });
  };

  const newVersion = () =>
    void guard(async () => {
      await action.run(() => unwrap(adminApi.POST("/v1/cropcycle/sops/{ref}/versions", { params: { path: { ref: sop!.ref }, header: IDEM() }, body: {} })));
      onChanged(sop!.ref, "Đã tạo bản nháp phiên bản mới.");
    });

  const publishBlockMessages = publishIssues.map((i) => i.message);

  return (
    <div className="stack" style={{ gap: 14 }}>
      <div className="row">
        <button type="button" className="btn btn-o btn-sm" onClick={onBack}>
          ← Thư viện SOP
        </button>
        {sop ? (
          <span className="mute small">
            <code>{sop.code}</code> · <span className={`chip ${TPL_STATUS[sop.status]?.cls ?? "c-mute"}`}>{TPL_STATUS[sop.status]?.text ?? sop.status}</span>
            {latest ? ` · Phiên bản ${latest.version_no} (${latest.status === "draft" ? "nháp" : "đã phát hành, không sửa được"}) · soạn ${formatDateTime(latest.authored_at)}` : ""}
          </span>
        ) : null}
      </div>

      <section className="panel stack" style={{ gap: 10 }} aria-label="Thông tin chung">
        <b>Thông tin chung</b>
        <div className="row" style={{ alignItems: "flex-start" }}>
          <div className="field">
            <label htmlFor="sop-crop">Loại cây *</label>
            <select
              id="sop-crop"
              className="select"
              disabled={!infoEditable}
              {...attrs("crop")}
              value={draft.crop}
              onChange={(e) => setDraft((d) => ({ ...d, crop: e.target.value, varietyCode: "" }))}
            >
              <option value="">Chọn loại cây</option>
              {cropOptions.map((c) => (
                <option key={c.code} value={c.code}>
                  {c.name}
                </option>
              ))}
            </select>
            <FieldError message={errOf("crop")} />
          </div>
          <div className="field">
            <label htmlFor="sop-variety">Giống (tùy chọn)</label>
            <select id="sop-variety" className="select" disabled={!infoEditable || !draft.crop} {...attrs("variety_code")} value={draft.varietyCode} onChange={(e) => setDraft((d) => ({ ...d, varietyCode: e.target.value }))}>
              <option value="">Chung cho cả loại cây</option>
              {varietyOptions.map((v) => (
                <option key={v.code} value={v.code}>
                  {v.name}
                </option>
              ))}
            </select>
            <FieldError message={errOf("variety_code")} />
          </div>
          <div className="field" style={{ flex: "1 1 260px" }}>
            <label htmlFor="sop-name">Tên SOP *</label>
            <input id="sop-name" className="input" disabled={!infoEditable} maxLength={200} {...attrs("name")} value={draft.name} onChange={(e) => setDraft((d) => ({ ...d, name: e.target.value }))} />
            <FieldError message={errOf("name")} />
          </div>
          <div className="field">
            <label htmlFor="sop-anchor">Tên mốc gốc *</label>
            <input
              id="sop-anchor"
              className="input"
              disabled={!infoEditable}
              maxLength={100}
              placeholder="vd Ngày xuống giống"
              {...attrs("anchor_name")}
              value={draft.anchorName}
              onChange={(e) => setDraft((d) => ({ ...d, anchorName: e.target.value }))}
            />
            <FieldError message={errOf("anchor_name")} />
          </div>
          {!sop ? (
            <div className="field">
              <label htmlFor="sop-code">Mã SOP (tùy chọn)</label>
              <input id="sop-code" className="input" placeholder="bỏ trống để hệ thống sinh" value={draft.code} onChange={(e) => setDraft((d) => ({ ...d, code: e.target.value }))} />
            </div>
          ) : null}
        </div>
      </section>

      {draft.stages.map((s, si) => (
        <section key={si} className="panel stack" style={{ gap: 10 }} aria-label={`Giai đoạn ${si + 1}`}>
          <div className="row" style={{ alignItems: "flex-end" }}>
            <div className="field" style={{ flex: "1 1 240px" }}>
              <label htmlFor={`stage-${si}-name`}>{`Tên giai đoạn ${si + 1}`}</label>
              <input id={`stage-${si}-name`} className="input" disabled={!editable} maxLength={200} {...attrs(`stages[${si}].name`)} value={s.name} onChange={(e) => setStage(si, { name: e.target.value })} />
              <FieldError message={errOf(`stages[${si}].name`)} />
            </div>
            <div className="field">
              <label htmlFor={`stage-${si}-from`}>{`Từ (ngày) giai đoạn ${si + 1}`}</label>
              <input id={`stage-${si}-from`} className="input grid-input" inputMode="numeric" disabled={!editable} {...attrs(`stages[${si}].from_day`)} value={s.from} onChange={(e) => setStage(si, { from: e.target.value })} />
              <FieldError message={errOf(`stages[${si}].from_day`)} />
            </div>
            <div className="field">
              <label htmlFor={`stage-${si}-to`}>{`Đến (ngày) giai đoạn ${si + 1}`}</label>
              <input id={`stage-${si}-to`} className="input grid-input" inputMode="numeric" disabled={!editable} {...attrs(`stages[${si}].to_day`)} value={s.to} onChange={(e) => setStage(si, { to: e.target.value })} />
              <FieldError message={errOf(`stages[${si}].to_day`)} />
            </div>
            {editable ? (
              <div className="row" style={{ gap: 4, flexWrap: "nowrap" }}>
                <button type="button" className="btn btn-o btn-sm" aria-label={`Chuyển giai đoạn ${si + 1} lên`} disabled={si === 0} onClick={() => setDraft((d) => ({ ...d, stages: moveItem(d.stages, si, -1) }))}>
                  ↑
                </button>
                <button type="button" className="btn btn-o btn-sm" aria-label={`Chuyển giai đoạn ${si + 1} xuống`} disabled={si === draft.stages.length - 1} onClick={() => setDraft((d) => ({ ...d, stages: moveItem(d.stages, si, 1) }))}>
                  ↓
                </button>
                <button type="button" className="btn btn-o btn-sm" aria-label={`Xóa giai đoạn ${si + 1}`} onClick={() => setDraft((d) => ({ ...d, stages: d.stages.filter((_, i) => i !== si) }))}>
                  ✕
                </button>
              </div>
            ) : null}
          </div>
          <FieldError message={errOf(`stages[${si}]`)} />
          <p className="mute small">Khoảng ngày so với mốc gốc, cho phép số âm; để trống nếu giai đoạn không có khoảng ngày.</p>

          {s.tasks.map((t, ti) => (
            <TaskRow
              key={ti}
              n={`${si + 1}.${ti + 1}`}
              path={`steps[${flatIndex(draft.stages, si, ti)}]`}
              task={t}
              stage={s}
              editable={editable}
              materials={materials}
              errOf={errOf}
              onChange={(patch) => setTask(si, ti, patch)}
              onMaterial={(k, patch) => setMaterial(si, ti, k, patch)}
              onMove={(delta) => setStage(si, { tasks: moveItem(s.tasks, ti, delta) })}
              onRemove={() => setStage(si, { tasks: s.tasks.filter((_, i) => i !== ti) })}
              isFirst={ti === 0}
              isLast={ti === s.tasks.length - 1}
            />
          ))}
          {editable ? (
            <div>
              <button type="button" className="btn btn-o btn-sm" onClick={() => setStage(si, { tasks: [...s.tasks, blankTask()] })}>
                {`+ Thêm việc vào giai đoạn ${si + 1}`}
              </button>
            </div>
          ) : null}
        </section>
      ))}
      <FieldError message={errOf("stages") ?? errOf("steps")} />
      {editable ? (
        <div>
          <button type="button" className="btn btn-o btn-sm" onClick={() => setDraft((d) => ({ ...d, stages: [...d.stages, blankStage()] }))}>
            + Thêm giai đoạn
          </button>
        </div>
      ) : null}

      {publishBlockMessages.length > 0 ? (
        <div className="alert alert-bad" role="alert">
          <Icon name="alert" />
          <div>
            <b>Chưa phát hành được</b>
            <ul style={{ margin: "6px 0 0 18px" }}>
              {publishBlockMessages.map((m) => (
                <li key={m}>{m}</li>
              ))}
            </ul>
          </div>
        </div>
      ) : null}
      {problem ? <ProblemAlert problem={problem} /> : null}
      {notice ? <p role="status">{notice}</p> : null}

      <div className="row">
        {editable ? (
          <button type="button" className="btn btn-o btn-sm" disabled={busy} onClick={save}>
            {busy ? "Đang lưu…" : "Lưu nháp"}
          </button>
        ) : null}
        {editable && sop ? (
          <button type="button" className="btn btn-p btn-sm" disabled={busy || dirty} title={dirty ? "Lưu nháp trước khi phát hành" : undefined} onClick={publish}>
            Phát hành
          </button>
        ) : null}
        {canAuthor && sop && !isDraftVersion && !retired ? (
          <button type="button" className="btn btn-p btn-sm" disabled={busy} onClick={newVersion}>
            Tạo phiên bản mới
          </button>
        ) : null}
        {canAuthor && sop && !retired ? (
          <button
            type="button"
            className="btn btn-o btn-sm"
            onClick={() => {
              setCloneName(`${sop.name} (bản sao)`);
              setCloneCrop(sop.crop);
              setCloneVariety(sop.variety_code ?? "");
              setCloneAnchor(sop.anchor_name);
              setProblem(null);
              setServer({});
              setDialog("clone");
            }}
          >
            Nhân bản
          </button>
        ) : null}
        {canAuthor && sop && !retired ? (
          <button type="button" className="btn btn-o btn-sm" onClick={() => setDialog("retire")}>
            Ngừng áp dụng
          </button>
        ) : null}
        {canDelete ? (
          <button type="button" className="btn btn-o btn-sm" onClick={() => setDialog("delete")}>
            Xóa
          </button>
        ) : null}
        {!canAuthor ? <span className="small mute">Bạn chỉ xem SOP; soạn và phát hành thuộc vai Admin.</span> : null}
      </div>
      {sop ? (
        <section className="card sec" aria-label="Lịch sử phiên bản">
          <div className="sec-h">
            <h2>Lịch sử phiên bản</h2>
          </div>
          <div className="tbl-wrap">
            <table className="tbl">
              <caption className="sr-only">Lịch sử phiên bản</caption>
              <thead>
                <tr>
                  <th scope="col">Phiên bản</th>
                  <th scope="col">Trạng thái</th>
                  <th scope="col">Người phát hành</th>
                  <th scope="col">Ngày phát hành</th>
                  <th scope="col">Ghi chú thay đổi</th>
                  <th scope="col">
                    <span className="sr-only">Xem</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {sop.versions.map((v) => (
                  <tr key={v.id}>
                    <td>v{v.version_no}</td>
                    <td>
                      <span className={`chip ${VERSION_STATE[v.state]?.cls ?? "c-mute"}`}>{VERSION_STATE[v.state]?.text ?? v.state}</span>
                    </td>
                    <td>{v.published_by_display ?? "—"}</td>
                    <td>{v.published_at ? formatDateTime(v.published_at) : "—"}</td>
                    <td>{v.change_note ?? "—"}</td>
                    <td>
                      <button type="button" className="btn btn-o btn-sm" aria-label={`Xem phiên bản ${v.version_no}`} onClick={() => setViewNo(v.version_no)}>
                        Xem
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>
      ) : null}
      {viewNo !== null && sop ? <VersionDialog sop={sop} versionNo={viewNo} materials={materials} onClose={() => setViewNo(null)} /> : null}
      {dialog === "clone" && sop ? (
        <Dialog
          title={`Nhân bản ${sop.name}`}
          onClose={() => setDialog("")}
          actions={
            <button
              type="button"
              className="btn btn-p btn-sm"
              disabled={busy || !cloneName.trim() || !cloneCrop || !cloneAnchor.trim()}
              onClick={() =>
                void guard(async () => {
                  const res = await action.run(() =>
                    unwrap(
                      adminApi.POST("/v1/cropcycle/sops/{ref}/clone", {
                        params: { path: { ref: sop.ref }, header: IDEM() },
                        // Giống rỗng = SOP chung của loại cây (không gửi/null = lấy của SOP nguồn).
                        body: { name: cloneName.trim(), crop: cloneCrop, variety_code: cloneVariety || "", anchor_name: cloneAnchor.trim() },
                      }),
                    ),
                  );
                  setDialog("");
                  onChanged(res.ref, "Đã nhân bản — bản sao là bản nháp.");
                })
              }
            >
              Nhân bản
            </button>
          }
        >
          <p className="small mute">Bản sao là SOP Nháp, giữ nguyên giai đoạn, việc, cửa sổ, vật tư và liều; sửa bản sao không ảnh hưởng SOP nguồn.</p>
          <div className="field">
            <label htmlFor="clone-name">Tên SOP mới</label>
            <input id="clone-name" className="input" value={cloneName} onChange={(e) => setCloneName(e.target.value)} />
            <FieldError message={server.name} />
          </div>
          <div className="field">
            <label htmlFor="clone-crop">Loại cây *</label>
            <select
              id="clone-crop"
              className="select"
              value={cloneCrop}
              onChange={(e) => {
                setCloneCrop(e.target.value);
                setCloneVariety("");
              }}
            >
              <option value="">Chọn loại cây</option>
              {cloneCropOptions.map((c) => (
                <option key={c.code} value={c.code}>
                  {c.name}
                </option>
              ))}
            </select>
            <FieldError message={server.crop} />
          </div>
          <div className="field">
            <label htmlFor="clone-variety">Giống (tùy chọn)</label>
            <select id="clone-variety" className="select" disabled={!cloneCrop} value={cloneVariety} onChange={(e) => setCloneVariety(e.target.value)}>
              <option value="">Chung cho cả loại cây</option>
              {cloneVarietyOptions.map((v) => (
                <option key={v.code} value={v.code}>
                  {v.name}
                </option>
              ))}
            </select>
            <FieldError message={server.variety_code} />
          </div>
          <div className="field">
            <label htmlFor="clone-anchor">Tên mốc gốc *</label>
            <input id="clone-anchor" className="input" maxLength={100} value={cloneAnchor} onChange={(e) => setCloneAnchor(e.target.value)} />
            <FieldError message={server.anchor_name} />
          </div>
          {problem ? <ProblemAlert problem={problem} /> : null}
        </Dialog>
      ) : null}
      {dialog === "delete" && sop ? (
        <ConfirmDialog
          title={`Xóa ${sop.name}`}
          confirmLabel="Xóa"
          onClose={() => setDialog("")}
          onDone={(msg) => {
            setDialog("");
            onDeleted(msg);
          }}
          run={async () => {
            await unwrap(adminApi.POST("/v1/cropcycle/sops/{ref}/delete", { params: { path: { ref: sop.ref }, header: IDEM() } }));
            return "Đã xóa SOP nháp.";
          }}
        >
          Xóa hẳn SOP nháp này? Không khôi phục được.
        </ConfirmDialog>
      ) : null}
      {dialog === "retire" && sop ? (
        <ReasonDialog
          title={`Ngừng áp dụng ${sop.name}`}
          label="Lý do (ít nhất 10 ký tự)"
          confirmText="Ngừng áp dụng"
          danger
          onClose={() => setDialog("")}
          run={async (reason, k) => {
            await unwrap(adminApi.POST("/v1/cropcycle/sops/{ref}/retire", { params: { path: { ref: sop.ref }, header: { "Idempotency-Key": k } }, body: { reason } }));
            setDialog("");
            onChanged(sop.ref, "Đã ngừng áp dụng.");
          }}
        >
          <p className="small mute">SOP không bị xóa, vẫn hiện trong thư viện với trạng thái Ngừng áp dụng; vụ đã ghim phiên bản vẫn giữ nguyên.</p>
        </ReasonDialog>
      ) : null}
    </div>
  );
}

/** Xem một phiên bản ở chế độ chỉ đọc (mọi ô bị khóa). */
function VersionDialog({ sop, versionNo, materials, onClose }: { sop: AdminCropSOP; versionNo: number; materials: CatalogItem[]; onClose: () => void }) {
  const v = sop.versions.find((x) => x.version_no === versionNo);
  const draft = useMemo(() => (v ? fromSopVersion(sop, v) : null), [sop, v]);
  if (!v || !draft) return null;
  const none = () => undefined;
  return (
    <Dialog title={`Phiên bản ${v.version_no} (chỉ đọc)`} onClose={onClose}>
      <div className="stack" style={{ gap: 10 }}>
        {draft.stages.map((s, si) => (
          <section key={si} className="stack" style={{ gap: 8 }} aria-label={`Giai đoạn ${si + 1} của phiên bản ${v.version_no}`}>
            <b>{s.name}</b>
            {s.tasks.map((t, ti) => (
              <TaskRow
                key={ti}
                n={`${si + 1}.${ti + 1}`}
                path={`steps[${flatIndex(draft.stages, si, ti)}]`}
                task={t}
                stage={s}
                editable={false}
                materials={materials}
                errOf={none}
                onChange={none}
                onMaterial={none}
                onMove={none}
                onRemove={none}
                isFirst={ti === 0}
                isLast={ti === s.tasks.length - 1}
              />
            ))}
          </section>
        ))}
      </div>
    </Dialog>
  );
}

function TaskRow({
  n,
  path,
  task,
  stage,
  editable,
  materials,
  errOf,
  onChange,
  onMaterial,
  onMove,
  onRemove,
  isFirst,
  isLast,
}: {
  n: string;
  path: string;
  task: SopTaskDraft;
  stage: SopStageDraft;
  editable: boolean;
  materials: CatalogItem[];
  errOf: (path: string) => string | undefined;
  onChange: (patch: Partial<SopTaskDraft>) => void;
  onMaterial: (k: number, patch: Partial<SopMaterialDraft>) => void;
  onMove: (delta: -1 | 1) => void;
  onRemove: () => void;
  isFirst: boolean;
  isLast: boolean;
}) {
  const w = task.window;
  const bad = (p: string) => (errOf(p) ? true : undefined);
  const setWindow = (patch: Partial<typeof w>) => onChange({ window: { ...w, ...patch } });
  const hasRange = stageHasRange(stage);
  return (
    <div className="card sec stack" style={{ gap: 8 }} role="group" aria-label={`Việc ${n}`}>
      <div className="row" style={{ alignItems: "flex-start" }}>
        <div className="field" style={{ flex: "1 1 260px" }}>
          <input className="input" aria-label={`Nội dung việc ${n}`} aria-invalid={bad(`${path}.name`)} disabled={!editable} maxLength={200} value={task.name} onChange={(e) => onChange({ name: e.target.value })} />
          <FieldError message={errOf(`${path}.name`)} />
        </div>
        <select className="select" aria-label={`Loại việc ${n}`} aria-invalid={bad(`${path}.activity_code`)} disabled={!editable} value={task.activity} onChange={(e) => onChange({ activity: e.target.value as CropActivity })}>
          {ACTIVITIES.map((a) => (
            <option key={a} value={a}>
              {ACTIVITY_LABEL[a]}
            </option>
          ))}
        </select>
        {editable ? (
          <div className="row" style={{ gap: 4, flexWrap: "nowrap" }}>
            <button type="button" className="btn btn-o btn-sm" aria-label={`Chuyển việc ${n} lên`} disabled={isFirst} onClick={() => onMove(-1)}>
              ↑
            </button>
            <button type="button" className="btn btn-o btn-sm" aria-label={`Chuyển việc ${n} xuống`} disabled={isLast} onClick={() => onMove(1)}>
              ↓
            </button>
            <button type="button" className="btn btn-o btn-sm" aria-label={`Xóa việc ${n}`} onClick={onRemove}>
              ✕
            </button>
          </div>
        ) : null}
      </div>

      <div className="row" style={{ alignItems: "flex-start" }}>
        <div className="field">
          <select className="select" aria-label={`Cửa sổ việc ${n}`} aria-invalid={bad(`${path}.window.kind`) ?? bad(`${path}.window`)} disabled={!editable} value={w.kind} onChange={(e) => setWindow({ kind: e.target.value as SopWindowKind })}>
            {WINDOW_KINDS.map((k) => (
              <option key={k} value={k} disabled={k === "stage" && !hasRange}>
                {WINDOW_KIND_LABEL[k]}
                {k === "stage" && !hasRange ? " (giai đoạn chưa có khoảng ngày)" : ""}
              </option>
            ))}
          </select>
          <FieldError message={errOf(`${path}.window.kind`) ?? errOf(`${path}.window`)} />
        </div>
        {w.kind === "date_range" || w.kind === "open" ? (
          <>
            <select className="select" aria-label={`Mốc việc ${n}`} aria-invalid={bad(`${path}.window.anchor`)} disabled={!editable} value={w.anchor} onChange={(e) => setWindow({ anchor: e.target.value as SopAnchor })}>
              {ANCHORS.map((a) => (
                <option key={a} value={a}>
                  {ANCHOR_LABEL[a]}
                </option>
              ))}
            </select>
            <div className="field">
              <input className="input grid-input" inputMode="numeric" aria-label={`Từ (ngày) việc ${n}`} aria-invalid={bad(`${path}.window.from`)} disabled={!editable} value={w.from} onChange={(e) => setWindow({ from: e.target.value })} />
              <FieldError message={errOf(`${path}.window.from`)} />
            </div>
          </>
        ) : null}
        {w.kind === "date_range" ? (
          <div className="field">
            <input className="input grid-input" inputMode="numeric" aria-label={`Đến (ngày) việc ${n}`} aria-invalid={bad(`${path}.window.to`)} disabled={!editable} value={w.to} onChange={(e) => setWindow({ to: e.target.value })} />
            <FieldError message={errOf(`${path}.window.to`)} />
          </div>
        ) : null}
        {w.kind === "stage" && hasRange ? <span className="mute small">Lấy khoảng ngày {stage.from} … {stage.to} của giai đoạn.</span> : null}
        <label className="row small" style={{ gap: 4 }}>
          <input type="checkbox" disabled={!editable} checked={task.mandatory} onChange={(e) => onChange({ mandatory: e.target.checked })} /> Bắt buộc
        </label>
        <label className="row small" style={{ gap: 4 }}>
          <input type="checkbox" aria-invalid={bad(`${path}.season_summary`)} disabled={!editable} checked={task.seasonSummary} onChange={(e) => onChange({ seasonSummary: e.target.checked })} /> Việc tổng kết vụ
        </label>
        <div className="field">
          <input className="input grid-input" inputMode="numeric" placeholder="Cách ly (ngày)" aria-label={`Cách ly việc ${n}`} aria-invalid={bad(`${path}.phi_days`)} disabled={!editable} value={task.phiDays} onChange={(e) => onChange({ phiDays: e.target.value })} />
          <FieldError message={errOf(`${path}.phi_days`)} />
        </div>
      </div>
      <FieldError message={errOf(`${path}.season_summary`)} />

      <div className="stack" style={{ gap: 6 }}>
        {task.materials.map((m, k) => {
          const g = `${path}.materials[${k}]`;
          return (
            <div key={k} className="row" style={{ alignItems: "flex-start" }} role="group" aria-label={`Dòng vật tư ${k + 1} của việc ${n}`}>
              <select
                className="select"
                aria-label={`Vật tư ${k + 1} của việc ${n}`}
                aria-invalid={bad(`${g}.material_code`)}
                disabled={!editable}
                value={m.materialCode}
                onChange={(e) => {
                  const it = materials.find((x) => x.code === e.target.value);
                  onMaterial(k, it ? { materialCode: it.code, name: it.name, unit: m.unit || it.attrs.unit || "" } : { materialCode: "" });
                }}
              >
                <option value="">— Nhập tên tự do —</option>
                {materials.map((x) => (
                  <option key={x.code} value={x.code}>
                    {x.name}
                  </option>
                ))}
              </select>
              {!m.materialCode ? (
                <div className="field">
                  <input className="input" placeholder="Tên vật tư" aria-label={`Tên vật tư ${k + 1} của việc ${n}`} aria-invalid={bad(`${g}.name`)} disabled={!editable} value={m.name} onChange={(e) => onMaterial(k, { name: e.target.value })} />
                  <FieldError message={errOf(`${g}.name`)} />
                </div>
              ) : null}
              <div className="field">
                <input
                  className="input grid-input"
                  inputMode="decimal"
                  placeholder="Liều/ha"
                  aria-label={`Liều/ha vật tư ${k + 1} của việc ${n}`}
                  aria-invalid={bad(`${g}.dose_per_ha`)}
                  disabled={!editable}
                  value={m.dose}
                  onChange={(e) => onMaterial(k, { dose: e.target.value })}
                  onBlur={() => {
                    const d = parseDecimalInput(m.dose, 4);
                    if (d !== null && d !== m.dose) onMaterial(k, { dose: d });
                  }}
                />
                <FieldError message={errOf(`${g}.dose_per_ha`)} />
              </div>
              <div className="field">
                <input className="input grid-input" placeholder="Đơn vị" aria-label={`Đơn vị vật tư ${k + 1} của việc ${n}`} aria-invalid={bad(`${g}.unit`)} disabled={!editable} value={m.unit} maxLength={20} onChange={(e) => onMaterial(k, { unit: e.target.value })} />
                <FieldError message={errOf(`${g}.unit`)} />
              </div>
              {editable ? (
                <button type="button" className="btn btn-o btn-sm" aria-label={`Xóa vật tư ${k + 1} của việc ${n}`} onClick={() => onChange({ materials: task.materials.filter((_, i) => i !== k) })}>
                  ✕
                </button>
              ) : null}
              {errOf(`${g}.material_code`) ? <FieldError message={errOf(`${g}.material_code`)} /> : null}
            </div>
          );
        })}
        {editable ? (
          <div>
            <button type="button" className="btn btn-o btn-sm" onClick={() => onChange({ materials: [...task.materials, blankMaterial()] })}>
              {`+ Thêm vật tư cho việc ${n}`}
            </button>
          </div>
        ) : null}
      </div>
    </div>
  );
}

// --------------------------------------------------------------------------------------------------------------------
// Gắn giống ↔ SOP (giữ nguyên từ màn cũ; mỗi lần gắn dùng khóa idempotency theo thao tác)
// --------------------------------------------------------------------------------------------------------------------
function Bindings({ templates, version }: { templates: CropSOPTemplate[]; version: number }) {
  const { api, adminApi, principal } = useAuth();
  const varieties = useCatalog("rice_variety");
  const action = useIdempotentAction();
  const bindings = useAsync(() => unwrap(api.GET("/v1/cropcycle/variety-bindings")), [api, version]);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const canBind = hasRole(principal, ROLES.sopAdmin, ROLES.platformAdmin);
  const published = templates.filter((t) => t.scope === "system" && t.status === "published");
  const bind = async (variety: string, template: string) => {
    setProblem(null);
    setNotice(null);
    try {
      await action.run(() => unwrap(adminApi.POST("/v1/cropcycle/variety-bindings", { params: { header: IDEM() }, body: { variety_code: variety, template_ref: template } })));
      setNotice(`Đã gắn giống ${variety}.`);
      bindings.reload();
    } catch (e) {
      setProblem(toApiProblem(e));
    }
  };
  return (
    <section className="card sec" aria-labelledby="bind-title" style={{ marginTop: 16 }}>
      <div className="sec-h">
        <h2 id="bind-title">Gắn giống lúa ↔ quy trình SOP</h2>
      </div>
      {bindings.error ? <ProblemAlert problem={bindings.error} /> : null}
      <div className="tbl-wrap">
        <table className="tbl">
          <caption className="sr-only">Gắn giống lúa với quy trình</caption>
          <thead>
            <tr>
              <th scope="col">Giống lúa</th>
              <th scope="col">Quy trình mặc định</th>
            </tr>
          </thead>
          <tbody>
            {varieties.map((v) => {
              const cur = bindings.data?.items.find((b) => b.variety_code === v.code);
              return (
                <tr key={v.code}>
                  <td>{v.name}</td>
                  <td>
                    <select className="select" aria-label={`Quy trình cho giống ${v.name}`} disabled={!canBind} value={cur?.template_ref ?? ""} onChange={(e) => e.target.value && void bind(v.code, e.target.value)}>
                      <option value="">— Chưa gắn —</option>
                      {published.map((t) => (
                        <option key={t.ref} value={t.ref}>
                          {t.name}
                        </option>
                      ))}
                    </select>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
      {notice ? <p role="status">{notice}</p> : null}
      {problem ? <ProblemAlert problem={problem} /> : null}
    </section>
  );
}

// --------------------------------------------------------------------------------------------------------------------
// Thư viện SOP + màn soạn
// --------------------------------------------------------------------------------------------------------------------
function EditorLoader({
  refId,
  version,
  onBack,
  onChanged,
  onDeleted,
}: {
  refId: string;
  version: number;
  onBack: () => void;
  onChanged: (ref: string | null, notice?: string) => void;
  onDeleted: (notice: string) => void;
}) {
  const { adminApi } = useAuth();
  const sop = useAsync(() => unwrap(adminApi.GET("/v1/cropcycle/sops/{ref}", { params: { path: { ref: refId } } })), [adminApi, refId, version]);
  if (sop.error) return <ProblemAlert problem={sop.error} onRetry={sop.reload} />;
  if (!sop.data) return <Loading />;
  // key theo phiên bản + cập nhật: nạp lại bản nháp sau mỗi lần lưu/phát hành.
  const latest = sop.data.versions.at(-1);
  return <Editor key={`${sop.data.ref}:${latest?.id}:${latest?.status}:${sop.data.updated_at}`} sop={sop.data} onBack={onBack} onChanged={onChanged} onDeleted={onDeleted} />;
}

export function SopScreen(): ReactNode {
  const { adminApi, principal } = useAuth();
  const [view, setView] = useState<{ kind: "library" } | { kind: "edit"; ref: string | null }>({ kind: "library" });
  const [version, setVersion] = useState(0);
  const [notice, setNotice] = useState<string | null>(null);
  const [cropFilter, setCropFilter] = useState("");
  const [statusFilter, setStatusFilter] = useState("");
  const [importOpen, setImportOpen] = useState(false);
  const list = useAsync(() => collectPages((cursor) => unwrap(adminApi.GET("/v1/cropcycle/sops", { params: { query: { cursor, limit: 200 } } })), 500), [adminApi, version]);
  const cropNames = useCatalogNames("crop");
  const varietyNames = useCatalogNames("rice_variety");
  const canAuthor = hasRole(principal, ROLES.sopAdmin, ROLES.platformAdmin);
  const templates = (list.data ?? []).filter((t) => (!cropFilter || t.crop === cropFilter) && (!statusFilter || t.status === statusFilter));
  const crops = useMemo(() => [...new Set((list.data ?? []).map((t) => t.crop))], [list.data]);

  const changed = (ref: string | null, msg?: string) => {
    setVersion((v) => v + 1);
    setNotice(msg ?? null);
    setView({ kind: "edit", ref });
  };

  const deleted = (msg: string) => {
    setVersion((v) => v + 1);
    setNotice(msg);
    setView({ kind: "library" });
  };

  return (
    <>
      <PageHead
        title="Quy trình SOP"
        subtitle="Quy trình kỹ thuật chuẩn theo loại cây, giống — soạn nháp, phát hành một bước; bản đã phát hành không sửa, sửa bằng phiên bản mới"
        actions={
          canAuthor && view.kind === "library" ? (
            <>
              <button type="button" className="btn btn-o btn-sm" onClick={() => setImportOpen(true)}>
                <Icon name="upload" size={15} />
                Nhập từ Excel
              </button>
              <button type="button" className="btn btn-p btn-sm" onClick={() => { setNotice(null); setView({ kind: "edit", ref: null }); }}>
                <Icon name="sop" size={15} />
                Soạn SOP
              </button>
            </>
          ) : null
        }
      />
      {importOpen ? (
        <SopImportDialog
          onClose={() => setImportOpen(false)}
          onImported={(ref) => {
            setImportOpen(false);
            changed(ref, "Đã nhập SOP Nháp từ Excel — kiểm tra rồi phát hành.");
          }}
        />
      ) : null}
      <MfaNotice />
      {notice ? <p role="status">{notice}</p> : null}
      {view.kind === "edit" ? (
        view.ref ? (
          <EditorLoader key={view.ref} refId={view.ref} version={version} onBack={() => { setNotice(null); setView({ kind: "library" }); }} onChanged={changed} onDeleted={deleted} />
        ) : (
          <Editor key="new" sop={null} onBack={() => setView({ kind: "library" })} onChanged={changed} onDeleted={deleted} />
        )
      ) : (
        <>
          {list.error ? <ProblemAlert problem={list.error} onRetry={list.reload} /> : null}
          <section className="card sec" aria-label="Thư viện SOP">
            <div className="row" style={{ marginBottom: 8 }}>
              <div className="field">
                <label htmlFor="sop-f-crop">Loại cây</label>
                <select id="sop-f-crop" className="select" value={cropFilter} onChange={(e) => setCropFilter(e.target.value)}>
                  <option value="">Tất cả</option>
                  {crops.map((c) => (
                    <option key={c} value={c}>
                      {cropNames.get(c) ?? c}
                    </option>
                  ))}
                </select>
              </div>
              <div className="field">
                <label htmlFor="sop-f-status">Trạng thái</label>
                <select id="sop-f-status" className="select" value={statusFilter} onChange={(e) => setStatusFilter(e.target.value)}>
                  <option value="">Tất cả</option>
                  {Object.entries(TPL_STATUS).map(([k, v]) => (
                    <option key={k} value={k}>
                      {v.text}
                    </option>
                  ))}
                </select>
              </div>
            </div>
            {list.loading && !list.data ? <Loading /> : null}
            <div className="tbl-wrap">
              <table className="tbl">
                <caption className="sr-only">Thư viện SOP</caption>
                <thead>
                  <tr>
                    <th scope="col">Tên SOP</th>
                    <th scope="col">Loại cây</th>
                    <th scope="col">Giống</th>
                    <th scope="col">Trạng thái</th>
                    <th scope="col">Phiên bản</th>
                  </tr>
                </thead>
                <tbody>
                  {templates.map((t) => (
                    <tr key={t.ref}>
                      <td>
                        <button type="button" className="btn btn-o btn-sm" onClick={() => { setNotice(null); setView({ kind: "edit", ref: t.ref }); }}>
                          {t.name}
                        </button>
                      </td>
                      <td>{cropNames.get(t.crop) ?? t.crop}</td>
                      <td>{t.variety_code ? (varietyNames.get(t.variety_code) ?? t.variety_code) : "Chung"}</td>
                      <td>
                        <span className={`chip ${TPL_STATUS[t.status]?.cls ?? "c-mute"}`}>{TPL_STATUS[t.status]?.text ?? t.status}</span>
                      </td>
                      <td>v{t.published_version ?? t.latest_version}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            {!list.loading && templates.length === 0 ? <Empty>Chưa có quy trình.</Empty> : null}
          </section>
          <Bindings templates={list.data ?? []} version={version} />
        </>
      )}
    </>
  );
}
