// Access token hiện tại — CHỈ trong bộ nhớ của tab (không localStorage/sessionStorage/cookie đọc được bằng JS
// ngoài tab). Tải lại trang thì mất. Phiên đăng nhập qua trang Keycloak được khôi phục bằng đăng nhập im lặng qua phiên SSO;
// phiên của màn đăng nhập riêng (FN-01, passwordSession.ts) KHÔNG khôi phục khi tải lại trang hay mở tab mới — phải đăng
// nhập lại, vì không có cookie SSO Keycloak.
let accessToken: string | null = null;

export const tokenStore = {
  get: (): string | null => accessToken,
  set: (t: string | null): void => {
    accessToken = t;
  },
};
