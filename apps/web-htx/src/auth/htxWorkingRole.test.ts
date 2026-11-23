import { afterEach, expect, it } from "vitest";
import { htxWorkingRole, LAST_ROLE_KEY, withHtxWorkingRole } from "./htxWorkingRole";

afterEach(() => htxWorkingRole.clear());

it("requires a choice for two roles and narrows UI permissions without mutating the token roles", () => {
  const roles = ["coop_manager", "htx_member"];
  expect(htxWorkingRole.bind("tenant:actor:coop", roles).selected).toBeNull();
  htxWorkingRole.select("htx_member");
  expect(withHtxWorkingRole({ roles }).roles).toEqual(["htx_member"]);
  expect(roles).toEqual(["coop_manager", "htx_member"]);
  expect(() => htxWorkingRole.select("platform_admin")).toThrow();
  expect(htxWorkingRole.bind("tenant:actor:coop", roles).selected).toBe("htx_member");
  htxWorkingRole.chooseAgain(); expect(htxWorkingRole.get()).toBeNull();
});

it("does not reuse another actor, tenant, cooperative or revoked role selection", () => {
  for (const next of ["other-tenant:actor:coop", "tenant:other-actor:coop", "tenant:actor:other-coop"]) {
    htxWorkingRole.bind("tenant:actor:coop", ["coop_manager", "htx_member"]); htxWorkingRole.select("htx_member");
    expect(htxWorkingRole.bind(next, ["coop_manager", "htx_member"]).selected).toBeNull();
  }
  expect(htxWorkingRole.bind("single", ["coop_manager"]).selected).toBe("coop_manager");
  expect(htxWorkingRole.bind("single", ["htx_member"]).selected).toBe("htx_member");
});

it("UX-TP-09 (AH-222): remembers the last used role per account without storing the account reference", () => {
  window.localStorage.clear();
  const roles = ["coop_manager", "htx_member"];
  htxWorkingRole.bind("tenant:actor:coop", roles);
  expect(htxWorkingRole.lastUsed()).toBeNull();
  htxWorkingRole.select("htx_member");
  const stored = window.localStorage.getItem(LAST_ROLE_KEY) ?? "";
  expect(stored).toMatch(/^[0-9a-f]{8}\|htx_member$/);
  expect(stored).not.toContain("actor");
  htxWorkingRole.clear();
  // Lần đăng nhập sau: chưa chọn sẵn (vẫn phải chọn), nhưng thẻ được đánh dấu.
  expect(htxWorkingRole.bind("tenant:actor:coop", roles).selected).toBeNull();
  expect(htxWorkingRole.lastUsed()).toBe("htx_member");
  // Tài khoản khác trên cùng máy không thấy dấu của tài khoản trước.
  htxWorkingRole.bind("tenant:other:coop", roles);
  expect(htxWorkingRole.lastUsed()).toBeNull();
  // Vai đã bị ngừng không còn được đánh dấu.
  htxWorkingRole.bind("tenant:actor:coop", ["coop_manager"]);
  expect(htxWorkingRole.lastUsed()).toBeNull();
});

it("FN-TK-01/EX-03 (AH-222): a refreshed token without the selected role drops the selection", () => {
  htxWorkingRole.bind("tenant:actor:coop", ["coop_manager", "htx_member"]);
  htxWorkingRole.select("htx_member");
  expect(htxWorkingRole.bind("tenant:actor:coop", ["coop_manager"]).selected).toBe("coop_manager");
  htxWorkingRole.bind("tenant:actor:coop", ["coop_manager", "htx_member"]);
  htxWorkingRole.select("coop_manager");
  expect(htxWorkingRole.bind("tenant:actor:coop", ["htx_member", "platform_admin"]).selected).toBeNull();
});
