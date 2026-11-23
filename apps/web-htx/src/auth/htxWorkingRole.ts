import { writeStoredValue } from "@/lib/useStoredValue";

export const HTX_ROLES = ["platform_admin", "coop_manager", "htx_member"] as const;
export type HtxWorkingRole = typeof HTX_ROLES[number];

/** UX-TP-09 (AH-222): vai dùng lần trước của tài khoản trên trình duyệt này — chỉ lưu mã băm định danh và tên vai. */
export const LAST_ROLE_KEY = "ag.htx.lastRole";

let binding = "";
let selected: HtxWorkingRole | null = null;
let available: HtxWorkingRole[] = [];

/** FNV-1a 32 bit: không lưu tenant/mã người dùng ở dạng đọc được, chỉ để phân biệt tài khoản trên cùng máy. */
function fingerprint(identity: string): string {
  let h = 0x811c9dc5;
  for (let i = 0; i < identity.length; i++) {
    h ^= identity.charCodeAt(i);
    h = Math.imul(h, 0x01000193) >>> 0;
  }
  return h.toString(16).padStart(8, "0");
}

function readLast(identity: string): HtxWorkingRole | null {
  try {
    const [owner, role] = (window.localStorage.getItem(LAST_ROLE_KEY) ?? "").split("|");
    return owner === fingerprint(identity) && HTX_ROLES.includes(role as HtxWorkingRole) ? (role as HtxWorkingRole) : null;
  } catch {
    return null;
  }
}

/** Tab memory only. A different account, tenant or cooperative cannot inherit a selection. */
export const htxWorkingRole = {
  bind(identity: string, roles: readonly string[]) {
    const next = HTX_ROLES.filter((role) => roles.includes(role));
    if (identity !== binding || !selected || !next.includes(selected)) selected = next.length === 1 ? next[0] ?? null : null;
    binding = identity; available = next;
    return { roles: [...available], selected };
  },
  get: () => selected,
  /** Vai dùng lần trước (UX-TP-09) nếu vẫn còn trong token; chỉ để đánh dấu thẻ, không tự chọn thay người dùng. */
  lastUsed(): HtxWorkingRole | null {
    const role = binding ? readLast(binding) : null;
    return role && available.includes(role) ? role : null;
  },
  select(role: HtxWorkingRole) {
    if (!available.includes(role)) throw new Error("Vai trò không thuộc tài khoản");
    selected = role;
    if (binding) writeStoredValue(LAST_ROLE_KEY, `${fingerprint(binding)}|${role}`);
  },
  chooseAgain() { selected = null; },
  clear() { binding = ""; selected = null; available = []; },
};

export function withHtxWorkingRole<T extends { roles: string[] }>(principal: T): T {
  const role = htxWorkingRole.get();
  return role ? { ...principal, roles: principal.roles.filter((r) => !HTX_ROLES.includes(r as HtxWorkingRole) || r === role) } : principal;
}
