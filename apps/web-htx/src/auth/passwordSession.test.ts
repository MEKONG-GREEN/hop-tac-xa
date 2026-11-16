// FN-01 (ADR-0060): phiên của màn đăng nhập riêng chỉ ở bộ nhớ; làm mới trước hạn; làm mới lỗi → hết phiên.
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { LoginTokens, PasswordLoginApi, RefreshOutcome } from "./passwordLogin";
import { passwordSession } from "./passwordSession";

const tokens = (over: Partial<LoginTokens> = {}): LoginTokens => ({
  accessToken: "access-1",
  refreshToken: "refresh-secret-1",
  expiresIn: 300,
  refreshExpiresIn: 36000,
  ...over,
});

function fakeApi(results: RefreshOutcome[]) {
  const refresh = vi.fn<PasswordLoginApi["refresh"]>(async () => results.shift() ?? { kind: "expired" });
  const api: PasswordLoginApi = { login: vi.fn(), refresh, logout: vi.fn() };
  return { api, refresh };
}

beforeEach(() => {
  vi.useFakeTimers();
  window.localStorage.clear();
  window.sessionStorage.clear();
});

afterEach(() => {
  passwordSession.stop();
  vi.useRealTimers();
});

describe("passwordSession", () => {
  it("giữ refresh token ở bộ nhớ: sessionStorage và localStorage không chứa token", () => {
    const { api } = fakeApi([]);
    passwordSession.start(tokens(), { onAccessToken: vi.fn(), onExpired: vi.fn() }, api);
    expect(passwordSession.active()).toBe(true);
    expect(passwordSession.refreshToken()).toBe("refresh-secret-1");
    for (const store of [window.localStorage, window.sessionStorage]) {
      expect(store.length).toBe(0);
      expect(JSON.stringify({ ...store })).not.toContain("refresh-secret-1");
    }
  });

  it("làm mới trước hạn 30 giây: đặt access token mới, đổi refresh token, hẹn lần kế tiếp", async () => {
    const onAccessToken = vi.fn();
    const { api, refresh } = fakeApi([
      { kind: "ok", tokens: tokens({ accessToken: "access-2", refreshToken: "refresh-2", expiresIn: 300 }) },
      { kind: "ok", tokens: tokens({ accessToken: "access-3", refreshToken: "refresh-3", expiresIn: 300 }) },
    ]);
    passwordSession.start(tokens({ expiresIn: 300 }), { onAccessToken, onExpired: vi.fn() }, api);
    await vi.advanceTimersByTimeAsync(269_000);
    expect(refresh).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(1_000);
    expect(refresh).toHaveBeenCalledWith("refresh-secret-1");
    expect(onAccessToken).toHaveBeenLastCalledWith("access-2");
    expect(passwordSession.refreshToken()).toBe("refresh-2");
    await vi.advanceTimersByTimeAsync(270_000);
    expect(refresh).toHaveBeenLastCalledWith("refresh-2");
    expect(onAccessToken).toHaveBeenLastCalledWith("access-3");
  });

  it("access token sống rất ngắn: hẹn tối thiểu 5 giây", async () => {
    const { api, refresh } = fakeApi([{ kind: "ok", tokens: tokens({ expiresIn: 300 }) }]);
    passwordSession.start(tokens({ expiresIn: 10 }), { onAccessToken: vi.fn(), onExpired: vi.fn() }, api);
    await vi.advanceTimersByTimeAsync(4_900);
    expect(refresh).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(200);
    expect(refresh).toHaveBeenCalledTimes(1);
  });

  it("làm mới lỗi hoặc phiên hết → onExpired và xóa giá trị", async () => {
    for (const outcome of [{ kind: "expired" }, { kind: "error" }] as RefreshOutcome[]) {
      const onExpired = vi.fn();
      const { api } = fakeApi([outcome]);
      passwordSession.start(tokens(), { onAccessToken: vi.fn(), onExpired }, api);
      await vi.advanceTimersByTimeAsync(300_000);
      expect(onExpired).toHaveBeenCalledTimes(1);
      expect(passwordSession.active()).toBe(false);
      expect(passwordSession.refreshToken()).toBeNull();
    }
  });

  it("stop() hủy hẹn và xóa giá trị: không còn gọi làm mới", async () => {
    const onExpired = vi.fn();
    const { api, refresh } = fakeApi([]);
    passwordSession.start(tokens(), { onAccessToken: vi.fn(), onExpired }, api);
    passwordSession.stop();
    expect(passwordSession.refreshToken()).toBeNull();
    await vi.advanceTimersByTimeAsync(600_000);
    expect(refresh).not.toHaveBeenCalled();
    expect(onExpired).not.toHaveBeenCalled();
  });

  it("đăng xuất giữa lúc đang làm mới: kết quả về muộn không hồi sinh phiên", async () => {
    let release: (r: RefreshOutcome) => void = () => undefined;
    const refresh = vi.fn(() => new Promise<RefreshOutcome>((resolve) => (release = resolve)));
    const api: PasswordLoginApi = { login: vi.fn(), refresh, logout: vi.fn() };
    const onAccessToken = vi.fn();
    passwordSession.start(tokens(), { onAccessToken, onExpired: vi.fn() }, api);
    await vi.advanceTimersByTimeAsync(300_000);
    passwordSession.stop();
    release({ kind: "ok", tokens: tokens({ accessToken: "late", refreshToken: "late-rt" }) });
    await vi.advanceTimersByTimeAsync(0);
    expect(onAccessToken).not.toHaveBeenCalled();
    expect(passwordSession.refreshToken()).toBeNull();
  });
});
