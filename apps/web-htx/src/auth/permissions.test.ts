import { describe, expect, it } from "vitest";
import { HTX_ROLE_MATRIX } from "@/api/generated/htx-roles";
import { effectiveRole, isHtxAppUser, lacksCoop, matrixAllows, matrixRolesOf, rolesWith } from "./permissions";

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
    expect(matrixAllows(as("kn_officer"), "KHMV-FN-03", "C")).toBe(true);
  });
});

describe("ma trận BRD Kế hoạch mùa vụ (Mục VI, dòng KHMV-*, AH-39)", () => {
  const writes = ["C", "U", "D"] as const;
  const rows = ["KHMV-FN-03", "KHMV-FN-05", "KHMV-FN-06"] as const;

  it("vai có hiệu lực theo thứ tự Admin > Đại diện HTX > Thành viên HTX", () => {
    expect(effectiveRole(as("htx_member"))).toBe("htx_member");
    expect(effectiveRole(as("htx_member", "coop_manager"))).toBe("coop_manager");
    expect(effectiveRole(as("htx_member", "platform_admin"))).toBe("platform_admin");
    expect(effectiveRole(as("kn_officer"))).toBeNull();
    expect(effectiveRole(null)).toBeNull();
  });

  it("Thành viên HTX mở vụ, ghi nhật ký, khai sản lượng; Đại diện HTX chỉ xem", () => {
    for (const fn of rows) {
      for (const a of writes) {
        expect(matrixAllows(as("htx_member"), fn, a)).toBe(true);
        expect(matrixAllows(as("coop_manager"), fn, a)).toBe(false);
        expect(matrixAllows(as("platform_admin"), fn, a)).toBe(true);
      }
      expect(matrixAllows(as("coop_manager"), fn, "R")).toBe(true);
    }
    expect(matrixAllows(as("coop_manager"), "KHMV-FN-03-LICH", "C")).toBe(true);
    expect(matrixAllows(as("htx_member"), "KHMV-FN-03-LICH", "C")).toBe(false);
  });

  it("kiêm hai vai dùng quyền Đại diện HTX — không cộng gộp (BRD mục V, tới AH-270)", () => {
    const both = as("htx_member", "coop_manager");
    for (const fn of rows) for (const a of writes) expect(matrixAllows(both, fn, a)).toBe(false);
    expect(matrixAllows(both, "KHMV-FN-03", "R")).toBe(true);
    // Các dòng FN-xx đang dùng giữ kết quả cũ.
    expect(matrixAllows(both, "FN-04", "C")).toBe(true);
    expect(matrixAllows(as("htx_member", "platform_admin"), "FN-02", "R")).toBe(true);
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
