// AH-222 (FN-TK-01/EX-03, AC-06): Admin ngừng vai Thành viên HTX trong lúc tài khoản đang dùng vai đó. Token làm mới chỉ
// còn Đại diện HTX; request kế tiếp nhận 409 accounts.working_role_revoked → AuthProvider đọc lại vai từ token, dùng luôn
// vai còn lại (AC-01) và đặt câu báo; request sau đó gửi X-HTX-Role của vai mới.
import { act, render, screen, waitFor } from "@testing-library/react";
import { useEffect } from "react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { MOCK_PERSONAS } from "@/api/mock/personas";
import { AuthProvider, useAuth, type AuthContextValue } from "./AuthProvider";
import { htxWorkingRole } from "./htxWorkingRole";
import { tokenStore } from "./tokenStore";

vi.mock("next/navigation", () => ({ useRouter: () => ({ replace: vi.fn() }) }));
vi.mock("@/config", async (orig) => {
  const actual = ((await orig()) as { config: { oidc: object } }).config;
  return { config: { ...actual, mock: false, surface: "legacy", apiBaseUrl: "https://api.test", adminApiBaseUrl: "https://admin.test", oidc: { ...actual.oidc, issuer: "https://id.test/realms/agrigreen-htx" } } };
});
const jwt = (roles: string[]) => `h.${btoa(JSON.stringify({ sub: "0197a000-0000-7000-8000-000000000001", tenant_id: "t1", org_ref: "o1", realm_access: { roles } })).replace(/=+$/, "")}.s`;
vi.mock("./passwordLogin", () => ({
  passwordLogin: {
    login: vi.fn(async () => ({ kind: "authenticated", tokens: { accessToken: jwt(["coop_manager", "htx_member"]), refreshToken: "r", expiresIn: 300, refreshExpiresIn: 3600 } })),
    refresh: vi.fn(), logout: vi.fn(),
  },
}));
vi.mock("./passwordSession", () => ({ passwordSession: { start: vi.fn(), stop: vi.fn(), active: () => true, refreshToken: () => "r" } }));

const ME = MOCK_PERSONAS.find((p) => p.id === "htx")!.me;
const sent: (string | null)[] = [];
let revoked = false;
const problem = (status: number, code: string) => new Response(JSON.stringify({ status, code, title: code }), { status, headers: { "Content-Type": "application/problem+json" } });
beforeEach(() => {
  sent.length = 0; revoked = false; tokenStore.set(null); htxWorkingRole.clear();
  vi.stubGlobal("fetch", vi.fn(async (input: Request) => {
    const url = new URL(input.url);
    if (url.pathname === "/v1/htx") {
      sent.push(input.headers.get("X-HTX-Role"));
      return revoked && input.headers.get("X-HTX-Role") === "htx_member" ? problem(409, "accounts.working_role_revoked") : new Response("{}", { status: 200, headers: { "Content-Type": "application/json" } });
    }
    if (url.pathname === "/v1/me/data-consent") return new Response(JSON.stringify({ required: false }), { status: 200, headers: { "Content-Type": "application/json" } });
    if (url.pathname === "/v1/me") return new Response(JSON.stringify(ME), { status: 200, headers: { "Content-Type": "application/json" } });
    return problem(404, "not_found");
  }));
});
afterEach(() => vi.unstubAllGlobals());

const probe: { auth: AuthContextValue | null } = { auth: null };
function Probe() {
  const a = useAuth();
  useEffect(() => { probe.auth = a; }, [a]);
  return <p data-testid="role">{`${a.status}|${a.htxRoles?.selected ?? ""}|${a.htxRoles?.notice ?? ""}`}</p>;
}

it("vai đang chọn bị ngừng → dùng vai còn lại kèm câu báo, request sau gửi vai mới", async () => {
  render(<AuthProvider><Probe /></AuthProvider>);
  await act(async () => { await probe.auth!.signIn("dai.dien", "matkhau1"); });
  await waitFor(() => expect(screen.getByTestId("role")).toHaveTextContent("authenticated||"));
  await act(async () => { await probe.auth!.htxRoles!.select("htx_member"); });
  await waitFor(() => expect(screen.getByTestId("role")).toHaveTextContent("authenticated|htx_member|"));

  // Token đã làm mới không còn Thành viên HTX; máy chủ từ chối X-HTX-Role cũ.
  tokenStore.set(jwt(["coop_manager"]));
  revoked = true;
  await act(async () => { await probe.auth!.api.GET("/v1/htx" as never).catch(() => undefined); });
  await waitFor(() => expect(screen.getByTestId("role")).toHaveTextContent("authenticated|coop_manager|Vai trò đang dùng đã bị ngừng. Chọn lại vai trò làm việc."));

  await act(async () => { await probe.auth!.api.GET("/v1/htx" as never).catch(() => undefined); });
  expect(sent).toEqual(["htx_member", "coop_manager"]);
  await act(async () => { probe.auth!.htxRoles!.clearNotice!(); });
  expect(screen.getByTestId("role")).toHaveTextContent("authenticated|coop_manager|");
});
