"use client";

import { useState, type FormEvent } from "react";
import { unwrap, type AdminPaths } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { fieldErrorMap, type ApiProblem } from "@/api/problem";
import type { AccountsUser, Cooperative } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { ProblemAlert } from "@/components/ProblemAlert";
import { toApiProblem } from "@/lib/useAsync";

type Revision = AdminPaths["/v1/accounts/htx-users/{id}/revisions"]["post"]["requestBody"]["content"]["application/json"];
const ROLE_LABELS: Record<string, string> = { platform_admin: "Admin", coop_manager: "Đại diện HTX", htx_member: "Thành viên HTX" };
const fullName = (u: AccountsUser) => `${u.last_name} ${u.first_name}`.trim();

/**
 * AH-276 (FN-07): chi tiết, sửa, khóa / mở khóa tài khoản của Admin. Tên đăng nhập và vai chính chỉ đọc (AC-06);
 * dòng của chính người đang đăng nhập có "(bạn)" và không có nút khóa (AC-08, BR-07); đổi HTX thì người dùng phải
 * đăng nhập lại (QTPQ-Q-22).
 */
export function NativeAccountDetail({ user, coops, onChanged, onClose }: { user: AccountsUser; coops: Cooperative[]; onChanged: (u: AccountsUser) => void; onClose: () => void }) {
  const { adminApi, principal } = useAuth();
  const self = principal?.subject === user.id;
  const roles = user.roles ?? [];
  const main = roles[0] ?? "";
  const [editing, setEditing] = useState(false);
  const [form, setForm] = useState<Revision>({ full_name: fullName(user), phone: user.phone ?? "", email: user.email ?? "", cooperative_ref: user.org_ref, also_member: roles.includes("htx_member") });
  const [editKey, setEditKey] = useState(newIdempotencyKey);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [notice, setNotice] = useState("");
  const change = (patch: Partial<Revision>) => { setForm((f) => ({ ...f, ...patch })); setEditKey(newIdempotencyKey()); };
  const errors = problem ? fieldErrorMap(problem) : {};
  const coopName = coops.find((c) => c.ref === user.org_ref)?.name ?? "—";

  const save = async (e: FormEvent) => {
    e.preventDefault(); if (busy) return;
    setBusy(true); setProblem(null); setNotice("");
    try {
      const body: Revision = { full_name: form.full_name, phone: form.phone, email: form.email || undefined, cooperative_ref: main === "platform_admin" ? null : form.cooperative_ref };
      if (main === "coop_manager") body.also_member = form.also_member;
      const res = await unwrap(adminApi.POST("/v1/accounts/htx-users/{id}/revisions", { params: { path: { id: user.id }, header: { "Idempotency-Key": editKey } }, body }));
      setEditing(false); setEditKey(newIdempotencyKey());
      setNotice(res.user.org_ref !== user.org_ref ? "Đã lưu. Tài khoản phải đăng nhập lại và chỉ thấy dữ liệu HTX mới." : "Đã lưu.");
      onChanged(res.user);
    } catch (err) { setProblem(toApiProblem(err)); } finally { setBusy(false); }
  };
  const setLocked = async (lock: boolean) => {
    if (busy) return;
    setBusy(true); setProblem(null); setNotice("");
    try {
      const path = lock ? "/v1/accounts/htx-users/{id}/lock" : "/v1/accounts/htx-users/{id}/unlock";
      const res = await unwrap(adminApi.POST(path, { params: { path: { id: user.id }, header: { "Idempotency-Key": newIdempotencyKey() } } }));
      setNotice(lock ? "Đã khóa. Tài khoản không đăng nhập được." : "Đã mở khóa. Tài khoản đăng nhập lại được.");
      onChanged(res.user);
    } catch (err) { setProblem(toApiProblem(err)); } finally { setBusy(false); }
  };

  return (
    <section className="card stack" aria-label="Chi tiết tài khoản">
      <strong>{fullName(user)}{self ? " (bạn)" : ""}</strong>
      <p>{user.username} · {user.phone ?? "—"} · {coopName}</p>
      <p>{roles.map((r) => ROLE_LABELS[r] ?? r).join(", ")} · {user.enabled ? "Đang hoạt động" : "Đã khóa"}</p>
      {notice ? <p role="status">{notice}</p> : null}
      {problem && !editing ? <ProblemAlert problem={problem} /> : null}
      {editing ? (
        <form className="stack" onSubmit={(e) => void save(e)} noValidate aria-label="Sửa tài khoản HTX">
          {problem ? <ProblemAlert problem={problem} /> : null}
          <label className="field">Tên đăng nhập<input className="input" value={user.username} readOnly disabled /></label>
          <label className="field">Vai trò<input className="input" value={ROLE_LABELS[main] ?? main} readOnly disabled /></label>
          {(([["full_name", "Họ tên"], ["phone", "Số điện thoại"], ["email", main === "platform_admin" ? "Email nhận OTP" : "Email"]]) as const).map(([field, label]) => (
            <label className="field" key={field}>{label}
              <input className="input" value={form[field] ?? ""} onChange={(e) => change({ [field]: e.target.value })} aria-invalid={!!errors[field]} autoComplete="off" />
              {errors[field] ? <span className="err">{errors[field]}</span> : null}
            </label>
          ))}
          {main !== "platform_admin" ? (
            <label className="field">HTX
              <select className="select" value={form.cooperative_ref ?? ""} disabled={self} onChange={(e) => change({ cooperative_ref: e.target.value || null })}>
                <option value="">Chọn HTX</option>
                {coops.filter((c) => c.active || c.ref === user.org_ref).map((c) => <option key={c.ref} value={c.ref}>{c.name}</option>)}
              </select>
              {errors.cooperative_ref ? <span className="err">{errors.cooperative_ref}</span> : null}
            </label>
          ) : null}
          {main === "coop_manager" ? (
            <label><input type="checkbox" checked={!!form.also_member} disabled={self} onChange={(e) => change({ also_member: e.target.checked })} /> Kiêm Thành viên HTX</label>
          ) : null}
          <div className="row">
            <button className="btn btn-p" type="submit" disabled={busy}>{busy ? "Đang lưu…" : "Lưu"}</button>
            <button className="btn" type="button" disabled={busy} onClick={() => { setEditing(false); setProblem(null); }}>Hủy</button>
          </div>
        </form>
      ) : (
        <div className="row">
          <button type="button" className="btn" onClick={() => { setEditing(true); setProblem(null); setNotice(""); }}>Sửa</button>
          {!self ? (
            user.enabled
              ? <button type="button" className="btn" disabled={busy} onClick={() => void setLocked(true)}>Khóa</button>
              : <button type="button" className="btn" disabled={busy} onClick={() => void setLocked(false)}>Mở khóa</button>
          ) : null}
          <button type="button" className="btn" onClick={onClose}>Đóng</button>
        </div>
      )}
    </section>
  );
}
