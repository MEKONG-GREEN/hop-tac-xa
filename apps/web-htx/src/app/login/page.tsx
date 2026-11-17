"use client";

import { useRouter, useSearchParams } from "next/navigation";
import { Suspense, useEffect, useRef, useState } from "react";
import { MOCK_PERSONAS } from "@/api/mock/personas";
import { useAuth, type SignInOutcome } from "@/auth/AuthProvider";
import { safeReturnTo } from "@/auth/oidc";
import { Icon } from "@/components/Icon";
import { config } from "@/config";
import { canSee, findNavItem, homeFor } from "@/nav/nav";
import { BrandMark } from "@/shell/BrandMark";
import { BRAND } from "@/shell/brand";
import { LoginCard, type LoginError, type LoginSubmit } from "@/shell/LoginCard";
import { LoginOtpForm, OTP_MESSAGES, type OtpStep } from "@/shell/LoginOtpForm";
import { menuAllows } from "@/shell/menu";
import { purgeLegacyIdentifier, rememberIdentifier, useRememberedIdentifier } from "@/lib/rememberLogin";

const NETWORK_ERROR = "Không kết nối được máy chủ đăng nhập. Vui lòng thử lại sau.";

/** Câu thông báo của kết quả không phải thành công, chờ mã hay lỗi riêng của màn mật khẩu. */
function messageFor(r: SignInOutcome): string {
  switch (r.kind) {
    case "rate_limited":
      return "Bạn thử đăng nhập quá nhanh. Vui lòng đợi một lát rồi thử lại.";
    case "network":
      return NETWORK_ERROR;
    case "otp_email_missing":
      return "Tài khoản chưa có email đã xác minh để nhận mã OTP. Liên hệ quản trị hệ thống.";
    case "otp_unavailable":
      return "Chưa gửi được mã OTP qua email. Vui lòng thử lại sau hoặc liên hệ quản trị hệ thống.";
    case "otp_rate_limited":
      return `Đã gửi quá nhiều mã OTP. Thử lại sau ${r.minutes} phút.`;
    default:
      return "Đăng nhập tạm thời chưa dùng được. Vui lòng thử lại sau hoặc liên hệ quản trị viên.";
  }
}

/** Lỗi hiển thị trên màn mật khẩu. */
function toLoginError(r: SignInOutcome): LoginError {
  switch (r.kind) {
    case "invalid":
      return { kind: "invalid", remaining: r.remaining };
    case "locked":
      return { kind: "locked", minutes: r.minutes };
    case "account_locked":
      return { kind: "account_locked" };
    case "setup_required":
      return { kind: "setup_required" };
    default:
      return { kind: "message", text: messageFor(r) };
  }
}

/** Câu thông báo giữ trên màn nhập mã; null → đưa về màn mật khẩu (sai mật khẩu, khóa, tài khoản không còn hợp lệ…). */
function otpMessage(r: SignInOutcome): string | null {
  switch (r.kind) {
    case "otp_invalid":
      return OTP_MESSAGES.invalid(r.remaining);
    case "otp_expired":
      return OTP_MESSAGES.expired;
    case "otp_unavailable":
    case "otp_rate_limited":
    case "rate_limited":
    case "network":
    case "unavailable":
      return messageFor(r);
    default:
      return null;
  }
}

