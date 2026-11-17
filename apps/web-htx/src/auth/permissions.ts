// Ma trận quyền App HTX (FN-05, Mục V — docs/conventions/vai-tro-app-htx.md): ba vai cố định Admin, Đại diện HTX,
// Thành viên HTX. Nguồn duy nhất là contracts/htxroles/htx-roles.v1.json (bản sinh ở src/api/generated).
// CHỈ dùng để ẩn/hiện menu và nút — máy chủ kiểm lại mọi yêu cầu (FN-05/BR-03, TECH-01).
import { HTX_ROLE_MATRIX } from "@/api/generated/htx-roles";
import { hasRole, KN_ROLES, ROLES, type Principal } from "./principal";

export type HtxFunction = (typeof HTX_ROLE_MATRIX.functions)[number]["key"];
export type HtxAction = keyof typeof HTX_ROLE_MATRIX.actions;

const MATRIX_ROLES: readonly string[] = HTX_ROLE_MATRIX.roles.map((r) => r.key);

function grants(role: string, fn: HtxFunction): string {
  const row = HTX_ROLE_MATRIX.functions.find((f) => f.key === fn);
  return (row?.grants as Readonly<Record<string, string>> | undefined)?.[role] ?? "";
}

/** Các vai của ma trận được thao tác action với chức năng fn. */
export function rolesWith(fn: HtxFunction, action: HtxAction): string[] {
  return MATRIX_ROLES.filter((r) => grants(r, fn).includes(action));
}

/** Các vai của ma trận mà người dùng có. */
export function matrixRolesOf(p: Pick<Principal, "roles"> | null): string[] {
  return p ? MATRIX_ROLES.filter((r) => p.roles.includes(r)) : [];
}

/** Người dùng có ít nhất một vai của App HTX. */
export function isHtxAppUser(p: Pick<Principal, "roles"> | null): boolean {
  return matrixRolesOf(p).length > 0;
}

/**
 * Ma trận cho người dùng thao tác action với fn không. Người không có vai nào của ma trận → true (ma trận không quản
 * vai đó, quyền theo danh sách vai sẵn có). Kiêm nhiều vai → hợp quyền, nên kiêm Đại diện HTX và Thành viên HTX dùng
 * quyền Đại diện HTX (FN-05/BR-02).
 */
export function matrixAllows(p: Pick<Principal, "roles"> | null, fn: HtxFunction, action: HtxAction): boolean {
  const roles = matrixRolesOf(p);
  return roles.length === 0 || roles.some((r) => grants(r, fn).includes(action));
}

/**
 * FN-06/EX-02: tài khoản Đại diện HTX / Thành viên HTX (không kèm Admin hay vai có phạm vi rộng hơn) chưa gắn HTX →
 * không có dữ liệu nào trong phạm vi. Máy chủ vẫn tự từ chối; web dùng để hiện thông báo thay cho danh sách.
 */
export function lacksCoop(p: Pick<Principal, "roles" | "orgRef"> | null): boolean {
  return (
    !!p &&
    !p.orgRef &&
    hasRole(p, ROLES.coopManager, ROLES.htxMember) &&
    !hasRole(p, ROLES.platformAdmin, ROLES.registryAdmin, ROLES.cghViewer, ...KN_ROLES)
  );
}
