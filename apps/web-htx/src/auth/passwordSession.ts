// Phiên của màn đăng nhập riêng (FN-01, ADR-0060): giữ refresh token và hạn CHỈ trong biến module (bộ nhớ của tab), không
// dùng localStorage, sessionStorage hay cookie. Hẹn làm mới access token trước hạn 30 giây (tối thiểu 5 giây); làm mới
// thất bại thì báo hết phiên. Tải lại trang hoặc mở tab mới thì mất phiên và phải đăng nhập lại (không có cookie SSO Keycloak).
import { passwordLogin, type LoginTokens, type PasswordLoginApi } from "./passwordLogin";

const MARGIN_S = 30;
const MIN_DELAY_S = 5;

export interface PasswordSessionHooks {
  /** Access token mới sau mỗi lần làm mới (đặt vào tokenStore). */
  onAccessToken: (accessToken: string) => void;
  /** Làm mới thất bại hoặc phiên đã hết: về màn đăng nhập. */
  onExpired: () => void;
}

let refreshToken: string | null = null;
let timer: ReturnType<typeof setTimeout> | null = null;
let generation = 0;

function clearTimer(): void {
  if (timer !== null) clearTimeout(timer);
  timer = null;
}

function schedule(expiresIn: number, api: PasswordLoginApi, hooks: PasswordSessionHooks, gen: number): void {
  clearTimer();
  const delayMs = Math.max(expiresIn - MARGIN_S, MIN_DELAY_S) * 1000;
  timer = setTimeout(() => {
    void renew(api, hooks, gen);
  }, delayMs);
}

async function renew(api: PasswordLoginApi, hooks: PasswordSessionHooks, gen: number): Promise<void> {
  const rt = refreshToken;
  if (rt === null || gen !== generation) return;
  const r = await api.refresh(rt);
  if (gen !== generation) return; // đã đăng xuất hoặc đăng nhập lại trong lúc chờ
  if (r.kind !== "ok") {
    passwordSession.stop();
    hooks.onExpired();
    return;
  }
  refreshToken = r.tokens.refreshToken;
  hooks.onAccessToken(r.tokens.accessToken);
  schedule(r.tokens.expiresIn, api, hooks, gen);
}

export const passwordSession = {
  /** Bắt đầu phiên với token vừa nhận; `api` để kiểm thử. */
  start(tokens: LoginTokens, hooks: PasswordSessionHooks, api: PasswordLoginApi = passwordLogin): void {
    passwordSession.stop();
    generation += 1;
    refreshToken = tokens.refreshToken;
    schedule(tokens.expiresIn, api, hooks, generation);
  },
  /** Có phiên của màn mới đang chạy. */
  active: (): boolean => refreshToken !== null,
  /** Refresh token hiện tại (để đăng xuất thu hồi phiên ở Keycloak); không lưu đi đâu khác. */
  refreshToken: (): string | null => refreshToken,
  /** Hủy hẹn và xóa giá trị. */
  stop(): void {
    generation += 1;
    clearTimer();
    refreshToken = null;
  },
};
