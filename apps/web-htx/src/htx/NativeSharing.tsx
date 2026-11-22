"use client";

import { useState, type FormEvent } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { matrixAllows } from "@/auth/permissions";
import { hasRole, ROLES } from "@/auth/principal";
import { unwrap, type ApiPaths } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import type { ApiProblem } from "@/api/problem";
import { Dialog } from "@/components/Dialog";
import { ProblemAlert } from "@/components/ProblemAlert";
import { FilterBar, SearchBox, type SortState } from "@/components/patterns/list";
import { clientList, ListScreen } from "@/components/patterns/listScreen";
import { Loading, PageHead } from "@/components/ui";
import { formatDateTime } from "@/lib/format";
import { toApiProblem, useAsync } from "@/lib/useAsync";

type Sharing = ApiPaths["/v1/htx-sharing"]["get"]["responses"][200]["content"]["application/json"]["items"][number];
type Action = "approve" | "reject" | "revoke";
const states = { requested: "Gửi yêu cầu", approved: "Duyệt", rejected: "Từ chối", revoked: "Thu hồi" };
const actions = { approve: "Duyệt", reject: "Từ chối", revoke: "Thu hồi" };
const direction = (r: Sharing) => `${r.requester_name || r.requester_cooperative_ref} xem dữ liệu của ${r.owner_name || r.owner_cooperative_ref}`;

