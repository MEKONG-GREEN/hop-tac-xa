// Đăng nhập riêng của App HTX (FN-01, ADR-0060): gửi tên đăng nhập hoặc SĐT + mật khẩu về máy chủ
// (POST /v1/public/auth/login), làm mới và đăng xuất cũng đi qua máy chủ. Admin đăng nhập hai bước: mật khẩu, rồi mã OTP
// gửi qua email (ADR-0061) — bước 2 gửi lại mật khẩu kèm challenge_id và mã.
//
// Client riêng, KHÔNG gắn authMiddleware, idempotencyScopeMiddleware hay idempotencyMiddleware: thân request chứa mật khẩu
// không được thành dấu vân tay idempotency, và API đăng nhập không đòi (cũng không nhận) Idempotency-Key hay Authorization.
// Không gửi cookie (credentials: omit). Mật khẩu và OTP không được ghi vào log, storage hay thông báo lỗi.
import createClient from "openapi-fetch";
import type { paths } from "@/api/generated/application";
import { config } from "@/config";

export interface LoginTokens {
  accessToken: string;
  refreshToken: string;
  /** Giây. */
  expiresIn: number;
  refreshExpiresIn: number;
}

/** Bước 1 của Admin (ADR-0061): máy chủ đã gửi mã OTP qua email. Thời gian tính bằng giây. */
export interface OtpPrompt {
  challengeId: string;
  /** Email đã che, vd a***@mekonggreen.vn. */
  emailHint: string;
  expiresIn: number;
  resendAfter: number;
}

/** Kết quả đăng nhập theo mã problem của máy chủ (docs/errors.md, pf/accounts). */
export type LoginOutcome =
  | { kind: "authenticated"; tokens: LoginTokens }
  /** ADR-0061: Admin đúng mật khẩu, mã OTP đã gửi qua email — gửi lại cùng mật khẩu kèm challengeId và mã. */
  | ({ kind: "otp_required" } & OtpPrompt)
  /** accounts.login_otp_invalid: mã email sai, `remaining` = số lần thử còn lại của mã. */
  | { kind: "otp_invalid"; remaining: number }
  /** accounts.login_otp_expired: mã hết hạn, đã dùng, đã hủy hoặc đã có mã mới — xin mã mới. */
  | { kind: "otp_expired" }
  /** accounts.login_otp_email_missing: tài khoản chưa có email đã xác minh. */
  | { kind: "otp_email_missing" }
  /** accounts.login_otp_unavailable: chưa gửi được mã (thiếu SMTP hoặc máy chủ thư lỗi). */
  | { kind: "otp_unavailable" }
  /** accounts.login_otp_rate_limited: quá 10 mã/giờ; `minutes` = số phút chờ. */
  | { kind: "otp_rate_limited"; minutes: number }
  /** accounts.login_failed (EX-02): `remaining` = số lần thử còn lại. */
  | { kind: "invalid"; remaining: number }
  /** accounts.login_temporarily_locked: `minutes` = số phút còn lại (làm tròn lên). */
  | { kind: "locked"; minutes: number }
  /** accounts.account_locked (EX-03): tài khoản đã bị khóa (FN-07). */
  | { kind: "account_locked" }
  /** accounts.login_setup_required: còn việc lần đầu (đổi mật khẩu tạm, đăng ký OTP) — hoàn tất ở trang xác thực. */
  | { kind: "setup_required" }
  | { kind: "rate_limited" }
  /** 400, 422, 503 (accounts.login_unavailable, accounts.dependency_unavailable) hoặc phản hồi lạ. */
  | { kind: "unavailable" }
  | { kind: "network" };

export type RefreshOutcome = { kind: "ok"; tokens: LoginTokens } | { kind: "expired" } | { kind: "error" };

export interface PasswordLoginApi {
  /** `challengeId` có giá trị là bước 2 của Admin (ADR-0061): `otp` là mã trong email. */
  login: (identifier: string, password: string, otp?: string, challengeId?: string) => Promise<LoginOutcome>;
  refresh: (refreshToken: string) => Promise<RefreshOutcome>;
  /** Lỗi bị bỏ qua: đăng xuất ở web luôn xóa phiên cục bộ. */
  logout: (refreshToken: string) => Promise<void>;
}

interface ProblemBody {
  code?: unknown;
  remaining_attempts?: unknown;
  retry_after_minutes?: unknown;
}

const asInt = (v: unknown, fallback: number): number => (typeof v === "number" && Number.isFinite(v) ? Math.trunc(v) : fallback);

