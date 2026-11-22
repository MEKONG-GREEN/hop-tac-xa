"use client";

import { useState, type FormEvent } from "react";
import { useAuth } from "@/auth/AuthProvider";
import type { AccountsUser } from "@/api/types";
import { listAllCooperatives } from "@/api/adapters/cooperatives";
import { newIdempotencyKey } from "@/api/idempotency";
import { fieldErrorMap } from "@/api/problem";
import { ProblemAlert } from "@/components/ProblemAlert";
import { FilterBar, SearchBox, type SortState } from "@/components/patterns/list";
import { clientList, ListScreen } from "@/components/patterns/listScreen";
import { PageHead, Loading } from "@/components/ui";
import { toApiProblem, useAsync } from "@/lib/useAsync";
import type { ApiProblem } from "@/api/problem";
import { NativeAccountDetail } from "./NativeAccountDetail";

import { accountsClient, type AccountInput as Input, type AccountsScope } from "./accountsClient";
const roleLabels = { platform_admin: "Admin", coop_manager: "Đại diện HTX", htx_member: "Thành viên HTX" } as const;
const name = (u: AccountsUser) => `${u.last_name} ${u.first_name}`.trim();
const state = (u: AccountsUser) => !u.enabled ? "locked" : u.required_actions.includes("UPDATE_PASSWORD") ? "pending" : "active";
const stateLabels: Record<string, string> = { locked: "Đã khóa", pending: "Chờ đổi mật khẩu", active: "Đang hoạt động" };

