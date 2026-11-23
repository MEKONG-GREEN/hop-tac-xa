// FN-TK-01/EX-03, AC-06 (AH-222): vai làm việc bị ngừng trong lúc đang dùng. Token đã làm mới không còn vai đang chọn
// nên máy chủ trả 409 accounts.working_role_revoked cho request còn gửi X-HTX-Role cũ. Bọc `fetch` dùng chung để nhận
// biết ở một chỗ, như accountLock.ts.
import type { FetchFn } from "./accountLock";

export const WORKING_ROLE_REVOKED_CODE = "accounts.working_role_revoked";
export const WORKING_ROLE_REVOKED_NOTICE = "Vai trò đang dùng đã bị ngừng. Chọn lại vai trò làm việc.";

/**
 * Gọi `onRevoked` khi phản hồi là 409 accounts.working_role_revoked của vai ĐANG chọn: lời gọi cũ gửi trước khi người
 * dùng chọn lại vai về muộn không đưa về màn chọn vai lần nữa. Luôn trả phản hồi gốc, còn nguyên thân.
 */
export function withWorkingRoleRevokedWatch(fetchFn: FetchFn, currentRole: () => string | null, onRevoked: () => void): FetchFn {
  return async (req) => {
    const res = await fetchFn(req);
    if (res.status !== 409) return res;
    const code = await res
      .clone()
      .json()
      .then((b: { code?: unknown }) => b?.code)
      .catch(() => undefined);
    if (code !== WORKING_ROLE_REVOKED_CODE) return res;
    const role = currentRole();
    if (role && req.headers.get("X-HTX-Role") === role) onRevoked();
    return res;
  };
}
