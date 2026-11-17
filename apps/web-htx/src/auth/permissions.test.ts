import { describe, expect, it } from "vitest";
import { HTX_ROLE_MATRIX } from "@/api/generated/htx-roles";
import { isHtxAppUser, lacksCoop, matrixAllows, matrixRolesOf, rolesWith } from "./permissions";

const as = (...roles: string[]) => ({ roles });

describe("ma trận quyền App HTX (FN-05, Mục V)", () => {
  it("ba vai cố định, đọc từ bản sinh của contracts/htxroles", () => {
    expect(HTX_ROLE_MATRIX.roles.map((r) => r.key)).toEqual(["platform_admin", "coop_manager", "htx_member"]);
    expect(rolesWith("FN-04", "R")).toEqual(["platform_admin", "coop_manager", "htx_member"]);
    expect(rolesWith("FN-02", "R")).toEqual(["platform_admin"]);
    expect(rolesWith("FN-07", "R")).toEqual(["platform_admin"]);
    expect(rolesWith("FN-15", "X")).toEqual(["platform_admin", "coop_manager"]);
  });

  it("Thành viên HTX chỉ xem; kiêm Đại diện HTX dùng quyền Đại diện HTX (BR-02)", () => {
    expect(matrixAllows(as("htx_member"), "FN-04", "R")).toBe(true);
    for (const a of ["C", "U", "D"] as const) expect(matrixAllows(as("htx_member"), "FN-04", a)).toBe(false);
    expect(matrixAllows(as("htx_member", "coop_manager"), "FN-04", "C")).toBe(true);
    expect(matrixAllows(as("coop_manager"), "FN-02", "R")).toBe(false);
  });

  it("vai ngoài ma trận: ma trận không quản (quyền theo danh sách vai sẵn có)", () => {
    expect(matrixRolesOf(as("kn_officer"))).toEqual([]);
    expect(isHtxAppUser(as("kn_officer"))).toBe(false);
    expect(isHtxAppUser(null)).toBe(false);
    expect(isHtxAppUser(as("htx_member"))).toBe(true);
    expect(matrixAllows(as("kn_officer"), "FN-04", "C")).toBe(true);
  });
});

describe("[FN-06/EX-02] lacksCoop: tài khoản App HTX chưa gắn HTX", () => {
  const who = (orgRef: string | null, ...roles: string[]) => ({ roles, orgRef });

  it("Đại diện HTX / Thành viên HTX không có HTX gắn kèm → true; có HTX → false", () => {
    expect(lacksCoop(who(null, "htx_member"))).toBe(true);
    expect(lacksCoop(who(null, "coop_manager"))).toBe(true);
    expect(lacksCoop(who(null, "coop_manager", "htx_member"))).toBe(true);
    expect(lacksCoop(who("01963011-1234-7123-8123-123456789abc", "htx_member"))).toBe(false);
    expect(lacksCoop(who("01963011-1234-7123-8123-123456789abc", "coop_manager"))).toBe(false);
  });

  it("Admin, vai có phạm vi rộng hơn hoặc ngoài App HTX → false", () => {
    expect(lacksCoop(who(null, "platform_admin"))).toBe(false);
    expect(lacksCoop(who(null, "coop_manager", "kn_officer"))).toBe(false);
    expect(lacksCoop(who(null, "htx_member", "registry_admin"))).toBe(false);
    expect(lacksCoop(who(null, "kn_officer"))).toBe(false);
    expect(lacksCoop(null)).toBe(false);
  });
});
