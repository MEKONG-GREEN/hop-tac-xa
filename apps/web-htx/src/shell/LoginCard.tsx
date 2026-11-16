"use client";

import Link from "next/link";
import { useEffect, useId, useRef, useState, type FormEvent } from "react";
import { Dialog } from "@/components/Dialog";
import { Icon } from "@/components/Icon";
import { config, type SupportInfo } from "@/config";
import { SupportContact } from "./SupportContact";

export type LoginError =
  | { kind: "invalid"; remaining: number }
  | { kind: "locked"; minutes: number }
  /** EX-03: tài khoản đã bị khóa (FN-07). */
  | { kind: "account_locked" }
  /** Còn việc lần đầu (đổi mật khẩu tạm, đăng ký OTP): hoàn tất ở trang xác thực. */
  | { kind: "setup_required" }
  | { kind: "message"; text: string };

export interface LoginSubmit {
  identifier: string;
  password: string;
  /** Mã OTP 6 số ở bước OTP; rỗng ở bước đầu. */
  otp: string;
  remember: boolean;
}

export interface LoginCardProps {
  busy?: boolean;
  error?: LoginError | null;
  /** Máy chủ đã xác nhận mật khẩu đúng và đòi mã OTP: thẻ chuyển sang bước OTP, giữ tên đăng nhập và mật khẩu đã nhập. */
  otpRequired?: boolean;
  /** Hộp "Liên hệ quản trị viên"; mặc định lấy từ cấu hình triển khai, null thì ẩn liên kết (QTPQ-Q-04). */
  support?: SupportInfo | null;
  initialIdentifier?: string;
  initialRemember?: boolean;
  onSubmit: (v: LoginSubmit) => void;
  /** Bước OTP: nút "Quay lại" về bước nhập mật khẩu. */
  onBack?: () => void;
  /** Nút "Hoàn tất trên trang xác thực" (việc lần đầu); nhận tên đăng nhập/SĐT đã gõ làm gợi ý. */
  onSetup?: (identifier: string) => void;
}

/** Câu thông báo của FN-01 (nguyên văn BRD và trả lời của nhóm dự án ngày 16/11/2026). */
export const LOGIN_MESSAGES = {
  identifierRequired: "Nhập tên đăng nhập hoặc số điện thoại",
  passwordRequired: "Nhập mật khẩu",
  accountLocked: "Tài khoản đã bị khóa. Liên hệ người đã cấp tài khoản để mở khóa.",
  tempLocked: (minutes: number) => `Tài khoản tạm khóa đăng nhập do nhập sai nhiều lần. Thử lại sau ${minutes} phút hoặc dùng Quên mật khẩu?`,
  failed: (remaining: number) => `Sai tài khoản hoặc mật khẩu. Còn ${remaining} lần thử.`,
  otpFailed: (remaining: number) => `Mã OTP không đúng. Còn ${remaining} lần thử.`,
  setupRequired: "Tài khoản cần hoàn tất thiết lập lần đầu (đổi mật khẩu tạm hoặc đăng ký mã OTP) trên trang xác thực.",
} as const;

const OTP_RE = /^[0-9]{6}$/;
const FORGOT_HREF = "/login/quen-mat-khau";

/**
 * Thẻ "Đăng nhập" (FN-01): tên đăng nhập hoặc số điện thoại, mật khẩu + "Hiện", Ghi nhớ, Quên mật khẩu, banner lỗi, bước OTP,
 * Liên hệ quản trị viên (hộp thông tin tĩnh). Mật khẩu chỉ nằm trong state của thẻ và đi tới máy chủ qua passwordLogin.
 */
