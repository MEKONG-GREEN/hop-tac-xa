export const HTX_ROLES = ["platform_admin", "coop_manager", "htx_member"] as const;
export type HtxWorkingRole = typeof HTX_ROLES[number];

let binding = "";
let selected: HtxWorkingRole | null = null;
let available: HtxWorkingRole[] = [];

/** Tab memory only. A different account, tenant or cooperative cannot inherit a selection. */
export const htxWorkingRole = {
  bind(identity: string, roles: readonly string[]) {
    const next = HTX_ROLES.filter((role) => roles.includes(role));
    if (identity !== binding || !selected || !next.includes(selected)) selected = next.length === 1 ? next[0] ?? null : null;
    binding = identity; available = next;
    return { roles: [...available], selected };
  },
  get: () => selected,
  select(role: HtxWorkingRole) { if (!available.includes(role)) throw new Error("Vai trò không thuộc tài khoản"); selected = role; },
  chooseAgain() { selected = null; },
  clear() { binding = ""; selected = null; available = []; },
};

export function withHtxWorkingRole<T extends { roles: string[] }>(principal: T): T {
  const role = htxWorkingRole.get();
  return role ? { ...principal, roles: principal.roles.filter((r) => !HTX_ROLES.includes(r as HtxWorkingRole) || r === role) } : principal;
}