function LoginInner() {
  const { status, sessionEnd, principal, kn, dataConsent, login, signIn, loginAsPersona } = useAuth();
  const router = useRouter();
  const params = useSearchParams();
  const returnTo = safeReturnTo(params.get("returnTo"));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<LoginError | null>(() => {
    // AC-N2 (FN-01/EX-06): bị đưa về vì tài khoản bị khóa trong lúc đang dùng → hiện câu EX-03 ngay khi mở màn.
    if (sessionEnd === "account_locked") return { kind: "account_locked" };
    return params.get("error") ? { kind: "message", text: "Không hoàn tất được đăng nhập. Vui lòng thử lại hoặc liên hệ quản trị viên." } : null;
  });
  // undefined trước hydrate → chờ để thẻ nhận đúng giá trị ban đầu.
  // Chỉ email, chỉ khi đã tích "Ghi nhớ đăng nhập", ở sessionStorage (src/lib/rememberLogin.ts).
  const remembered = useRememberedIdentifier();
  useEffect(purgeLegacyIdentifier, []);
  // ADR-0061: bước nhập mã OTP email của Admin. Mật khẩu chỉ giữ trong bộ nhớ trang tới khi xong bước 2 hoặc quay lại;
  // không ghi vào storage.
  const pending = useRef<{ identifier: string; password: string; challengeId: string } | null>(null);
  const [otpStep, setOtpStep] = useState<OtpStep | null>(null);
  const [otpError, setOtpError] = useState<string | null>(null);
  const [lastIdentifier, setLastIdentifier] = useState<string | null>(null);

  useEffect(() => {
    if (status !== "authenticated" || !principal) return;
    // AH-23: màn Đồng ý thu thập dữ liệu đứng trước mọi bước vào ứng dụng.
    if (dataConsent.status === "required" || dataConsent.status === "error") {
      router.replace(`/dong-y-thu-thap?returnTo=${encodeURIComponent(returnTo)}`);
      return;
    }
    if (kn.status === "choose") {
      router.replace(`/chon-vai-tro?returnTo=${encodeURIComponent(returnTo)}`);
      return;
    }
    if (kn.status === "active" && kn.context && kn.menu) {
      router.replace(returnTo !== "/" && menuAllows(kn.menu, returnTo) ? returnTo : kn.context.default_route);
      return;
    }
    const item = findNavItem(returnTo);
    const allowed = returnTo !== "/" && (!item || canSee(item, principal));
    router.replace(allowed ? returnTo : homeFor(principal));
  }, [status, principal, kn, dataConsent, returnTo, router]);

  const showOtp = (r: Extract<SignInOutcome, { kind: "otp_required" }>, identifier: string, password: string) => {
    pending.current = { identifier, password, challengeId: r.challengeId };
    setOtpError(null);
    setOtpStep((s) => ({ emailHint: r.emailHint, expiresIn: r.expiresIn, resendAfter: r.resendAfter, seq: (s?.seq ?? 0) + 1 }));
  };

  const cancelOtp = (err: LoginError | null = null) => {
    if (pending.current) setLastIdentifier(pending.current.identifier);
    pending.current = null;
    setOtpStep(null);
    setOtpError(null);
    setError(err);
  };

  const submit = async (v: LoginSubmit) => {
    setBusy(true);
    setError(null);
    rememberIdentifier(v.identifier, v.remember);
    try {
      const r = await signIn(v.identifier, v.password, v.otp || undefined);
      // Thành công: điều hướng do useEffect ở trên đảm nhiệm khi phiên đã thiết lập.
      if (r.kind === "authenticated") return;
      if (r.kind === "otp_required") showOtp(r, v.identifier, v.password);
      else setError(toLoginError(r));
    } catch {
      setError({ kind: "message", text: NETWORK_ERROR });
    }
    setBusy(false);
  };

  // Phản hồi của bước 2 hoặc của "Gửi lại mã" khi không phải thành công hay mã mới.
  const otpFailed = (r: SignInOutcome) => {
    const msg = otpMessage(r);
    if (msg === null) {
      cancelOtp(toLoginError(r));
      return;
    }
    setOtpError(msg);
    // Mã hết hạn hoặc bị hủy: mở ngay nút "Gửi lại mã".
    if (r.kind === "otp_expired") setOtpStep((s) => (s ? { ...s, resendAfter: 0, seq: s.seq + 1 } : s));
  };

  const verifyOtp = async (code: string) => {
    const p = pending.current;
    if (!p) return;
    setBusy(true);
    setOtpError(null);
    try {
      const r = await signIn(p.identifier, p.password, code, p.challengeId);
      if (r.kind === "authenticated") {
        pending.current = null;
        return;
      }
      otpFailed(r);
    } catch {
      setOtpError(NETWORK_ERROR);
    }
    setBusy(false);
  };

  const resendOtp = async () => {
    const p = pending.current;
    if (!p) return;
    setBusy(true);
    setOtpError(null);
    try {
      const r = await signIn(p.identifier, p.password);
      if (r.kind === "otp_required") showOtp(r, p.identifier, p.password);
      else if (r.kind !== "authenticated") otpFailed(r);
    } catch {
      setOtpError(NETWORK_ERROR);
    }
    setBusy(false);
  };

  // Việc lần đầu (đổi mật khẩu tạm, đăng ký OTP) hoàn tất ở trang xác thực của Keycloak cho tới khi có AH-269.
  const finishSetup = (identifier: string) => {
    setBusy(true);
    login(returnTo, identifier).catch(() => {
      setError({ kind: "message", text: "Không kết nối được máy chủ đăng nhập. Vui lòng thử lại sau." });
      setBusy(false);
    });
  };

  return (
    <div className="auth-bg">
      <main className="login">
        <section className="login-brand" aria-label="Giới thiệu hệ thống">
          <div className="brand-row">
            <BrandMark />
            {BRAND.productName}
          </div>
          <div className="login-hero">
            <span className="sys-tag">{BRAND.systemTag}</span>
            <p className="sr-only">Tên sản phẩm, logo và ảnh nền là tạm, chờ bộ nhận diện của TTKNQG.</p>
            <h2>{BRAND.loginTitle}</h2>
            <p>{BRAND.loginLead}</p>
          </div>
          <p className="login-copy">
            © {BRAND.year} {BRAND.orgFull}
          </p>
        </section>
        <section className="login-side">
          {otpStep ? (
            <LoginOtpForm
              key={otpStep.seq}
              step={otpStep}
              busy={busy || status === "loading"}
              error={otpError}
              onVerify={(code) => void verifyOtp(code)}
              onResend={() => void resendOtp()}
              onCancel={() => cancelOtp()}
            />
          ) : remembered !== undefined ? (
            <LoginCard
              busy={busy || status === "loading"}
              error={error}
              initialIdentifier={lastIdentifier ?? remembered ?? ""}
              initialRemember={!!remembered}
              onSubmit={(v) => void submit(v)}
              onSetup={finishSetup}
            />
          ) : null}
          {config.mock ? (
            <div className="persona-card">
              <div className="alert alert-info">
                <Icon name="info" />
                <span>
                  Chế độ <b style={{ display: "inline" }}>dữ liệu giả lập</b> (NEXT_PUBLIC_API_MOCK=1): không gọi Keycloak hay backend. Tài khoản KN v7.7
                  (vd an.nv@angiang.gov.vn) đăng nhập bằng mật khẩu thử ở README; hoặc chọn một vai thử dưới đây.
                </span>
              </div>
              <h2>Vai thử (giao diện hiện có)</h2>
              <div className="stack" style={{ gap: 6 }} role="list">
                {MOCK_PERSONAS.map((p) => (
                  <div role="listitem" key={p.id}>
                    <button type="button" className="persona" onClick={() => void loginAsPersona(p.id)}>
                      <b>{p.title}</b>
                      <span className="mute small">{p.description}</span>
                    </button>
                  </div>
                ))}
              </div>
            </div>
          ) : null}
        </section>
      </main>
    </div>
  );
}

export default function LoginPage() {
  return (
    <Suspense fallback={null}>
      <LoginInner />
    </Suspense>
  );
}
