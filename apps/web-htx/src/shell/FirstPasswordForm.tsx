"use client";

import { useId, useState, type FormEvent } from "react";
import { Icon } from "@/components/Icon";

/** Câu thông báo tại trường của bước đổi mật khẩu lần đầu (AH-269, FN-01/EX-05, AC-N1, AC-N2) — khớp máy chủ. */
export const FIRST_PASSWORD_MESSAGES = {
  short: "Mật khẩu tối thiểu 06 ký tự",
  mismatch: "Mật khẩu nhập lại không khớp",
  same: "Mật khẩu mới phải khác mật khẩu tạm",
} as const;

export interface FirstPasswordFormProps {
  /** Mật khẩu tạm vừa nhập (chỉ để so khác, không hiển thị, không lưu). */
  temporaryPassword: string;
  busy?: boolean;
  /** Lỗi tại trường do máy chủ trả (new_password, confirm_password). */
  fieldErrors?: Record<string, string>;
  error?: string | null;
  onSubmit: (newPassword: string, confirmPassword: string) => void;
  onCancel: () => void;
}

/** Khung "Đổi mật khẩu lần đầu" (FN-01/AC-04): bắt buộc trước khi vào ứng dụng; đổi thành công vào thẳng trang chủ. */
export function FirstPasswordForm({ temporaryPassword, busy, fieldErrors = {}, error, onSubmit, onCancel }: FirstPasswordFormProps) {
  const id = useId();
  const [next, setNext] = useState("");
  const [confirm, setConfirm] = useState("");
  const [touched, setTouched] = useState(false);
  const local: Record<string, string> = {};
  if ([...next].length < 6) local.new_password = FIRST_PASSWORD_MESSAGES.short;
  else if (next === temporaryPassword) local.new_password = FIRST_PASSWORD_MESSAGES.same;
  if (confirm !== next) local.confirm_password = FIRST_PASSWORD_MESSAGES.mismatch;
  const shown = touched ? { ...fieldErrors, ...local } : fieldErrors;

  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (busy) return;
    setTouched(true);
    if (Object.keys(local).length) return;
    onSubmit(next, confirm);
  };

  const field = (name: "new_password" | "confirm_password", label: string, value: string, set: (v: string) => void) => (
    <div className="field">
      <label htmlFor={`${id}-${name}`} className="req">{label}</label>
      <input
        id={`${id}-${name}`}
        className="input"
        type="password"
        autoComplete="new-password"
        value={value}
        onChange={(e) => set(e.target.value)}
        aria-invalid={!!shown[name] || undefined}
        aria-describedby={shown[name] ? `${id}-${name}-e` : undefined}
      />
      {shown[name] ? <span className="err" id={`${id}-${name}-e`}>{shown[name]}</span> : null}
    </div>
  );

  return (
    <form className="login-card" onSubmit={submit} noValidate aria-labelledby={`${id}-h`}>
      <h1 id={`${id}-h`}>Đổi mật khẩu lần đầu</h1>
      <p className="sub">Tài khoản mới phải đổi mật khẩu tạm trước khi vào ứng dụng. Mật khẩu mới tối thiểu 06 ký tự, khác mật khẩu tạm.</p>
      {field("new_password", "Mật khẩu mới", next, setNext)}
      {field("confirm_password", "Nhập lại mật khẩu mới", confirm, setConfirm)}
      <button type="submit" className="btn btn-p btn-block" disabled={busy}>
        {busy ? "Đang lưu…" : "Đổi mật khẩu"}
      </button>
      <div className="login-opts">
        <button type="button" className="link-btn" onClick={onCancel} disabled={busy}>Quay lại</button>
      </div>
      {error ? (
        <div className="login-error" role="alert">
          <Icon name="xCircle" />
          <span>{error}</span>
        </div>
      ) : null}
    </form>
  );
}
