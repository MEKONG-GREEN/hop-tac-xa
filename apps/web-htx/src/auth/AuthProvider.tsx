"use client";

import { useRouter } from "next/navigation";
import type { User } from "oidc-client-ts";
import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { withAccountLockWatch, type FetchFn } from "@/api/accountLock";
import { invalidateUnitResolver } from "@/api/adapters/units";
import { createAdminApiClient, createApiClient, unwrap, type AdminApiClient, type ApiClient } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { mockAdminFetch, mockFetch } from "@/api/mock/handler";
import { LOCK_MINUTES, mockKnLogin } from "@/api/mock/kn";
import { findPersona, mockToken, type MockPersona } from "@/api/mock/personas";
import { ApiProblem } from "@/api/problem";
import type { Me } from "@/api/types";
import { WORKING_ROLE_REVOKED_NOTICE, withWorkingRoleRevokedWatch } from "@/api/workingRoleRevoked";
import { config } from "@/config";
import { createKnApi } from "@/kn/client";
import { createCollectApi, createCycleApi, createKnAdminApi, supportReasonStore, type CollectApi, type CycleApi, type KnAdminApi } from "@/kn/cycle/api";
import type { KnApi, MenuBadges, MenuTree, RoleAssignment, SelectContextRequest, SessionContext } from "@/kn/contract";
import { contextStore, readSelection, writeSelection } from "@/kn/contextStore";
import { autoSelection, isValidSelection } from "@/kn/session";
import { forgetIdentifier } from "@/lib/rememberLogin";
import { buildPrincipal, decodeAccessToken, type AccessTokenClaims, type Principal } from "./principal";
import { htxWorkingRole, withHtxWorkingRole, type HtxWorkingRole } from "./htxWorkingRole";
import { getUserManager, safeReturnTo } from "./oidc";
import { passwordLogin, type FirstPasswordChange, type LoginOutcome } from "./passwordLogin";
import { passwordSession } from "./passwordSession";
import { tokenStore } from "./tokenStore";

export type AuthStatus = "loading" | "anonymous" | "authenticated";

/** Lý do máy chủ vừa kết thúc phiên: "account_locked" = tài khoản bị khóa trong lúc đang dùng (FN-01/EX-06). */
export type SessionEnd = "account_locked" | null;

/**
 * Phiên KN v7.7 (FN-CH-00.01): "legacy" = tài khoản không có vai trò KN hoặc backend chưa có P1
 * (menu cũ, đăng nhập một vai như hiện nay); "choose" = cần màn Chọn vai trò và phòng ban;
 * "active" = đã có ngữ cảnh (vai trò + phòng ban), menu theo cấu hình.
 */
export type KnStatus = "legacy" | "choose" | "active";

export interface KnSessionState {
  status: KnStatus;
  assignments: RoleAssignment[];
  context: SessionContext | null;
  menu: MenuTree | null;
  badges: MenuBadges | null;
  /** Chế độ hỗ trợ (FN-AD-04.07): đang thay cho ai — nhãn hiển thị trên dải vàng, lý do gửi kèm thao tác ghi. */
  support?: { target: string; reason: string } | null;
}

/**
 * Kết quả `signIn`: như LoginOutcome, nhưng đăng nhập thành công không trả token (token chỉ nằm ở tokenStore và
 * passwordSession, không đi ra màn hình).
 */
export type SignInOutcome = Exclude<LoginOutcome, { kind: "authenticated" }> | { kind: "authenticated" };

const KN_LEGACY: KnSessionState = { status: "legacy", assignments: [], context: null, menu: null, badges: null };

/**
 * Đồng ý thu thập ảnh GPS và dữ liệu sản xuất (AH-23, REQ-011, KH-24): hỏi ngay sau đăng nhập, trước khi vào ứng dụng.
 * "unknown" = chưa hỏi; "not_required" = Admin hoặc đã đồng ý phiên bản hiện hành; "required" = phải hiện màn Đồng ý;
 * "error" = không xác định được (fail-closed: vẫn chặn, cho thử lại hoặc đăng xuất).
 */
