"use client";

import { useEffect, useId, useRef, useState, type FormEvent } from "react";
import { Icon } from "@/components/Icon";

/** Bước 2 của Admin (ADR-0061): mã OTP đã gửi qua email. Thời gian tính bằng giây. */
export interface OtpStep {
  /** Email đã che, vd a***@mekonggreen.vn. */
  emailHint: string;
  expiresIn: number;
  resendAfter: number;
  /** Tăng mỗi lần máy chủ trả thử thách (kể cả thử thách cũ); trang dùng làm `key` để dựng lại thẻ từ đầu. */
  seq: number;
}

export const OTP_MESSAGES = {
  sent: (hint: string) => `Mã OTP gồm 6 chữ số đã được gửi tới ${hint || "email của tài khoản"}. Mã có hiệu lực 5 phút.`,
  codeRequired: "Nhập mã OTP gồm 6 chữ số",
  invalid: (remaining: number) => `Mã OTP không đúng. Còn ${remaining} lần thử.`,
  expired: "Mã OTP đã hết hạn hoặc đã bị hủy. Bấm “Gửi lại mã” để nhận mã mới.",
} as const;

const OTP_RE = /^[0-9]{6}$/;

export interface LoginOtpFormProps {
  step: OtpStep;
  busy?: boolean;
  /** Lỗi của bước nhập mã (câu đã dựng sẵn); null là không có. */
  error?: string | null;
  onVerify: (code: string) => void;
  onResend: () => void;
  onCancel: () => void;
}

/** Màn nhập mã OTP gửi qua email: ô mã 6 số, Xác nhận, Gửi lại mã (mở sau thời gian chờ), Quay lại. */
export function LoginOtpForm({ step, busy, error, onVerify, onResend, onCancel }: LoginOtpFormProps) {
  const id = useId();
  const [code, setCode] = useState("");
  const [touched, setTouched] = useState(false);
  const [wait, setWait] = useState(step.resendAfter);
  const inFlight = useRef(false);
  const input = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (!busy) inFlight.current = false;
  }, [busy, error]);

  // Đếm ngược tới khi được gửi lại mã; đưa con trỏ vào ô mã. Mã mới thì trang dựng lại thẻ (key = seq) nên ô mã và đếm
  // ngược bắt đầu lại từ đầu.
  useEffect(() => {
    input.current?.focus();
    if (step.resendAfter <= 0) return;
    const started = Date.now();
    const timer = setInterval(() => {
      const left = step.resendAfter - Math.floor((Date.now() - started) / 1000);
      setWait(Math.max(left, 0));
      if (left <= 0) clearInterval(timer);
    }, 1000);
    return () => clearInterval(timer);
  }, [step.resendAfter]);

  const bad = touched && !OTP_RE.test(code);

  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (busy || inFlight.current) return;
    setTouched(true);
    if (!OTP_RE.test(code)) return;
    inFlight.current = true;
    onVerify(code);
  };

  return (
    <form className="login-card" onSubmit={submit} noValidate aria-labelledby={`${id}-h`}>
      <h1 id={`${id}-h`}>Nhập mã OTP</h1>
      <p className="sub">{OTP_MESSAGES.sent(step.emailHint)}</p>
      <div className="field">
        <label htmlFor={`${id}-c`} className="req">
          Mã OTP
        </label>
        <input
          ref={input}
          id={`${id}-c`}
          className="input"
          name="otp"
          inputMode="numeric"
          autoComplete="one-time-code"
          maxLength={6}
          value={code}
          onChange={(e) => setCode(e.target.value.replace(/\D/g, ""))}
          aria-invalid={bad || undefined}
          aria-describedby={bad ? `${id}-ce` : undefined}
        />
        {bad ? (
          <span className="err" id={`${id}-ce`}>
            {OTP_MESSAGES.codeRequired}
          </span>
        ) : null}
      </div>
      <button type="submit" className="btn btn-p btn-block" disabled={busy}>
        {busy ? "Đang xác nhận…" : "Xác nhận"}
      </button>
      <div className="login-opts">
        <button type="button" className="link-btn" onClick={onCancel} disabled={busy}>
          Quay lại
        </button>
        <button type="button" className="link-btn" onClick={onResend} disabled={busy || wait > 0}>
          {wait > 0 ? `Gửi lại mã sau ${wait} giây` : "Gửi lại mã"}
        </button>
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