export function NativeSharing() {
  const { api, principal } = useAuth();
  // FN-09/BR-03: chỉ Đại diện HTX (vai đang hiệu lực theo ma trận); Admin, Thành viên HTX không có chức năng.
  const allowed = hasRole(principal, ROLES.coopManager) && !hasRole(principal, ROLES.platformAdmin) && matrixAllows(principal, "FN-09", "R");
  const own = principal?.orgRef;
  const list = useAsync(async () => {
    if (!allowed) return null;
    const [requests, candidates] = await Promise.all([unwrap(api.GET("/v1/htx-sharing")), unwrap(api.GET("/v1/htx-sharing/candidates"))]);
    return { requests: requests.items, candidates: candidates.items };
  }, [api, allowed]);
  const [tab, setTab] = useState("incoming");
  const [state, setState] = useState("");
  const [search, setSearch] = useState("");
  const [page, setPage] = useState(1);
  const [sort, setSort] = useState<SortState | null>({ key: "requested_at", dir: "desc" });
  const [creating, setCreating] = useState(false);
  const [target, setTarget] = useState("");
  const [requestKey, setRequestKey] = useState(newIdempotencyKey);
  const [operation, setOperation] = useState<{ row: Sharing; action: Action; key: string } | null>(null);
  const [selected, setSelected] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<ApiProblem | null>(null);
  const [validation, setValidation] = useState("");
  const rows = list.data?.requests ?? [];
  const candidates = list.data?.candidates ?? [];
  const pending = rows.filter((r) => r.owner_cooperative_ref === own && r.state === "requested").length;
  const filtered = clientList(rows, {
    tabs: [{ id: "incoming", label: `Gửi đến · ${pending} chờ duyệt`, match: (r) => r.owner_cooperative_ref === own }, { id: "outgoing", label: "Đã gửi", match: (r) => r.requester_cooperative_ref === own }],
    tab, page, sort,
    filter: (r) => (!state || r.state === state) && direction(r).toLocaleLowerCase("vi").includes(search.trim().toLocaleLowerCase("vi")),
    sortValue: (r, key) => key === "direction" ? direction(r) : r.requested_at,
  });
  const detail = rows.find((r) => r.ref === selected);
  async function submit(event: FormEvent) {
    event.preventDefault(); if (busy) return;
    if (!target) { setValidation("Bắt buộc nhập"); return; }
    setBusy(true); setError(null);
    try {
      await unwrap(api.POST("/v1/htx-sharing", { params: { header: { "Idempotency-Key": requestKey } }, body: { owner_cooperative_ref: target } }));
      setCreating(false); setTarget(""); setRequestKey(newIdempotencyKey()); setTab("outgoing"); setPage(1); list.reload();
    } catch (e) { setError(toApiProblem(e)); } finally { setBusy(false); }
  }
  async function transition() {
    if (!operation || busy) return;
    setBusy(true); setError(null);
    try {
      await unwrap(api.POST("/v1/htx-sharing/{ref}/transitions", { params: { path: { ref: operation.row.ref }, header: { "Idempotency-Key": operation.key } }, body: { action: operation.action, expected_revision: operation.row.revision } }));
      setOperation(null); list.reload();
    } catch (e) { setError(toApiProblem(e)); list.reload(); } finally { setBusy(false); }
  }
  function choose(row: Sharing, action: Action) { setError(null); setOperation({ row, action, key: newIdempotencyKey() }); }
  function controls(row: Sharing) {
    return <>{row.state === "requested" && row.owner_cooperative_ref === own ? <><button className="btn" onClick={() => choose(row, "approve")}>Duyệt</button><button className="btn" onClick={() => choose(row, "reject")}>Từ chối</button></> : null}
      {row.state === "approved" || (row.state === "requested" && row.requester_cooperative_ref === own) ? <button className="btn" onClick={() => choose(row, "revoke")}>Thu hồi</button> : null}</>;
  }
  if (!allowed) return null;
  return <div className="stack">
    <PageHead title="Liên kết & chia sẻ dữ liệu" subtitle="Toàn bộ dữ liệu (chỉ xem). Mỗi chiều chia sẻ được quyết định độc lập." />
    {list.error && <ProblemAlert problem={list.error} />}
    <div className="row"><button type="button" className="btn btn-p" onClick={() => { setCreating(true); setValidation(""); setError(null); }}>Gửi yêu cầu</button><button type="button" className="btn" onClick={list.reload}>Tải lại</button></div>
    {!list.loading && !list.error && !candidates.length ? <p>Chưa có HTX để gửi yêu cầu</p> : null}
    {creating && <Dialog title="Gửi yêu cầu chia sẻ" onClose={() => { if (!busy) setCreating(false); }}><form onSubmit={(e) => void submit(e)} noValidate className="stack">
      <label className="field">HTX được yêu cầu<select className="select" value={target} disabled={busy} onChange={(e) => { setTarget(e.target.value); setRequestKey(newIdempotencyKey()); setValidation(""); }}><option value="">Chọn HTX</option>{candidates.map((c) => <option key={c.ref} value={c.ref}>{c.code} · {c.name}</option>)}</select></label>
      <p>HTX của bạn yêu cầu xem dữ liệu của HTX được chọn. Toàn bộ dữ liệu (chỉ xem).</p>
      {validation && <p role="alert">{validation}</p>}{error && <ProblemAlert problem={error} />}
      <button type="submit" className="btn btn-p" disabled={busy || !candidates.length}>Gửi</button>
    </form></Dialog>}
    {operation && <Dialog title={`${actions[operation.action]} yêu cầu`} onClose={() => { if (!busy) setOperation(null); }}>
      <p>{direction(operation.row)}</p><p>{operation.action === "approve" ? "Sau khi duyệt, HTX yêu cầu được xem toàn bộ dữ liệu ở chế độ chỉ xem." : operation.action === "revoke" ? "Thu hồi sẽ ngừng quyền xem ngay. Lịch sử yêu cầu vẫn được giữ." : "Từ chối không cấp quyền xem dữ liệu."}</p>
      {error && <ProblemAlert problem={error} />}<button type="button" className="btn btn-p" disabled={busy} onClick={() => void transition()}>Xác nhận {actions[operation.action].toLowerCase()}</button>
    </Dialog>}
    {detail && <Dialog title="Chi tiết yêu cầu" onClose={() => setSelected(null)}><p>{direction(detail)}</p><p>Toàn bộ dữ liệu (chỉ xem) · {states[detail.state]}</p><p>Ngày gửi: {formatDateTime(detail.requested_at)}</p><p>Ngày xử lý: {formatDateTime(detail.processed_at)}</p><p>Mã người xử lý: {detail.actor_ref}</p></Dialog>}
    {list.loading && !list.data ? <Loading /> : <ListScreen caption="Yêu cầu chia sẻ" tabsLabel="Chiều yêu cầu" tabs={filtered.tabs} tab={tab} onTab={(v) => { setTab(v); setPage(1); }}
      filterBar={<FilterBar resultCount={filtered.total} activeCount={Number(!!search) + Number(!!state)} onClear={() => { setSearch(""); setState(""); setPage(1); }}><SearchBox value={search} onChange={(v) => { setSearch(v); setPage(1); }} placeholder="Tìm HTX" /><select className="select" aria-label="Trạng thái chia sẻ" value={state} onChange={(e) => { setState(e.target.value); setPage(1); }}><option value="">Mọi trạng thái</option>{Object.entries(states).map(([id, label]) => <option key={id} value={id}>{label}</option>)}</select></FilterBar>}
      rows={filtered.rows} rowKey={(r) => r.ref} total={filtered.total} page={filtered.page} onPage={setPage} sort={sort} onSort={setSort}
      code={{ key: "ref", header: "Mã", cell: (r) => r.ref.slice(-8) }} main={{ key: "direction", header: "Chiều chia sẻ", sortKey: "direction", title: direction, sub: () => "Toàn bộ dữ liệu (chỉ xem)", open: (r) => ({ onClick: () => setSelected(r.ref) }) }}
      columns={[{ key: "state", header: "Trạng thái", cell: (r) => states[r.state] }, { key: "requested_at", header: "Ngày gửi", sortKey: "requested_at", cell: (r) => formatDateTime(r.requested_at) }]} actions={{ cell: controls }} empty={<p>Chưa có yêu cầu phù hợp</p>} />}
  </div>;
}