export type DataConsentState =
  | { status: "unknown" }
  | { status: "not_required" }
  | { status: "required"; version: string; text: string; message?: string }
  | { status: "error"; problem: ApiProblem | null };

const CONSENT_UNKNOWN: DataConsentState = { status: "unknown" };

export interface AuthContextValue {
  status: AuthStatus;
  /** Màn đăng nhập hiện câu EX-03 khi phiên vừa kết thúc vì tài khoản bị khóa (AH-301). */
  sessionEnd?: SessionEnd;
  principal: Principal | null;
  /** /v1/me chưa có hoặc lỗi → phạm vi chưa xác định (chỉ hiện menu theo vai). */
  scopeWarning: string | null;
  /** Application API (api.<domain>). */
  api: ApiClient;
  /** Admin API (host riêng, MFA bắt buộc): danh mục, tham số, cây tổ chức, nhập nông hộ. */
  adminApi: AdminApiClient;
  /** Hợp đồng tạm KN v7.7 (ngữ cảnh phiên, menu theo cấu hình). */
  knApi: KnApi;
  /** Vòng đời báo cáo KN v7.7 (P4) — Application API. */
  cycleApi: CycleApi;
  /** Quản trị KN v7.7 (P1/P2) — Admin API. */
  knAdminApi: KnAdminApi;
  /** Thu thập dữ liệu Cấp Xã KN v7.7 (P7 /v1/kn/*). */
  collectApi: CollectApi;
  kn: KnSessionState;
  htxRoles?: {
    roles: HtxWorkingRole[]; selected: HtxWorkingRole | null; select: (role: HtxWorkingRole) => Promise<void>; chooseAgain: () => void;
    /** UX-TP-09 (AH-222): vai dùng lần trước trên trình duyệt này — chỉ để đánh dấu thẻ. */
    lastUsed?: HtxWorkingRole | null;
    /** FN-TK-01/EX-03 (AH-222): câu báo vai vừa bị ngừng; null khi không có. */
    notice?: string | null;
    clearNotice?: () => void;
  };
  /** Trạng thái đồng ý thu thập dữ liệu của phiên (GET /v1/me/data-consent). */
  dataConsent: DataConsentState;
  /** Ghi đồng ý phiên bản `version` (POST /v1/me/data-consent); gọi trong `useIdempotentAction().run`. */
  acceptDataConsent: (version: string) => Promise<void>;
  /** Hỏi lại trạng thái đồng ý (nút Thử lại; máy chủ trả 403 accounts.data_consent_required). */
  refreshDataConsent: () => Promise<void>;
  /** Làm mới tên/đơn vị của phiên sau khi sửa hồ sơ HTX mình. */
  refreshPrincipal?: () => Promise<void>;
  /** FN-AD-04.07: vào menu vai trò khác với ngữ cảnh hỗ trợ (POST /v1/accounts/support-sessions). */
  enterSupport: (ctx: SessionContext, target: string, reason: string) => Promise<void>;
  /** Thoát chế độ hỗ trợ → về ngữ cảnh Admin. */
  exitSupport: () => Promise<void>;
  /** Sửa lý do của các thao tác ghi tiếp theo trong chế độ hỗ trợ. */
  setSupportReason: (reason: string) => void;
  /** Chọn/đổi vai trò + phòng ban (không cần đăng nhập lại). Trả ngữ cảnh mới (default_route). */
  selectContext: (sel: SelectContextRequest) => Promise<SessionContext>;
  /**
   * Chuyển hướng tới Keycloak (luồng OIDC/PKCE); `loginHint` = tên đăng nhập/SĐT người dùng gõ ở thẻ Đăng nhập. Dùng cho
   * nút hoàn tất việc lần đầu (đổi mật khẩu tạm, đăng ký OTP) trên trang xác thực.
   */
  login: (returnTo?: string, loginHint?: string) => Promise<void>;
  /**
   * Đăng nhập ở màn riêng (FN-01): tên đăng nhập hoặc SĐT + mật khẩu (+ OTP) gửi về máy chủ. `authenticated` → phiên đã
   * thiết lập (trang đăng nhập tự điều hướng vào trang đầu tiên được xem). Chế độ giả lập: mật khẩu thử (tài khoản KN v7.7).
   */
  /** `challengeId` có giá trị là bước 2 của Admin (ADR-0061): `otp` là mã trong email. */
  signIn: (identifier: string, password: string, otp?: string, challengeId?: string, change?: FirstPasswordChange) => Promise<SignInOutcome>;
  /** Chỉ chế độ giả lập: đăng nhập bằng persona. */
  loginAsPersona: (personaId: string) => Promise<void>;
  /** Đổi mật khẩu: Keycloak kc_action=UPDATE_PASSWORD (chế độ giả lập: không làm gì). */
  changePassword: () => Promise<void>;
  logout: () => Promise<void>;
}

