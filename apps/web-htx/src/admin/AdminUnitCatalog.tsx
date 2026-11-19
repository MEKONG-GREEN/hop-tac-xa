"use client";

import { useState, type FormEvent } from "react";
import { unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import type { ApiProblem } from "@/api/problem";
import { useAuth } from "@/auth/AuthProvider";
import { hasRole, ROLES } from "@/auth/principal";
import { Dialog } from "@/components/Dialog";
import { MfaNotice } from "@/components/MfaNotice";
import { ProblemAlert } from "@/components/ProblemAlert";
import { FilterBar, SearchBox, type SortState } from "@/components/patterns/list";
import { ListScreen, clientList, type ClientTab } from "@/components/patterns/listScreen";
import { Loading, PageHead } from "@/components/ui";
import { normalizeVi } from "@/cgh/pins";
import { toApiProblem, useAsync, useRequestKey } from "@/lib/useAsync";
import { listManagedUnits, type ManagedUnit } from "./adminUnits";
import { AdminUnitImport } from "./AdminUnitImport";

const tabs: ClientTab<ManagedUnit>[] = [
  { id: "all", label: "Tất cả", match: () => true },
  { id: "active", label: "Đang sử dụng", match: (u) => u.active },
  { id: "inactive", label: "Ngừng sử dụng", match: (u) => !u.active },
];

function UnitForm({ parent, item, onClose, onSaved }: { parent: ManagedUnit | null; item?: ManagedUnit; onClose: () => void; onSaved: () => void }) {
  const { adminApi } = useAuth();
  const [name, setName] = useState(item?.name ?? "");
  const [active, setActive] = useState(item?.active ?? true);
  const [key, setKey] = useState(newIdempotencyKey);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<ApiProblem | null>(null);
  function changed() { setKey(newIdempotencyKey()); setError(null); }
  async function submit(e: FormEvent) {
    e.preventDefault(); if (busy) return; setBusy(true); setError(null);
    try {
      if (item) await unwrap(adminApi.POST("/v1/masterdata/admin-unit-profiles/{code}/revisions", { params: { path: { code: item.code }, header: { "Idempotency-Key": key } }, body: { name, active, expected_revision: item.revision } }));
      else await unwrap(adminApi.POST("/v1/masterdata/admin-unit-profiles", { params: { header: { "Idempotency-Key": key } }, body: { name, active, level: parent ? "commune" : "province", parent_code: parent?.code ?? "" } }));
      onSaved();
    } catch (e) { setError(toApiProblem(e)); } finally { setBusy(false); }
  }
  return <Dialog title={item ? "Sửa đơn vị hành chính" : parent ? "Thêm xã / phường / đặc khu" : "Thêm tỉnh / thành phố"} onClose={() => { if (!busy) onClose(); }}>
    <form onSubmit={submit} className="stack">
      {parent ? <p>Tỉnh / thành phố: <b>{parent.name}</b></p> : null}
      {item ? <p>Mã: {item.code}</p> : <p>Mã do hệ thống tự sinh.</p>}
      <fieldset disabled={busy} className="stack">
        <label className="field">Tên<input className="input" required maxLength={200} value={name} onChange={(e) => { setName(e.target.value); changed(); }} /></label>
        <label className="row"><input type="checkbox" checked={active} onChange={(e) => { setActive(e.target.checked); changed(); }} />Đang sử dụng</label>
        <p className="small">Ngừng sử dụng sẽ ẩn khỏi ô chọn khi tạo mới. Hồ sơ đã có vẫn giữ tên đơn vị. Không có thao tác xóa.</p>
        <button className="btn btn-p" type="submit">{busy ? "Đang lưu…" : "Lưu"}</button>
      </fieldset>
      {error ? <ProblemAlert problem={error} /> : null}
    </form>
  </Dialog>;
}

function UnitLevel({ parent, onBack, onProvince }: { parent: ManagedUnit | null; onBack: () => void; onProvince: (u: ManagedUnit) => void }) {
  const { adminApi } = useAuth();
  const data = useAsync(() => listManagedUnits(adminApi, parent?.code ?? ""), [adminApi, parent?.code]);
  const [query, setQuery] = useState(""); const [tab, setTab] = useState("all");
  const [page, setPage] = useState(1); const [sort, setSort] = useState<SortState | null>(null);
  const [editing, setEditing] = useState<ManagedUnit | "new" | null>(null); const [importing, setImporting] = useState(false);
  const term = normalizeVi(query);
  const list = clientList(data.data ?? [], { tabs, tab, page, sort, filter: (u) => !term || normalizeVi(`${u.code} ${u.name}`).includes(term), sortValue: (u, key) => key === "count" ? (parent ? u.cooperative_count ?? -1 : u.commune_count) : key === "active" ? Number(u.active) : key === "name" ? u.name : u.code });
  function reset<T>(set: (v: T) => void) { return (v: T) => { set(v); setPage(1); }; }
  function saved() { setEditing(null); data.reload(); }
  return <>
    <PageHead title={parent ? `Xã / phường · ${parent.name}` : "Đơn vị hành chính"} subtitle="Danh mục tỉnh – xã dùng chung cho hồ sơ HTX và nông hộ" actions={<div className="row">
      {parent ? <button className="btn btn-o" onClick={onBack}>Danh sách tỉnh</button> : null}
      <button className="btn btn-o" onClick={data.reload}>Tải lại</button>
      <button className="btn btn-o" onClick={() => setImporting(true)}>Nhập từ Excel</button>
      <button className="btn btn-p" disabled={parent?.active === false} onClick={() => setEditing("new")}>{parent ? "Thêm xã / phường" : "Thêm tỉnh / thành phố"}</button>
    </div>} />
    <MfaNotice />
    {parent?.active === false ? <p role="status">Tỉnh đã ngừng sử dụng. Các xã thuộc tỉnh không được chọn khi tạo hồ sơ mới.</p> : null}
    {data.loading ? <Loading /> : data.error ? <ProblemAlert problem={data.error} onRetry={data.reload} /> : <ListScreen
      caption={parent ? "Danh sách xã / phường / đặc khu" : "Danh sách tỉnh / thành phố"}
      tabsLabel="Trạng thái đơn vị hành chính" tabs={list.tabs} tab={tab} onTab={reset(setTab)}
      filterBar={<FilterBar resultCount={list.total} activeCount={query.trim() ? 1 : 0} onClear={() => { setQuery(""); setPage(1); }}><SearchBox value={query} onChange={reset(setQuery)} placeholder="Tìm theo mã hoặc tên" /></FilterBar>}
      code={{ key: "code", header: "Mã", sortKey: "code", cell: (u) => u.code }}
      main={{ key: "name", header: "Tên", sortKey: "name", title: (u) => u.name, open: (u) => ({ onClick: () => parent ? setEditing(u) : onProvince(u) }) }}
      columns={[{ key: "count", header: parent ? "Số HTX" : "Số xã / phường", sortKey: "count", className: "num", cell: (u) => parent ? u.cooperative_count ?? "—" : u.commune_count }, { key: "active", header: "Trạng thái", sortKey: "active", cell: (u) => u.active ? "Đang sử dụng" : "Ngừng sử dụng" }]}
      actions={{ cell: (u) => <button className="btn btn-o btn-sm" onClick={() => setEditing(u)}>Sửa</button> }}
      rows={list.rows} rowKey={(u) => u.code} total={list.total} page={list.page} onPage={setPage} sort={sort} onSort={reset(setSort)} empty="Chưa có đơn vị hành chính khớp bộ lọc."
    />}
    {editing ? <UnitForm parent={parent} item={editing === "new" ? undefined : editing} onClose={() => setEditing(null)} onSaved={saved} /> : null}
    {importing ? <AdminUnitImport onClose={() => setImporting(false)} onImported={data.reload} /> : null}
  </>;
}

function UnitWorkspace() {
  const [parent, setParent] = useState<ManagedUnit | null>(null);
  return <UnitLevel key={parent?.code ?? "provinces"} parent={parent} onBack={() => setParent(null)} onProvince={setParent} />;
}

export function AdminUnitCatalog() {
  const { principal } = useAuth(); const session = useRequestKey([principal]);
  if (!hasRole(principal, ROLES.platformAdmin)) return <p role="alert">Chỉ Admin được quản lý đơn vị hành chính.</p>;
  return <UnitWorkspace key={session.value} />;
}
