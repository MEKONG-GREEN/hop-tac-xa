"use client";

import Link from "next/link";
import { useEffect, useId, useState, type FormEvent } from "react";
import createClient from "openapi-fetch";
import type { paths } from "@/api/generated/application";
import { Icon } from "@/components/Icon";
import { config } from "@/config";
import { FIRST_PASSWORD_MESSAGES } from "./FirstPasswordForm";

/**
 * Quên mật khẩu của realm HTX (AH-280, FN-10): nhập tên đăng nhập hoặc SĐT → mã OTP gửi qua SMS tới số đã lưu (che 4 đầu
 * / 3 cuối) → nhập mã và mật khẩu mới. Client riêng, không Idempotency-Key, không gửi cookie; mã và mật khẩu không vào
 * storage hay log. Câu thông báo lấy từ máy chủ (FN-10/EX-01 → EX-06).
 */
type Fetch = (input: Request) => Promise<Response>;
interface Step {
  identifier: string;
  challengeId: string;
  phoneHint: string;
  resendAfter: number;
  seq: number;
}
const NETWORK = "Không kết nối được máy chủ. Vui lòng thử lại sau.";
const detailOf = (e: unknown, fallback: string) => {
  const d = (e as { detail?: unknown } | undefined)?.detail;
  return typeof d === "string" && d ? d : fallback;
};

export function OtpForgotPassword({ fetch }: { fetch?: Fetch }) {
  const id = useId();
  const [client] = useState(() => createClient<paths>({ baseUrl: config.apiBaseUrl.replace(/\/+$/, ""), credentials: "omit", ...(fetch ? { fetch } : {}) }));
  const [identifier, setIdentifier] = useState("");
  const [step, setStep] = useState<Step | null>(null);
  const [code, setCode] = useState("");
  const [next, setNext] = useState("");
  const [confirm, setConfirm] = useState("");
  const [fields, setFields] = useState<Record<string, string>>({});
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState(false);
  const [wait, setWait] = useState(0);

  useEffect(() => {
    if (!step || step.resendAfter <= 0) return;
    const started = Date.now();
    const timer = setInterval(() => {
      const left = step.resendAfter - Math.floor((Date.now() - started) / 1000);
      setWait(Math.max(left, 0));
      if (left <= 0) clearInterval(timer);
    }, 1000);
    return () => clearInterval(timer);
  }, [step]);

  const send = async (who: string) => {
    if (!who.trim()) {
      setFields({ identifier: "Nhập tên đăng nhập hoặc số điện thoại" });
      return;
    }
    setBusy(true);
    setError(null);
    setFields({});
    try {
      const { data, error: e } = await client.POST("/v1/public/auth/password-otp", { body: { identifier: who.trim() } });
      if (data) setWait(data.resend_after);
      if (data) setStep((s) => ({ identifier: who.trim(), challengeId: data.challenge_id, phoneHint: data.phone_hint, resendAfter: data.resend_after, seq: (s?.seq ?? 0) + 1 }));
      else setError(detailOf(e, "Chưa gửi được mã OTP. Vui lòng thử lại sau."));
    } catch {
      setError(NETWORK);
    }
    setBusy(false);
  };

  const reset = async (ev: FormEvent) => {
    ev.preventDefault();
    if (!step || busy) return;
    const local: Record<string, string> = {};
    if (!/^[0-9]{6}$/.test(code)) local.otp = "Nhập mã OTP gồm 6 chữ số";
    if ([...next].length < 6) local.new_password = FIRST_PASSWORD_MESSAGES.short;
    if (confirm !== next) local.confirm_password = FIRST_PASSWORD_MESSAGES.mismatch;
    setFields(local);
    if (Object.keys(local).length) return;
    setBusy(true);
    setError(null);
    try {
      const { data, error: e, response } = await client.POST("/v1/public/auth/password-otp/reset", {
        body: { identifier: step.identifier, challenge_id: step.challengeId, otp: code, new_password: next, confirm_password: confirm },
      });
      if (data) setDone(true);
      else if (response.status === 422) {
        const errs = (e as { errors?: { field?: string; reason?: string }[] } | undefined)?.errors ?? [];
        setFields(Object.fromEntries(errs.filter((x) => x.field).map((x) => [x.field as string, x.reason ?? "Không hợp lệ"])));
      } else {
        setError(detailOf(e, "Không đặt lại được mật khẩu. Vui lòng thử lại."));
        if (response.status === 410) setCode("");
      }
    } catch {
      setError(NETWORK);
    }
    setBusy(false);
  };

  const field = (name: string, label: string, value: string, set: (v: string) => void, type: string, autoComplete: string) => (
    <div className="field">
      <label htmlFor={`${id}-${name}`} className="req">{label}</label>
      <input id={`${id}-${name}`} className="input" type={type} autoComplete={autoComplete} inputMode={name === "otp" ? "numeric" : undefined}
        value={value} onChange={(e) => set(name === "otp" ? e.target.value.replace(/\D/g, "").slice(0, 6) : e.target.value)}
        aria-invalid={!!fields[name] || undefined} aria-describedby={fields[name] ? `${id}-${name}-e` : undefined} />
      {fields[name] ? <span className="err" id={`${id}-${name}-e`}>{fields[name]}</span> : null}
    </div>
  );
  const alert = error ? (
    <div className="login-error" role="alert">
      <Icon name="xCircle" />
      <span>{error}</span>
    </div>
  ) : null;

  if (done) {
    return (
      <>
        <p role="status">Đã đặt mật khẩu mới. Đăng nhập bằng mật khẩu mới.</p>
        <Link className="btn btn-p btn-block" href="/login">Đăng nhập</Link>
      </>
    );
  }
  if (!step) {
    return (
      <form onSubmit={(e) => { e.preventDefault(); if (!busy) void send(identifier); }} noValidate aria-label="Gửi mã OTP đặt lại mật khẩu">
        <p className="sub">Nhập tên đăng nhập hoặc số điện thoại. Hệ thống gửi mã OTP qua SMS tới số điện thoại của tài khoản.</p>
        {field("identifier", "Tên đăng nhập hoặc số điện thoại", identifier, setIdentifier, "text", "username")}
        <button type="submit" className="btn btn-p btn-block" disabled={busy}>{busy ? "Đang gửi…" : "Gửi mã OTP"}</button>
        {alert}
        <p className="login-help"><Link href="/login">Quay lại đăng nhập</Link></p>
      </form>
    );
  }
  return (
    <form key={step.seq} onSubmit={(e) => void reset(e)} noValidate aria-label="Đặt mật khẩu mới bằng mã OTP">
      <p className="sub" role="status">Mã OTP gồm 6 chữ số đã gửi qua SMS tới {step.phoneHint}. Mã có hiệu lực 5 phút.</p>
      {field("otp", "Mã OTP", code, setCode, "text", "one-time-code")}
      {field("new_password", "Mật khẩu mới", next, setNext, "password", "new-password")}
      {field("confirm_password", "Nhập lại mật khẩu mới", confirm, setConfirm, "password", "new-password")}
      <button type="submit" className="btn btn-p btn-block" disabled={busy}>{busy ? "Đang lưu…" : "Đặt mật khẩu mới"}</button>
      <div className="login-opts">
        <button type="button" className="link-btn" disabled={busy || wait > 0} onClick={() => void send(step.identifier)}>
          {wait > 0 ? `Gửi lại mã sau ${wait} giây` : "Gửi lại mã"}
        </button>
        <Link href="/login">Quay lại đăng nhập</Link>
      </div>
      {alert}
    </form>
  );
}