/** FN-07 (Admin, scope "admin") và FN-08 (Đại diện HTX, scope "coop", AH-277): chỉ tạo Thành viên HTX của HTX mình. */
export function NativeAccounts({ scope = "admin" }: { scope?: AccountsScope } = {}) {
  const { api, adminApi, principal } = useAuth();
  const coopScope = scope === "coop";
  const client = accountsClient(scope, api, adminApi);
  const list = useAsync(async () => {
    const [users, coops] = await Promise.all([client.list(), listAllCooperatives(api,{includeInactive:true})]);
    return { users, coops };
  }, [adminApi, api, scope]);
  const [tab, setTab] = useState("all");
  const [search, setSearch] = useState("");
  const [status, setStatus] = useState("");
  const [coop, setCoop] = useState("");
  const [page, setPage] = useState(1);
  const [sort, setSort] = useState<SortState | null>({ key: "username", dir: "asc" });
  const [selected, setSelected] = useState<AccountsUser | null>(null);
  const [creating, setCreating] = useState(false);
  const blank = (): Input => coopScope ? { username: "", full_name: "", phone: "", email: "", cooperative_ref: principal?.orgRef ?? null, roles: ["htx_member"] } : { username: "", full_name: "", phone: "", email: "", cooperative_ref: null, roles: ["coop_manager"] };
  const [input, setInput] = useState<Input>(blank);
  const [idem, setIdem] = useState(newIdempotencyKey);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<ApiProblem | null>(null);
  const [credential, setCredential] = useState<{ username: string; password: string | null } | null>(null);
  const coops = list.data?.coops ?? [];
  const coopName = (ref: string | null) => coops.find((c) => c.ref === ref)?.name ?? "—";
  const filtered = clientList(list.data?.users ?? [], {
    tabs: [{ id: "all", label: "Tất cả", match: () => true }, ...Object.entries(roleLabels).map(([id, label]) => ({ id, label, match: (u: AccountsUser) => (u.roles ?? []).includes(id) }))],
    tab, page, sort,
    // Phone searches are exact, including when a name contains the same digits.
    filter: (u) => (!search.trim() || (/^[0-9+]+$/.test(search.trim()) ? u.phone === search.trim() : `${u.username} ${name(u)}`.toLocaleLowerCase("vi").includes(search.trim().toLocaleLowerCase("vi")))) && (!status || state(u) === status) && (!coop || u.org_ref === coop),
    sortValue: (u, key) => key === "name" ? name(u) : key === "status" ? stateLabels[state(u)] : key === "coop" ? coopName(u.org_ref) : u.username,
  });
  const change = (patch: Partial<Input>) => { setInput((v) => ({ ...v, ...patch })); setIdem(newIdempotencyKey()); };
  const submit = async (event: FormEvent) => {
    event.preventDefault(); if (busy) return; setBusy(true); setError(null);
    try {
      const res = await client.create(input, idem);
      setCreating(false); setCredential({ username: res.user.username || input.username, password: res.temporary_password });
      setInput(blank()); setIdem(newIdempotencyKey()); list.reload();
    } catch (e) { setError(toApiProblem(e)); } finally { setBusy(false); }
  };
  const errors = error ? fieldErrorMap(error) : {};
  return <div className="stack">
    <PageHead title="Tài khoản HTX" subtitle="Một HTX cho mỗi tài khoản; Admin không gắn HTX" />
    {list.error && <ProblemAlert problem={list.error} />}
    <button className="btn primary" type="button" onClick={() => { setCreating(true); setSelected(null); setError(null); setCredential(null); }}>Tạo tài khoản</button>
    {credential && <section className="card stack" aria-label="Tài khoản vừa tạo">
      <strong>Đã tạo tài khoản {credential.username}</strong>
      {credential.password ? <><p>Mật khẩu tạm chỉ hiển thị lần này. Người dùng phải đổi mật khẩu ở lần đăng nhập đầu.</p><output aria-label="Mật khẩu tạm">{credential.password}</output></> : <p>Yêu cầu đã được xử lý trước đó. Mật khẩu tạm không được hiển thị lại.</p>}
      <button type="button" className="btn" onClick={() => setCredential(null)}>Đóng thông tin mật khẩu</button>
    </section>}
    {creating && <form className="card stack" onSubmit={(e) => void submit(e)} noValidate aria-label="Tạo tài khoản HTX">
      {error && <ProblemAlert problem={error} />}
      {([ ["username", "Tên đăng nhập"], ["full_name", "Họ tên"], ["phone", "Số điện thoại"], ["email", coopScope ? "Email" : "Email nhận OTP (bắt buộc với Admin)"] ] as const).map(([field, label]) => <label className="field" key={field}>{label}<input className="input" value={input[field] ?? ""} onChange={(e) => change({ [field]: e.target.value })} aria-invalid={!!errors[field]} autoComplete="off" />{errors[field] && <span className="err">{errors[field]}</span>}</label>)}
      {coopScope ? <p>Vai trò: Thành viên HTX · HTX: {coopName(principal?.orgRef ?? null)}</p> : <><label className="field">Vai trò<select className="select" value={input.roles[0]} onChange={(e) => { const role = e.target.value as Input["roles"][number]; change({ roles: [role], cooperative_ref: null }); }}>
        {Object.entries(roleLabels).map(([value, label]) => <option key={value} value={value}>{label}</option>)}
      </select></label>
      {input.roles[0] === "coop_manager" && <label><input type="checkbox" checked={input.roles.includes("htx_member")} onChange={(e) => change({ roles: e.target.checked ? ["coop_manager", "htx_member"] : ["coop_manager"] })} /> Đồng thời là Thành viên HTX</label>}
      {input.roles[0] !== "platform_admin" && <label className="field">HTX<select className="select" value={input.cooperative_ref ?? ""} onChange={(e) => change({ cooperative_ref: e.target.value || null })}><option value="">Chọn HTX</option>{coops.filter((c) => c.active).map((c) => <option key={c.ref} value={c.ref}>{c.name}</option>)}</select>{errors.cooperative_ref && <span className="err">{errors.cooperative_ref}</span>}</label>}</>}
      <div className="row"><button className="btn primary" disabled={busy} type="submit">{busy ? "Đang tạo…" : "Lưu tài khoản"}</button><button className="btn" disabled={busy} type="button" onClick={() => setCreating(false)}>Hủy</button></div>
    </form>}
    {selected && <NativeAccountDetail key={selected.id} scope={scope} user={selected} coops={coops} onChanged={(u) => { setSelected(u); list.reload(); }} onClose={() => setSelected(null)} />}
    {list.loading ? <Loading /> : <ListScreen caption="Tài khoản HTX" tabsLabel="Vai trò tài khoản" tabs={filtered.tabs} tab={tab} onTab={(v) => { setTab(v); setPage(1); }}
      filterBar={<FilterBar resultCount={filtered.total} activeCount={Number(!!search) + Number(!!status) + Number(!!coop)} onClear={() => { setSearch(""); setStatus(""); setCoop(""); setPage(1); }}>
        <SearchBox value={search} onChange={(v) => { setSearch(v); setPage(1); }} placeholder="Tên, tên đăng nhập hoặc đúng SĐT" />
        <select className="select" aria-label="Trạng thái tài khoản" value={status} onChange={(e) => { setStatus(e.target.value); setPage(1); }}><option value="">Mọi trạng thái</option>{Object.entries(stateLabels).map(([value, label]) => <option value={value} key={value}>{label}</option>)}</select>
        <select className="select" aria-label="Lọc HTX" value={coop} onChange={(e) => { setCoop(e.target.value); setPage(1); }}><option value="">Mọi HTX</option>{coops.map((c) => <option value={c.ref} key={c.ref}>{c.name}</option>)}</select>
      </FilterBar>}
      rows={filtered.rows} rowKey={(u) => u.id} total={filtered.total} page={filtered.page} onPage={setPage} sort={sort} onSort={setSort}
      code={{ key: "username", header: "Mã / tên đăng nhập", sortKey: "username", cell: (u) => u.username }}
      main={{ key: "name", header: "Họ tên", sortKey: "name", title: (u) => principal?.subject === u.id ? `${name(u)} (bạn)` : name(u), sub: (u) => `${u.phone ?? "—"} · ${coopName(u.org_ref)}`, open: (u) => ({ onClick: () => setSelected(u) }) }}
      columns={[{ key: "roles", header: "Vai trò", cell: (u) => (u.roles ?? []).map((r) => roleLabels[r as keyof typeof roleLabels] ?? r).join(", ") }, { key: "status", header: "Trạng thái", sortKey: "status", cell: (u) => stateLabels[state(u)] }]}
      actions={{ cell: (u) => <button type="button" className="btn" onClick={() => setSelected(u)}>Chi tiết</button> }} empty={<p>Không có bản ghi phù hợp</p>} />}
  </div>;
}
