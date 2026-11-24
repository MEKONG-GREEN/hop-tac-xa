"use client";

import { useState, type FormEvent } from "react";
import { newIdempotencyKey } from "@/api/idempotency";
import { fieldErrorMap, type ApiProblem } from "@/api/problem";
import type { AccountsUser, Cooperative } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { Dialog } from "@/components/Dialog";
import { ProblemAlert } from "@/components/ProblemAlert";
import { toApiProblem, useAsync } from "@/lib/useAsync";

import { accountsClient, type AccountRevision as Revision, type AccountsScope, type PasswordReset } from "./accountsClient";
const ROLE_LABELS: Record<string, string> = { platform_admin: "Admin", coop_manager: "Đại diện HTX", htx_member: "Thành viên HTX" };
const fullName = (u: AccountsUser) => `${u.last_name} ${u.first_name}`.trim();
const LAST_REPRESENTATIVE = "accounts.last_representative";

/**
 * AH-276 (FN-07): chi tiết, sửa, khóa / mở khóa tài khoản của Admin. Tên đăng nhập và vai chính chỉ đọc (AC-06);
 * dòng của chính người đang đăng nhập có "(bạn)" và không có nút khóa (AC-08, BR-07); đổi HTX thì người dùng phải
 * đăng nhập lại (QTPQ-Q-22).
 */
