// AH-301 (FN-01/EX-06, AC-N2): máy chủ trả 403 accounts.account_locked ở thao tác kế tiếp → AuthProvider xóa phiên đang
// giữ (token, persona giả lập) và đặt sessionEnd để màn đăng nhập hiện câu EX-03; phủ cả client openapi-fetch lẫn client KN.
import { act, render, screen, waitFor } from "@testing-library/react";
import { useEffect } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ACCOUNT_LOCKED_CODE } from "@/api/accountLock";
import type * as MockHandler from "@/api/mock/handler";
import { AuthProvider, useAuth, type AuthContextValue } from "./AuthProvider";
import { tokenStore } from "./tokenStore";

vi.mock("next/navigation", () => ({ useRouter: () => ({ replace: vi.fn() }) }));
vi.mock("@/config", async (orig) => ({ config: { ...((await orig()) as { config: object }).config, mock: true } }));

const server = vi.hoisted(() => ({ locked: false }));
vi.mock("@/api/mock/handler", async (orig) => {
  const actual = await orig<typeof MockHandler>();
  const lockable =
    (base: (r: Request) => Promise<Response>) =>
    (r: Request): Promise<Response> =>
      server.locked
        ? Promise.resolve(
            new Response(JSON.stringify({ status: 403, code: ACCOUNT_LOCKED_CODE, detail: "Tài khoản đã bị khóa. Liên hệ người đã cấp tài khoản để mở khóa." }), {
              status: 403,
              headers: { "Content-Type": "application/problem+json" },
            }),
          )
        : base(r);
  return { ...actual, mockFetch: lockable(actual.mockFetchNoDelay), mockAdminFetch: lockable(actual.mockAdminFetchNoDelay) };
});

const probe: { auth: AuthContextValue | null } = { auth: null };
function Probe() {
  const a = useAuth();
  useEffect(() => {
    probe.auth = a;
  }, [a]);
  return <p data-testid="status">{`${a.status}|${a.sessionEnd ?? ""}`}</p>;
}
const auth = (): AuthContextValue => probe.auth!;

async function signedIn() {
  render(
    <AuthProvider>
      <Probe />
    </AuthProvider>,
  );
  await waitFor(() => expect(screen.getByTestId("status")).toHaveTextContent("anonymous|"));
  await act(() => auth().loginAsPersona("tw"));
  await waitFor(() => expect(screen.getByTestId("status")).toHaveTextContent("authenticated|"));
  expect(tokenStore.get()).not.toBeNull();
  expect(window.sessionStorage.getItem("ag.mock.persona")).toBe("tw");
}

function expectSignedOutLocked() {
  expect(screen.getByTestId("status")).toHaveTextContent("anonymous|account_locked");
  expect(tokenStore.get()).toBeNull();
  expect(window.sessionStorage.getItem("ag.mock.persona")).toBeNull();
}

beforeEach(() => {
  server.locked = false;
  tokenStore.set(null);
  window.sessionStorage.clear();
});

describe("Tài khoản bị khóa trong lúc đang dùng (AH-301)", () => {
  it("AC-N2: lời gọi openapi-fetch nhận 403 accounts.account_locked → xóa phiên, sessionEnd = account_locked", async () => {
    await signedIn();
    server.locked = true;
    await act(async () => {
      await auth().api.GET("/v1/me");
    });
    await waitFor(expectSignedOutLocked);
  });

  it("AC-N2: client KN (fetch thuần, không qua openapi-fetch) cũng kích hoạt", async () => {
    await signedIn();
    server.locked = true;
    await act(async () => {
      await auth().knApi.badges().catch(() => undefined);
    });
    await waitFor(expectSignedOutLocked);
  });

  it("mở khóa → đăng nhập lại: sessionEnd về null", async () => {
    await signedIn();
    server.locked = true;
    await act(async () => {
      await auth().api.GET("/v1/me");
    });
    await waitFor(expectSignedOutLocked);
    server.locked = false;
    await act(() => auth().loginAsPersona("tw"));
    await waitFor(() => expect(screen.getByTestId("status")).toHaveTextContent("authenticated|"));
  });
});