export function LoginCard({ busy, error, otpRequired, support = config.support, initialIdentifier = "", initialRemember = false, onSubmit, onBack, onSetup }: LoginCardProps) {
  const id = useId();
  const [identifier, setIdentifier] = useState(initialIdentifier);
  const [password, setPassword] = useState("");
  const [otp, setOtp] = useState("");
  const [show, setShow] = useState(false);
  const [remember, setRemember] = useState(initialRemember);
  const [touched, setTouched] = useState(false);
  const [help, setHelp] = useState(false);
  // Chặn gửi lại đồng bộ: hai lần bấm sát nhau (trước khi `busy` kịp render) chỉ gửi một yêu cầu, nên không thành hai lần sai.
  const inFlight = useRef(false);
  useEffect(() => {
    // Có phản hồi (hết busy, lỗi mới hoặc chuyển bước OTP) thì cho gửi lại.
    if (!busy) inFlight.current = false;
  }, [busy, error, otpRequired]);
  // Rời bước OTP (quay lại, khóa tạm, việc lần đầu) thì bỏ mã đã nhập — điều chỉnh state ngay lúc render, không dùng effect.
  const [prevOtpRequired, setPrevOtpRequired] = useState(otpRequired);
  if (otpRequired !== prevOtpRequired) {
    setPrevOtpRequired(otpRequired);
    if (!otpRequired) setOtp("");
  }

  const idMissing = touched && !identifier.trim();
  const pwMissing = touched && !password;
  const otpBad = touched && !!otpRequired && !OTP_RE.test(otp);

  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (busy || inFlight.current) return;
    setTouched(true);
    if (!identifier.trim() || !password || (otpRequired && !OTP_RE.test(otp))) return;
    inFlight.current = true;
    onSubmit({ identifier: identifier.trim(), password, otp: otpRequired ? otp : "", remember });
  };

  const back = () => {
    setOtp("");
    setTouched(false);
    onBack?.();
  };

  return (
    <>
    <form className="login-card" onSubmit={submit} noValidate aria-labelledby={`${id}-h`}>
      <h1 id={`${id}-h`}>Đăng nhập</h1>
      <p className="sub">{otpRequired ? "Nhập mã OTP từ ứng dụng xác thực để tiếp tục." : "Nhập thông tin tài khoản để tiếp tục."}</p>
      <div className="field">
        <label htmlFor={`${id}-u`} className="req">
          Tên đăng nhập hoặc số điện thoại
        </label>
        <input
          id={`${id}-u`}
          className="input"
          name="username"
          autoComplete="username"
          value={identifier}
          readOnly={otpRequired}
          onChange={(e) => setIdentifier(e.target.value)}
          aria-invalid={idMissing || undefined}
          aria-describedby={idMissing ? `${id}-ue` : undefined}
        />
        {idMissing ? (
          <span className="err" id={`${id}-ue`}>
            {LOGIN_MESSAGES.identifierRequired}
          </span>
        ) : null}
      </div>
      <div className="field">
        <label htmlFor={`${id}-p`} className="req">
          Mật khẩu
        </label>
        <div className="pw-wrap">
          <input
            id={`${id}-p`}
            className="input"
            name="password"
            type={show ? "text" : "password"}
            autoComplete="current-password"
            value={password}
            readOnly={otpRequired}
            onChange={(e) => setPassword(e.target.value)}
            aria-invalid={pwMissing || undefined}
            aria-describedby={pwMissing ? `${id}-pe` : undefined}
          />
          <button type="button" className="pw-toggle" onClick={() => setShow((v) => !v)} aria-pressed={show} aria-controls={`${id}-p`}>
            {show ? "Ẩn" : "Hiện"}
          </button>
        </div>
        {pwMissing ? (
          <span className="err" id={`${id}-pe`}>
            {LOGIN_MESSAGES.passwordRequired}
          </span>
        ) : null}
      </div>
      {otpRequired ? (
        <div className="field">
          <label htmlFor={`${id}-o`} className="req">
            Mã OTP
          </label>
          <input
            id={`${id}-o`}
            className="input"
            name="otp"
            inputMode="numeric"
            autoComplete="one-time-code"
            maxLength={6}
            value={otp}
            onChange={(e) => setOtp(e.target.value.replace(/\D/g, ""))}
            aria-invalid={otpBad || undefined}
            aria-describedby={otpBad ? `${id}-oe` : undefined}
            autoFocus
          />
          {otpBad ? (
            <span className="err" id={`${id}-oe`}>
              Nhập mã OTP gồm 6 chữ số
            </span>
          ) : null}
        </div>
      ) : null}
      <div className="login-opts">
        <label>
          <input type="checkbox" checked={remember} onChange={(e) => setRemember(e.target.checked)} />
          Ghi nhớ đăng nhập
        </label>
        <Link href={FORGOT_HREF}>Quên mật khẩu?</Link>
      </div>
      <button type="submit" className="btn btn-p btn-block" disabled={busy}>
        {busy ? "Đang đăng nhập…" : "Đăng nhập"}
      </button>
      {otpRequired ? (
        <button type="button" className="btn btn-o btn-block" onClick={back} disabled={busy}>
          Quay lại
        </button>
      ) : null}
      {error ? (
        <div className="login-error" role="alert">
          <Icon name="xCircle" />
          {error.kind === "invalid" ? (
            <span>
              <b>{otpRequired ? LOGIN_MESSAGES.otpFailed(error.remaining) : LOGIN_MESSAGES.failed(error.remaining)}</b>
              Nếu quên mật khẩu, hãy dùng “Quên mật khẩu?” để đặt lại.
            </span>
          ) : error.kind === "locked" ? (
            <span>
              <b>{LOGIN_MESSAGES.tempLocked(error.minutes)}</b>
            </span>
          ) : error.kind === "account_locked" ? (
            <span>
              <b>{LOGIN_MESSAGES.accountLocked}</b>
            </span>
          ) : error.kind === "setup_required" ? (
            <span>
              <b>{LOGIN_MESSAGES.setupRequired}</b>
              {onSetup ? (
                <button type="button" className="btn btn-o btn-sm" onClick={() => onSetup(identifier.trim())}>
                  Hoàn tất trên trang xác thực
                </button>
              ) : null}
            </span>
          ) : (
            <span>{error.text}</span>
          )}
        </div>
      ) : null}
      {support ? (
        <p className="login-help">
          Cần hỗ trợ?{" "}
          <button type="button" className="link-btn" onClick={() => setHelp(true)}>
            Liên hệ quản trị viên
          </button>
        </p>
      ) : null}
    </form>
    {help && support ? (
      <Dialog title="Liên hệ quản trị viên" onClose={() => setHelp(false)}>
        <SupportContact info={support} />
      </Dialog>
    ) : null}
    </>
  );
}