interface TokenBody {
  access_token?: string;
  refresh_token?: string;
  expires_in?: number;
  refresh_expires_in?: number;
}

interface OtpBody {
  challenge_id?: string;
  email_hint?: string;
  expires_in?: number;
  resend_after?: number;
}

function toOtpPrompt(b: OtpBody | undefined): OtpPrompt | null {
  if (!b?.challenge_id) return null;
  return {
    challengeId: b.challenge_id,
    emailHint: b.email_hint ?? "",
    expiresIn: Math.max(asInt(b.expires_in, 300), 0),
    resendAfter: Math.max(asInt(b.resend_after, 60), 0),
  };
}

function toTokens(b: TokenBody | undefined): LoginTokens | null {
  if (!b?.access_token || !b.refresh_token) return null;
  return {
    accessToken: b.access_token,
    refreshToken: b.refresh_token,
    expiresIn: asInt(b.expires_in, 300),
    refreshExpiresIn: asInt(b.refresh_expires_in, 0),
  };
}

export function createPasswordLoginApi(opts: { baseUrl: string; fetch?: (input: Request) => Promise<Response> }): PasswordLoginApi {
  const client = createClient<paths>({
    baseUrl: opts.baseUrl.replace(/\/+$/, ""),
    credentials: "omit",
    ...(opts.fetch ? { fetch: opts.fetch } : {}),
  });

  return {
    async login(identifier, password, otp, challengeId) {
      let res;
      try {
        res = await client.POST("/v1/public/auth/login", {
          body: { identifier, password, ...(otp ? { otp } : {}), ...(challengeId ? { challenge_id: challengeId } : {}) },
        });
      } catch {
        return { kind: "network" };
      }
      const { data, response } = res;
      const problem = (res.error ?? {}) as ProblemBody;
      switch (response.status) {
        case 200: {
          if (data?.status === "otp_required") {
            const prompt = toOtpPrompt(data);
            return prompt ? { kind: "otp_required", ...prompt } : { kind: "unavailable" };
          }
          const tokens = data?.status === "authenticated" ? toTokens(data) : null;
          return tokens ? { kind: "authenticated", tokens } : { kind: "unavailable" };
        }
        case 401:
          if (problem.code === "accounts.login_otp_invalid") return { kind: "otp_invalid", remaining: Math.max(asInt(problem.remaining_attempts, 0), 0) };
          return problem.code === "accounts.login_failed" ? { kind: "invalid", remaining: Math.max(asInt(problem.remaining_attempts, 0), 0) } : { kind: "unavailable" };
        case 403:
          if (problem.code === "accounts.account_locked") return { kind: "account_locked" };
          if (problem.code === "accounts.login_otp_email_missing") return { kind: "otp_email_missing" };
          return problem.code === "accounts.login_setup_required" ? { kind: "setup_required" } : { kind: "unavailable" };
        case 410:
          return problem.code === "accounts.login_otp_expired" ? { kind: "otp_expired" } : { kind: "unavailable" };
        case 423:
          return { kind: "locked", minutes: Math.max(asInt(problem.retry_after_minutes, 15), 1) };
        case 429:
          if (problem.code === "accounts.login_otp_rate_limited") return { kind: "otp_rate_limited", minutes: Math.max(asInt(problem.retry_after_minutes, 60), 1) };
          return { kind: "rate_limited" };
        case 503:
          return problem.code === "accounts.login_otp_unavailable" ? { kind: "otp_unavailable" } : { kind: "unavailable" };
        default:
          return { kind: "unavailable" };
      }
    },

    async refresh(refreshToken) {
      try {
        const { data, response } = await client.POST("/v1/public/auth/refresh", { body: { refresh_token: refreshToken } });
        if (response.status === 401) return { kind: "expired" };
        const tokens = response.ok ? toTokens(data) : null;
        return tokens ? { kind: "ok", tokens } : { kind: "error" };
      } catch {
        return { kind: "error" };
      }
    },

    async logout(refreshToken) {
      try {
        await client.POST("/v1/public/auth/logout", { body: { refresh_token: refreshToken } });
      } catch {
        // bỏ qua: phiên cục bộ vẫn bị xóa
      }
    },
  };
}

/** Client dùng cho ứng dụng (gốc Application API từ cấu hình). */
export const passwordLogin: PasswordLoginApi = createPasswordLoginApi({ baseUrl: config.apiBaseUrl });
