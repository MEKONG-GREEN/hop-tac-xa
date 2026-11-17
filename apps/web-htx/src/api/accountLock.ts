// Tài khoản bị khóa trong lúc đang dùng (AH-301, FN-01/EX-06): máy chủ trả 403 accounts.account_locked ở MỌI API cần
// đăng nhập. Bọc `fetch` dùng chung cho mọi client (openapi-fetch lẫn client KN dùng fetch thuần) để nhận biết ở một chỗ.

/** Mã problem của máy chủ khi token còn hạn thuộc tài khoản đang Đã khóa (câu EX-03). */
export const ACCOUNT_LOCKED_CODE = "accounts.account_locked";

export type FetchFn = (input: Request) => Promise<Response>;

/**
 * Gọi `onLocked` khi phản hồi là 403 accounts.account_locked của token ĐANG dùng: lời gọi cũ (gửi trước khi đăng xuất
 * hay đăng nhập lại) về muộn không đá phiên mới ra, như nhánh 401 của authMiddleware. Luôn trả phản hồi gốc, còn nguyên thân.
 */
export function withAccountLockWatch(fetchFn: FetchFn, getAccessToken: () => string | null, onLocked: () => void): FetchFn {
  return async (req) => {
    const res = await fetchFn(req);
    if (res.status !== 403) return res;
    // Đọc bản sao: thân phản hồi gốc vẫn nguyên cho người gọi.
    const code = await res
      .clone()
      .json()
      .then((b: { code?: unknown }) => b?.code)
      .catch(() => undefined);
    if (code !== ACCOUNT_LOCKED_CODE) return res;
    const token = getAccessToken();
    if (token && req.headers.get("Authorization") === `Bearer ${token}`) onLocked();
    return res;
  };
}
