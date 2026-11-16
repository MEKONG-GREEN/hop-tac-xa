// FN-01 (ADR-0060): client đăng nhập riêng — không Idempotency-Key/Authorization, thân chỉ gồm identifier/password/otp,
// ánh xạ đúng mọi mã problem của máy chủ sang kết quả hiển thị.
import { describe, expect, it, vi } from "vitest";
import { createPasswordLoginApi } from "./passwordLogin";

function reply(status: number, body?: unknown, headers: Record<string, string> = {}): Response {
  const type = status >= 400 ? "application/problem+json" : "application/json";
  return new Response(status === 204 ? null : JSON.stringify(body ?? {}), { status, headers: { "Content-Type": type, ...headers } });
}

function harness(res: Response | Error) {
  const sent: Request[] = [];
  const fetch = vi.fn(async (req: Request) => {
    sent.push(req.clone());
    if (res instanceof Error) throw res;
    return res.clone();
  });
  return { api: createPasswordLoginApi({ baseUrl: "http://api.test/", fetch }), sent, fetch };
}

const TOKENS = { status: "authenticated", access_token: "at", refresh_token: "rt", expires_in: 300, refresh_expires_in: 36000, token_type: "Bearer" };

describe("passwordLogin.login", () => {
  it("gửi đúng đường dẫn, thân chỉ gồm identifier/password/otp, không có Idempotency-Key hay Authorization", async () => {
    const { api, sent } = harness(reply(200, TOKENS));
    await api.login("0912345678", "Mat-khau-12!", "123456");
    expect(sent).toHaveLength(1);
    expect(sent[0]!.method).toBe("POST");
    expect(new URL(sent[0]!.url).pathname).toBe("/v1/public/auth/login");
    expect(sent[0]!.headers.get("Idempotency-Key")).toBeNull();
    expect(sent[0]!.headers.get("Authorization")).toBeNull();
    expect(sent[0]!.credentials).toBe("omit");
    expect(await sent[0]!.json()).toEqual({ identifier: "0912345678", password: "Mat-khau-12!", otp: "123456" });
  });

  it("không gửi otp khi không có", async () => {
    const { api, sent } = harness(reply(200, TOKENS));
    await api.login("an", "x");
    expect(await sent[0]!.json()).toEqual({ identifier: "an", password: "x" });
  });

  it("200 authenticated → token; 200 otp_required → otp_required", async () => {
    expect(await harness(reply(200, TOKENS)).api.login("a", "b")).toEqual({
      kind: "authenticated",
      tokens: { accessToken: "at", refreshToken: "rt", expiresIn: 300, refreshExpiresIn: 36000 },
    });
    expect(await harness(reply(200, { status: "otp_required" })).api.login("a", "b")).toEqual({ kind: "otp_required" });
  });

  it("ánh xạ mã problem sang kết quả", async () => {
    const cases: [Response, unknown][] = [
      [reply(401, { code: "accounts.login_failed", remaining_attempts: 4 }), { kind: "invalid", remaining: 4 }],
      [reply(401, { code: "accounts.login_failed", remaining_attempts: 1 }), { kind: "invalid", remaining: 1 }],
      [reply(423, { code: "accounts.login_temporarily_locked", retry_after_minutes: 15 }, { "Retry-After": "900" }), { kind: "locked", minutes: 15 }],
      [reply(403, { code: "accounts.account_locked" }), { kind: "account_locked" }],
      [reply(403, { code: "accounts.login_setup_required" }), { kind: "setup_required" }],
      [reply(429, { code: "rate_limited" }), { kind: "rate_limited" }],
      [reply(503, { code: "accounts.login_unavailable" }), { kind: "unavailable" }],
      [reply(503, { code: "accounts.dependency_unavailable" }), { kind: "unavailable" }],
      [reply(422, { code: "validation_failed" }), { kind: "unavailable" }],
      [reply(401, { code: "unauthenticated" }), { kind: "unavailable" }],
      [reply(200, { status: "authenticated" }), { kind: "unavailable" }], // thiếu token → không coi là đã đăng nhập
    ];
    for (const [res, want] of cases) {
      expect(await harness(res).api.login("a", "b")).toEqual(want);
    }
  });

  it("không tới được máy chủ → network", async () => {
    expect(await harness(new TypeError("Failed to fetch")).api.login("a", "b")).toEqual({ kind: "network" });
  });
});

describe("passwordLogin.refresh và logout", () => {
  it("refresh: 200 → token mới; 401 → hết phiên; lỗi khác → error", async () => {
    const h = harness(reply(200, TOKENS));
    expect(await h.api.refresh("rt")).toEqual({
      kind: "ok",
      tokens: { accessToken: "at", refreshToken: "rt", expiresIn: 300, refreshExpiresIn: 36000 },
    });
    expect(new URL(h.sent[0]!.url).pathname).toBe("/v1/public/auth/refresh");
    expect(await h.sent[0]!.json()).toEqual({ refresh_token: "rt" });
    expect(h.sent[0]!.headers.get("Idempotency-Key")).toBeNull();
    expect(await harness(reply(401, { code: "accounts.session_expired" })).api.refresh("rt")).toEqual({ kind: "expired" });
    expect(await harness(reply(503, { code: "accounts.dependency_unavailable" })).api.refresh("rt")).toEqual({ kind: "error" });
    expect(await harness(new TypeError("x")).api.refresh("rt")).toEqual({ kind: "error" });
  });

  it("logout: gửi refresh_token tới /logout, lỗi bị bỏ qua", async () => {
    const h = harness(reply(204));
    await h.api.logout("rt");
    expect(new URL(h.sent[0]!.url).pathname).toBe("/v1/public/auth/logout");
    expect(await h.sent[0]!.json()).toEqual({ refresh_token: "rt" });
    await expect(harness(new TypeError("x")).api.logout("rt")).resolves.toBeUndefined();
    await expect(harness(reply(503, { code: "accounts.dependency_unavailable" })).api.logout("rt")).resolves.toBeUndefined();
  });
});