export function NativeAccountDetail({ user, coops, onChanged, onClose, scope = "admin" }: { user: AccountsUser; coops: Cooperative[]; onChanged: (u: AccountsUser) => void; onClose: () => void; scope?: AccountsScope }) {
  const { api, adminApi, principal } = useAuth();
  const client = accountsClient(scope, api, adminApi);
  const self = principal?.subject === user.id;
  // FN-08 (AH-277, QTPQ-Q-28): Đại diện HTX trên dòng của mình chỉ sửa ô "Kiêm Thành viên HTX"; không đổi HTX của ai.
  const profileLocked = scope === "coop" && self;
  const roles = user.roles ?? [];
  const main = roles[0] ?? "";
  const [editing, setEditing] = useState(false);
  // FN-TK-02/BR-04 (AH-223): danh sách chỉ có SĐT đã che; chi tiết lấy SĐT đầy đủ (máy chủ ghi nhật ký truy cập).
  const detail = useAsync(() => client.get(user.id), [user.id, scope]);
  const full = detail.data?.id === user.id ? { ...user, phone: detail.data.phone } : null;
  const toForm = (u: AccountsUser): Revision => ({ full_name: fullName(u), phone: u.phone ?? "", email: u.email ?? "", cooperative_ref: u.org_ref, also_member: (u.roles ?? []).includes("htx_member") });
  const [form, setForm] = useState<Revision>(() => toForm(user));
  const [editKey, setEditKey] = useState(newIdempotencyKey);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [notice, setNotice] = useState("");
  const [confirmReset, setConfirmReset] = useState(false);
  const [reset, setReset] = useState<PasswordReset | null>(null);
  // FN-TK-02/EX-04 (AH-223): máy chủ báo thao tác làm HTX không còn Đại diện HTX đang hoạt động → hỏi xác nhận.
  const [confirmLast, setConfirmLast] = useState<"lock" | "save" | null>(null);
  const change = (patch: Partial<Revision>) => { setForm((f) => ({ ...f, ...patch })); setEditKey(newIdempotencyKey()); };
  const errors = problem ? fieldErrorMap(problem) : {};
  const coopName = coops.find((c) => c.ref === user.org_ref)?.name ?? "—";

  // 409 accounts.last_representative → hộp xác nhận thay vì báo lỗi; bấm Tiếp tục gửi lại với khóa mới.
  const failed = (err: unknown, retry: "lock" | "save") => {
    const p = toApiProblem(err);
    if (p.code === LAST_REPRESENTATIVE) setConfirmLast(retry);
    else setProblem(p);
  };
  const save = async (e?: FormEvent, confirmed = false) => {
    e?.preventDefault(); if (busy) return;
    setBusy(true); setProblem(null); setNotice(""); setConfirmLast(null);
    try {
      const body: Revision = { full_name: form.full_name, phone: form.phone, email: form.email || undefined, cooperative_ref: main === "platform_admin" ? null : form.cooperative_ref };
      if (main === "coop_manager") body.also_member = form.also_member;
      // Lượt xác nhận là thao tác mới của người dùng: khóa Idempotency-Key mới.
      const res = await client.update(user.id, body, confirmed ? newIdempotencyKey() : editKey, confirmed);
      setEditing(false); setEditKey(newIdempotencyKey());
      setNotice(res.user.org_ref !== user.org_ref ? "Đã lưu. Tài khoản phải đăng nhập lại và chỉ thấy dữ liệu HTX mới." : "Đã lưu.");
      onChanged(res.user);
    } catch (err) { failed(err, "save"); } finally { setBusy(false); }
  };
  const setLocked = async (lock: boolean, confirmed = false) => {
    if (busy) return;
    setBusy(true); setProblem(null); setNotice(""); setConfirmLast(null);
    try {
      const res = await client.setLocked(user.id, lock, newIdempotencyKey(), confirmed);
      setNotice(lock ? "Đã khóa. Tài khoản không đăng nhập được." : "Đã mở khóa. Tài khoản đăng nhập lại được.");
      onChanged(res.user);
    } catch (err) { failed(err, "lock"); } finally { setBusy(false); }
  };

  // AH-281 (FN-10/AC-05): không cho chính mình, không cho tài khoản đã khóa; mật khẩu tạm chỉ hiển thị một lần.
  const resetPassword = async () => {
    if (busy) return;
    setConfirmReset(false); setBusy(true); setProblem(null); setNotice(""); setReset(null);
    try {
      setReset(await client.resetPassword(user.id, newIdempotencyKey()));
    } catch (err) { setProblem(toApiProblem(err)); } finally { setBusy(false); }
  };

  return (
    <section className="card stack" aria-label="Chi tiết tài khoản">
      <strong>{fullName(user)}{self ? " (bạn)" : ""}</strong>
      <p>{user.username} · {(full ?? user).phone ?? "—"} · {coopName}</p>
      {detail.error ? <ProblemAlert problem={detail.error} /> : null}
      <p>{roles.map((r) => ROLE_LABELS[r] ?? r).join(", ")} · {user.enabled ? "Đang hoạt động" : "Đã khóa"}</p>
      {notice ? <p role="status">{notice}</p> : null}
      {reset ? (
        <div role="status" className="stack">
          {reset.mode === "otp_sent" ? (
            <p>Đã gửi mã OTP tới số {reset.phone_hint}. Người dùng nhập mã ở màn Quên mật khẩu để đặt mật khẩu mới.</p>
          ) : reset.temporary_password ? (
            <>
              <p>Mật khẩu tạm (chỉ hiển thị một lần): <code aria-label="Mật khẩu tạm">{reset.temporary_password}</code></p>
              <p>Mật khẩu cũ đã hết hiệu lực. Người dùng phải đổi mật khẩu ở lần đăng nhập kế tiếp.</p>
            </>
          ) : (
            <p>Đã đặt lại mật khẩu. Mật khẩu tạm chỉ hiển thị ở lần bấm trước.</p>
          )}
        </div>
      ) : null}
      {problem && !editing ? <ProblemAlert problem={problem} /> : null}
      {editing ? (
        <form className="stack" onSubmit={(e) => void save(e)} noValidate aria-label="Sửa tài khoản HTX">
          {problem ? <ProblemAlert problem={problem} /> : null}
          <label className="field">Tên đăng nhập<input className="input" value={user.username} readOnly disabled /></label>
          <label className="field">Vai trò<input className="input" value={ROLE_LABELS[main] ?? main} readOnly disabled /></label>
          {(([["full_name", "Họ tên"], ["phone", "Số điện thoại"], ["email", main === "platform_admin" ? "Email nhận OTP" : "Email"]]) as const).map(([field, label]) => (
            <label className="field" key={field}>{label}
              <input className="input" value={form[field] ?? ""} disabled={profileLocked} onChange={(e) => change({ [field]: e.target.value })} aria-invalid={!!errors[field]} autoComplete="off" />
              {errors[field] ? <span className="err">{errors[field]}</span> : null}
            </label>
          ))}
          {main !== "platform_admin" ? (
            <label className="field">HTX
              <select className="select" value={form.cooperative_ref ?? ""} disabled={self || scope === "coop"} onChange={(e) => change({ cooperative_ref: e.target.value || null })}>
                <option value="">Chọn HTX</option>
                {coops.filter((c) => c.active || c.ref === user.org_ref).map((c) => <option key={c.ref} value={c.ref}>{c.name}</option>)}
              </select>
              {errors.cooperative_ref ? <span className="err">{errors.cooperative_ref}</span> : null}
            </label>
          ) : null}
          {main === "coop_manager" ? (
            <label><input type="checkbox" checked={!!form.also_member} disabled={self && scope !== "coop"} onChange={(e) => change({ also_member: e.target.checked })} /> Kiêm Thành viên HTX</label>
          ) : null}
          <div className="row">
            <button className="btn btn-p" type="submit" disabled={busy}>{busy ? "Đang lưu…" : "Lưu"}</button>
            <button className="btn" type="button" disabled={busy} onClick={() => { setEditing(false); setProblem(null); }}>Hủy</button>
          </div>
        </form>
      ) : (
        <div className="row">
          <button type="button" className="btn" disabled={!full} onClick={() => { if (full) setForm(toForm(full)); setEditing(true); setProblem(null); setNotice(""); }}>Sửa</button>
          {!self ? (
            user.enabled
              ? <button type="button" className="btn" disabled={busy} onClick={() => void setLocked(true)}>Khóa</button>
              : <button type="button" className="btn" disabled={busy} onClick={() => void setLocked(false)}>Mở khóa</button>
          ) : null}
          {!self && user.enabled ? <button type="button" className="btn" disabled={busy} onClick={() => setConfirmReset(true)}>Đặt lại mật khẩu</button> : null}
          <button type="button" className="btn" onClick={onClose}>Đóng</button>
        </div>
      )}
      {confirmReset ? (
        <Dialog
          title="Đặt lại mật khẩu"
          onClose={() => setConfirmReset(false)}
          actions={<>
            <button type="button" className="btn" onClick={() => setConfirmReset(false)}>Hủy</button>
            <button type="button" className="btn btn-p" onClick={() => void resetPassword()}>Đặt lại</button>
          </>}
        >
          <p>Đặt lại mật khẩu cho {fullName(user)}? Mật khẩu hiện tại sẽ hết hiệu lực và các phiên đăng nhập bị thu hồi.</p>
        </Dialog>
      ) : null}
      {confirmLast ? (
        <Dialog
          title={confirmLast === "lock" ? "Khóa Đại diện HTX cuối cùng" : "Chuyển Đại diện HTX cuối cùng"}
          onClose={() => setConfirmLast(null)}
          actions={<>
            <button type="button" className="btn" onClick={() => setConfirmLast(null)}>Hủy</button>
            <button type="button" className="btn btn-p" onClick={() => void (confirmLast === "lock" ? setLocked(true, true) : save(undefined, true))}>Tiếp tục</button>
          </>}
        >
          <p>HTX sẽ không còn người duyệt kết quả lô. Vẫn tiếp tục?</p>
        </Dialog>
      ) : null}
    </section>
  );
}