const AuthContext = createContext<AuthContextValue | null>(null);

const MOCK_KEY = "ag.mock.persona";

function readMockPersona(): string | null {
  try {
    return window.sessionStorage.getItem(MOCK_KEY);
  } catch {
    return null;
  }
}

function writeMockPersona(id: string | null): void {
  try {
    if (id) window.sessionStorage.setItem(MOCK_KEY, id);
    else window.sessionStorage.removeItem(MOCK_KEY);
  } catch {
    // bỏ qua: chỉ mất khả năng nhớ persona khi tải lại
  }
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const router = useRouter();
  const [status, setStatus] = useState<AuthStatus>("loading");
  const [sessionEnd, setSessionEnd] = useState<SessionEnd>(null);
  const [principal, setPrincipal] = useState<Principal | null>(null);
  const [scopeWarning, setScopeWarning] = useState<string | null>(null);
  const [kn, setKn] = useState<KnSessionState>(KN_LEGACY);
  const [htxRoles,setHtxRoles] = useState<{roles:HtxWorkingRole[];selected:HtxWorkingRole|null;notice?:string|null}>({roles:[],selected:null});
  // FN-TK-01/EX-03: xử lý 409 accounts.working_role_revoked; gán trong effect vì cần loadPrincipal (dựng sau client).
  const workingRoleRevoked = useRef<() => void>(() => undefined);
  const [dataConsent, setDataConsent] = useState<DataConsentState>(CONSENT_UNKNOWN);
  const claimsRef = useRef<AccessTokenClaims>({});
  // Máy chủ chặn route ảnh/GPS bằng 403 accounts.data_consent_required → tăng số này để hỏi lại trạng thái (client dựng
  // trước hàm tải nên không gọi thẳng được).
  const [consentPing, setConsentPing] = useState(0);

  const onUnauthenticated = useCallback(() => {
    htxWorkingRole.clear();setHtxRoles({roles:[],selected:null});
    passwordSession.stop();
    tokenStore.set(null);
    contextStore.set(null);
    setPrincipal(null);
    setKn(KN_LEGACY);
    setDataConsent(CONSENT_UNKNOWN);
    setStatus("anonymous");
  }, []);

  const onDataConsentRequired = useCallback(() => setConsentPing((n) => n + 1), []);

  /**
   * AH-301 (FN-01/EX-06): máy chủ trả 403 accounts.account_locked cho token đang dùng → xóa phiên đang giữ; AppShell
   * đưa về /login và màn đăng nhập hiện câu EX-03.
   */
  const onAccountLocked = useCallback(() => {
    // Ghi nhận trước khi dọn: onUnauthenticated dừng passwordSession.
    const oidc = !config.mock && !passwordSession.active();
    setSessionEnd("account_locked");
    onUnauthenticated();
    supportReasonStore.set(null);
    // Tải lại trang không được khôi phục user OIDC cũ.
    if (oidc) void getUserManager().removeUser().catch(() => undefined);
    if (config.mock) writeMockPersona(null);
  }, [onUnauthenticated]);

  const clients = useMemo(() => {
    const getAccessToken = tokenStore.get;
    const getContextId = contextStore.get;
    // Một lớp bọc cho cả 6 client: client KN dùng fetch thuần, không qua middleware của openapi-fetch.
    const watch = (base: FetchFn): FetchFn => withAccountLockWatch((request) => {
      const role=htxWorkingRole.get();
      if (!role) return base(request);
      const scoped=new Request(request);scoped.headers.set("X-HTX-Role",role);
      return withWorkingRoleRevokedWatch(base, htxWorkingRole.get, () => workingRoleRevoked.current())(scoped);
    }, getAccessToken, onAccountLocked);
    const appFetch = watch(config.mock ? mockFetch : (r) => fetch(r));
    const adminFetch = watch(config.mock ? mockAdminFetch : (r) => fetch(r));
    return {
      api: createApiClient({
        baseUrl: config.apiBaseUrl,
        getAccessToken,
        getContextId,
        onUnauthenticated,
        onDataConsentRequired,
        fetch: appFetch,
      }),
      adminApi: createAdminApiClient({
        baseUrl: config.adminApiBaseUrl,
        getAccessToken,
        getContextId,
        onUnauthenticated,
        fetch: adminFetch,
      }),
      knApi: createKnApi({ baseUrl: config.apiBaseUrl, getAccessToken, getContextId, fetch: appFetch }),
      cycleApi: createCycleApi({ baseUrl: config.apiBaseUrl, getAccessToken, getContextId, fetch: appFetch }),
      collectApi: createCollectApi({ baseUrl: config.apiBaseUrl, getAccessToken, getContextId, fetch: appFetch }),
      knAdminApi: createKnAdminApi({ baseUrl: config.adminApiBaseUrl, getAccessToken, getContextId, fetch: adminFetch }),
    };
  }, [onUnauthenticated, onDataConsentRequired, onAccountLocked]);

  /** GET /v1/me → Principal (gọi lại sau khi đổi ngữ cảnh: vai và phạm vi theo lần gán đã chọn). */
  const loadPrincipal = useCallback(async () => {
    invalidateUnitResolver(clients.api);
    let me: Me | null = null;
    try {
      me = await unwrap(clients.api.GET("/v1/me"));
      setScopeWarning(null);
    } catch (e) {
      const p = e instanceof ApiProblem ? e : null;
      setScopeWarning(
        `Chưa lấy được phạm vi dữ liệu (GET /v1/me${p ? `: ${p.message}${p.traceId ? ` — mã tra cứu ${p.traceId}` : ""}` : ""}). Menu chỉ hiển thị theo vai; dữ liệu vẫn được máy chủ lọc theo phạm vi.`,
      );
    }
    const p = withHtxWorkingRole(buildPrincipal(claimsRef.current, me));
    setPrincipal(p);
    return p;
  }, [clients]);

  /** GET /v1/me/data-consent → trạng thái cho màn Đồng ý. Lỗi → "error" (chặn, không tự cho qua). */
  const fetchDataConsent = useCallback(
    async (message?: string): Promise<DataConsentState> => {
      try {
        const r = await unwrap(clients.api.GET("/v1/me/data-consent"));
        if (!r.required) return { status: "not_required" };
        if (r.version && r.text) return { status: "required", version: r.version, text: r.text, ...(message ? { message } : {}) };
        return { status: "error", problem: null };
      } catch (e) {
        return { status: "error", problem: e instanceof ApiProblem ? e : null };
      }
    },
    [clients],
  );

  const loadDataConsent = useCallback(
    async (message?: string) => {
      setDataConsent(await fetchDataConsent(message));
    },
    [fetchDataConsent],
  );

  useEffect(() => {
    if (consentPing === 0) return;
    let alive = true;
    void fetchDataConsent().then((next) => {
      if (alive) setDataConsent(next);
    });
    return () => {
      alive = false;
    };
  }, [consentPing, fetchDataConsent]);

  /** POST /v1/me/data-consent: lời gọi ghi khởi phát đồng bộ để nằm trong phiên idempotency của `run`. */
  const acceptDataConsent = useCallback(
    async (version: string) => {
      const call = clients.api.POST("/v1/me/data-consent", {
        // Trong useIdempotentAction().run, middleware thay khóa này bằng khóa của phiên thao tác.
        params: { header: { "Idempotency-Key": newIdempotencyKey() } },
        body: { version },
      });
      try {
        await unwrap(call);
      } catch (e) {
        if (e instanceof ApiProblem && e.code === "accounts.data_consent_version_stale") {
          // Nội dung vừa đổi: tải lại bản mới kèm thông báo, người dùng đọc lại rồi xác nhận.
          await loadDataConsent(e.message);
          return;
        }
        throw e;
      }
      await loadDataConsent();
    },
    [clients, loadDataConsent],
  );

  const refreshDataConsent = useCallback(() => loadDataConsent(), [loadDataConsent]);

  /** POST /v1/me/context → header X-AG-Context cho mọi lời gọi sau; tải menu + số đếm của ngữ cảnh. */
  const activate = useCallback(
    async (sel: SelectContextRequest, assignments: RoleAssignment[]): Promise<SessionContext> => {
      const ctx = await clients.knApi.selectContext(sel);
      contextStore.set(ctx.context_id);
      writeSelection(sel);
      const [menu, badges] = await Promise.all([
        clients.knApi.menu(),
        clients.knApi.badges().catch((): MenuBadges => ({ counts: {} })), // số đếm lỗi: ẩn số, không chặn menu
      ]);
      await loadPrincipal();
      setKn({ status: "active", assignments, context: ctx, menu, badges });
      return ctx;
    },
    [clients, loadPrincipal],
  );

  /** Ngữ cảnh KN sau đăng nhập. Backend chưa có P1 (404/lỗi) → giữ đăng nhập một vai như hiện nay. */
  const resolveKn = useCallback(async () => {
    if (config.surface === "legacy") return setKn(KN_LEGACY);
    let assignments: RoleAssignment[];
    try {
      assignments = (await clients.knApi.listContexts()).assignments ?? [];
    } catch {
      return setKn(KN_LEGACY);
    }
    if (!assignments.length) return setKn(KN_LEGACY);
    const stored = readSelection();
    const sel = isValidSelection(assignments, stored) ? stored : autoSelection(assignments);
    if (sel) {
      try {
        await activate(sel, assignments);
        return;
      } catch {
        writeSelection(null); // lựa chọn cũ không còn hợp lệ → cho chọn lại
      }
    }
    setKn({ status: "choose", assignments, context: null, menu: null, badges: null });
  }, [clients, activate]);

  const establish = useCallback(
    async (accessToken: string, mock?: MockPersona) => {
      setSessionEnd(null);
      tokenStore.set(accessToken);
      contextStore.set(null);
      claimsRef.current = mock
        ? { sub: mock.me.actor_ref, name: mock.displayName, amr: mock.amr, realm_access: { roles: mock.me.roles } }
        : decodeAccessToken(accessToken);
      if (config.oidc.issuer.replace(/\/$/, "").endsWith("/realms/agrigreen-htx")) {
        const c=claimsRef.current;
        setHtxRoles(htxWorkingRole.bind(`${c.tenant_id ?? ""}:${c.sub ?? ""}:${c.org_ref ?? ""}`,c.realm_access?.roles ?? []));
      } else { htxWorkingRole.clear();setHtxRoles({roles:[],selected:null}); }
      await loadPrincipal();
      await loadDataConsent();
      await resolveKn();
      setStatus("authenticated");
    },
    [loadPrincipal, loadDataConsent, resolveKn],
  );

  useEffect(() => {
    let cancelled = false;
    if (config.mock) {
      void (async () => {
        const persona = findPersona(readMockPersona());
        if (persona) await establish(mockToken(persona.id), persona);
        else if (!cancelled) setStatus("anonymous");
      })();
      return () => {
        cancelled = true;
      };
    }
    const um = getUserManager();
    const onLoaded = (user: User) => {
      if (!cancelled) void establish(user.access_token);
    };
    const onExpired = () => {
      if (!cancelled) onUnauthenticated();
    };
    um.events.addUserLoaded(onLoaded);
    um.events.addAccessTokenExpired(onExpired);
    um.events.addUserSignedOut(onExpired);

    // Trang callback/silent tự xử lý; nơi khác thử khôi phục phiên SSO im lặng.
    const path = window.location.pathname;
    if (!path.startsWith("/login/callback") && !path.startsWith("/login/silent")) {
      (async () => {
        const user = await um.getUser();
        if (user && !user.expired) return onLoaded(user);
        try {
          const renewed = await um.signinSilent();
          if (renewed && !cancelled) return; // userLoaded đã kích hoạt establish
        } catch {
          // không có phiên SSO → cần đăng nhập
        }
        if (!cancelled) setStatus("anonymous");
      })();
    }
    return () => {
      cancelled = true;
      um.events.removeUserLoaded(onLoaded);
      um.events.removeAccessTokenExpired(onExpired);
      um.events.removeUserSignedOut(onExpired);
    };
  }, [establish, onUnauthenticated]);

  const login = useCallback(async (returnTo?: string, loginHint?: string) => {
    if (config.mock) return;
    const hint = loginHint?.trim();
    await getUserManager().signinRedirect({
      state: { returnTo: safeReturnTo(returnTo) },
      ...(hint ? { login_hint: hint } : {}),
    });
  }, []);

  const loginAsPersona = useCallback(
    async (personaId: string) => {
      const persona = findPersona(personaId);
      if (!config.mock || !persona) return;
      writeMockPersona(persona.id);
      writeSelection(null);
      setStatus("loading");
      await establish(mockToken(persona.id), persona);
    },
    [establish],
  );

  const signIn = useCallback(
    async (identifier: string, password: string, otp?: string, challengeId?: string, change?: FirstPasswordChange): Promise<SignInOutcome> => {
      if (config.mock) {
        const r = mockKnLogin(identifier, password);
        if (r.ok) {
          await loginAsPersona(r.personaId);
          return { kind: "authenticated" };
        }
        return r.error === "locked" ? { kind: "locked", minutes: LOCK_MINUTES } : { kind: "invalid", remaining: r.remaining };
      }
      const outcome = await passwordLogin.login(identifier, password, otp, challengeId, change);
      if (outcome.kind !== "authenticated") return outcome;
      htxWorkingRole.clear(); // A fresh login asks dual-role users again; token refresh preserves a choice.
      // Token (kể cả refresh token) chỉ ở bộ nhớ của tab; làm mới trước hạn, thất bại thì về màn đăng nhập.
      passwordSession.start(outcome.tokens, { onAccessToken: tokenStore.set, onExpired: onUnauthenticated });
      await establish(outcome.tokens.accessToken);
      return { kind: "authenticated" };
    },
    [loginAsPersona, establish, onUnauthenticated],
  );

  const selectContext = useCallback(
    (sel: SelectContextRequest) => {
      supportReasonStore.set(null);
      return activate(sel, kn.assignments);
    },
    [activate, kn.assignments],
  );

  const enterSupport = useCallback(
    async (ctx: SessionContext, target: string, reason: string) => {
      contextStore.set(ctx.context_id);
      supportReasonStore.set(reason);
      const [menu, badges] = await Promise.all([clients.knApi.menu(), clients.knApi.badges().catch((): MenuBadges => ({ counts: {} }))]);
      await loadPrincipal();
      setKn((k) => ({ status: "active", assignments: k.assignments, context: ctx, menu, badges, support: { target, reason } }));
    },
    [clients, loadPrincipal],
  );

  const exitSupport = useCallback(async () => {
    const id = kn.context?.support_session_id;
    if (id) await clients.knAdminApi.endSupport(id).catch(() => undefined);
    supportReasonStore.set(null);
    contextStore.set(null); // ngữ cảnh hỗ trợ đã hết hiệu lực — không gửi kèm khi chọn lại ngữ cảnh Admin
    const sel = readSelection() ?? autoSelection(kn.assignments);
    if (sel) await activate(sel, kn.assignments);
  }, [clients, kn.context, kn.assignments, activate]);

  const setSupportReason = useCallback((reason: string) => {
    supportReasonStore.set(reason);
    setKn((k) => (k.support ? { ...k, support: { ...k.support, reason } } : k));
  }, []);

  const changePassword = useCallback(async () => {
    if (config.mock) return;
    await getUserManager().signinRedirect({ extraQueryParams: { kc_action: "UPDATE_PASSWORD" }, state: { returnTo: safeReturnTo(window.location.pathname) } });
  }, []);

  const logout = useCallback(async () => {
    // Phiên của màn đăng nhập riêng: thu hồi phiên ở Keycloak qua máy chủ (bỏ qua lỗi), rồi xóa trạng thái cục bộ.
    const passwordRt = passwordSession.refreshToken();
    if (passwordRt) await passwordLogin.logout(passwordRt);
    passwordSession.stop();
    supportReasonStore.set(null);
    tokenStore.set(null);
    contextStore.set(null);
    writeSelection(null);
    htxWorkingRole.clear();setHtxRoles({roles:[],selected:null});
    forgetIdentifier(); // M19: email "Ghi nhớ đăng nhập" không ở lại sau khi đăng xuất
    setPrincipal(null);
    setKn(KN_LEGACY);
    setDataConsent(CONSENT_UNKNOWN);
    setStatus("anonymous");
    if (config.mock) {
      writeMockPersona(null);
      return;
    }
    if (passwordRt) {
      // Token, refresh token và ngữ cảnh đã xóa ở trên; về màn đăng nhập (không có trang Keycloak để chuyển tới).
      router.replace("/login");
      return;
    }
    const um = getUserManager();
    const user = await um.getUser();
    await um.removeUser();
    await um.signoutRedirect(user?.id_token ? { id_token_hint: user.id_token } : undefined);
  }, [router]);

  const selectHtxRole=useCallback(async (role:HtxWorkingRole)=>{
    htxWorkingRole.select(role);
    await loadPrincipal();
    setHtxRoles((previous)=>({...previous,selected:role,notice:null}));
  },[loadPrincipal]);
  const chooseHtxRoleAgain=useCallback(()=>{htxWorkingRole.chooseAgain();setHtxRoles((previous)=>({...previous,selected:null,notice:null}));},[]);
  const clearHtxNotice=useCallback(()=>setHtxRoles((previous)=>(previous.notice ? {...previous,notice:null} : previous)),[]);

  // FN-TK-01/EX-03, AC-06 (AH-222): vai đang chọn đã bị ngừng (token làm mới không còn vai đó). Đọc lại vai từ token
  // hiện tại: còn hai vai → màn Chọn vai trò; còn một vai → tự dùng vai đó (AC-01); kèm câu báo.
  useEffect(() => {
    workingRoleRevoked.current = () => {
      const token = tokenStore.get();
      if (!token) return;
      const c = decodeAccessToken(token);
      claimsRef.current = c;
      const next = htxWorkingRole.bind(`${c.tenant_id ?? ""}:${c.sub ?? ""}:${c.org_ref ?? ""}`, c.realm_access?.roles ?? []);
      setHtxRoles({ ...next, notice: WORKING_ROLE_REVOKED_NOTICE });
      void loadPrincipal();
    };
  }, [loadPrincipal]);

  const value = useMemo<AuthContextValue>(
    () => ({
      status,
      sessionEnd,
      principal,
      scopeWarning,
      api: clients.api,
      adminApi: clients.adminApi,
      knApi: clients.knApi,
      cycleApi: clients.cycleApi,
      knAdminApi: clients.knAdminApi,
      collectApi: clients.collectApi,
      kn,
      htxRoles:{...htxRoles,select:selectHtxRole,chooseAgain:chooseHtxRoleAgain,lastUsed:htxWorkingRole.lastUsed(),clearNotice:clearHtxNotice},
      dataConsent,
      acceptDataConsent,
      refreshDataConsent,
      refreshPrincipal: async () => { await loadPrincipal(); },
      enterSupport,
      exitSupport,
      setSupportReason,
      selectContext,
      login,
      signIn,
      loginAsPersona,
      changePassword,
      logout,
    }),
    [status, sessionEnd, principal, scopeWarning, clients, kn, htxRoles,selectHtxRole,chooseHtxRoleAgain,clearHtxNotice,dataConsent, acceptDataConsent, refreshDataConsent, loadPrincipal, enterSupport, exitSupport, setSupportReason, selectContext, login, signIn, loginAsPersona, changePassword, logout],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error("useAuth phải nằm trong <AuthProvider>");
  return ctx;
}

/** Cho kiểm thử/Storybook: cung cấp context cố định. */
export const AuthContextForTests = AuthContext;
