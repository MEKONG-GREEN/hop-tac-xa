// Đăng nhập riêng của App HTX (FN-01, ADR-0060): gửi tên đăng nhập hoặc SĐT + mật khẩu về máy chủ
// (POST /v1/public/auth/login), làm mới và đăng xuất cũng đi qua máy chủ.
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

/** Kết quả đăng nhập theo mã problem của máy chủ (docs/errors.md, pf/accounts). */
export type LoginOutcome =
  | { kind: "authenticated"; tokens: LoginTokens }
  | { kind: "otp_required" }
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
  login: (identifier: string, password: string, otp?: string) => Promise<LoginOutcome>;
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
    async login(identifier, password, otp) {
      let res;
      try {
        res = await client.POST("/v1/public/auth/login", { body: { identifier, password, ...(otp ? { otp } : {}) } });
      } catch {
        return { kind: "network" };
      }
      const { data, response } = res;
      const problem = (res.error ?? {}) as ProblemBody;
      switch (response.status) {
        case 200: {
          if (data?.status === "otp_required") return { kind: "otp_required" };
          const tokens = data?.status === "authenticated" ? toTokens(data) : null;
          return tokens ? { kind: "authenticated", tokens } : { kind: "unavailable" };
        }
        case 401:
          return problem.code === "accounts.login_failed" ? { kind: "invalid", remaining: Math.max(asInt(problem.remaining_attempts, 0), 0) } : { kind: "unavailable" };
        case 403:
          if (problem.code === "accounts.account_locked") return { kind: "account_locked" };
          return problem.code === "accounts.login_setup_required" ? { kind: "setup_required" } : { kind: "unavailable" };
        case 423:
          return { kind: "locked", minutes: Math.max(asInt(problem.retry_after_minutes, 15), 1) };
        case 429:
          return { kind: "rate_limited" };
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
