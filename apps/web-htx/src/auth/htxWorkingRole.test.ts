import { afterEach, expect, it } from "vitest";
import { htxWorkingRole, withHtxWorkingRole } from "./htxWorkingRole";

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
