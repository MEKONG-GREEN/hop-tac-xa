import { render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { MOCK_PERSONAS } from "@/api/mock/personas";
import { buildPrincipal, type Principal } from "@/auth/principal";
import { legacyMenu } from "@/shell/menu";
import { Sidebar } from "@/shell/Sidebar";
import { canSee, disabledModules, findNavItem, homeFor, NAV_GROUPS, visibleNav } from "./nav";

vi.mock("next/navigation", () => ({ usePathname: () => "/", useRouter: () => ({ replace: vi.fn() }) }));

function persona(id: string): Principal {
  const p = MOCK_PERSONAS.find((x) => x.id === id);
  if (!p) throw new Error(id);
  return buildPrincipal({ sub: p.me.actor_ref, name: p.displayName, amr: p.amr, realm_access: { roles: p.me.roles } }, p.me);
}

const hrefs = (p: Principal | null) => visibleNav(p).flatMap((g) => g.items.map((i) => i.href));

describe("menu theo phạm vi và vai", () => {
  it("TW/Tỉnh/Xã thấy màn hình khuyến nông, không thấy cổng Cục và quản trị", () => {
    for (const id of ["tw", "tinh", "xa"]) {
      const h = hrefs(persona(id));
      expect(h).toEqual(expect.arrayContaining(["/", "/htx", "/ban-do", "/nhiem-vu", "/thu-vien"]));
      expect(h).not.toContain("/cgh");
      expect(h.some((x) => x.startsWith("/quan-tri"))).toBe(false);
    }
  });

  it("Cục KTHT chỉ thấy cổng CGH + xuất báo cáo CGH, được đưa thẳng tới /cgh", () => {
    const cuc = persona("cuc");
    expect(hrefs(cuc).sort()).toEqual(["/bao-cao", "/cgh"]);
    expect(homeFor(cuc)).toBe("/cgh");
  });

  it("chỉ đạo, báo cáo cho KN; đồng bộ ngay chỉ cho quản lý tỉnh/TW; hộp thư không nằm trong menu", () => {
    expect(hrefs(persona("xa"))).toEqual(expect.arrayContaining(["/chi-dao", "/bao-cao"]));
    expect(hrefs(persona("xa"))).not.toContain("/dong-bo");
    expect(hrefs(persona("tinh"))).toContain("/dong-bo");
    expect(hrefs(persona("tw"))).toContain("/dong-bo");
    expect(hrefs(persona("setter")).includes("/thong-bao")).toBe(false);
    const inbox = findNavItem("/thong-bao");
    expect(inbox && canSee(inbox, persona("cuc"))).toBe(true);
  });

  it("quản trị tài khoản tỉnh chỉ thấy màn hình tài khoản trong nhóm quản trị", () => {
    const h = hrefs(persona("account-tinh")).filter((x) => x.startsWith("/quan-tri"));
    expect(h).toEqual(["/quan-tri/tai-khoan"]);
  });

  it("platform_admin thấy mọi nhóm, kể cả quản trị", () => {
    const h = hrefs(persona("setter"));
    expect(h).toEqual(expect.arrayContaining(["/", "/cgh", "/quan-tri/danh-muc", "/quan-tri/tham-so", "/quan-tri/to-chuc"]));
  });

  it("param_approver không phải admin: chỉ thêm Tham số, không có Danh mục/Tổ chức", () => {
    const h = hrefs(persona("approver"));
    expect(h).toContain("/quan-tri/tham-so");
    expect(h).not.toContain("/quan-tri/danh-muc");
    expect(h).not.toContain("/quan-tri/to-chuc");
    expect(h).not.toContain("/cgh");
  });

  it("AH-2 AC5: Dữ liệu nền › Danh mục giống chỉ cho quản trị danh mục (platform_admin); vai khác không thấy", () => {
    expect(hrefs(persona("setter"))).toContain("/du-lieu-nen/danh-muc-giong");
    expect(findNavItem("/du-lieu-nen/danh-muc-giong")?.label).toBe("Danh mục giống");
    for (const id of ["htx", "approver", "tw", "tinh", "xa", "cuc"]) expect(hrefs(persona(id)), id).not.toContain("/du-lieu-nen/danh-muc-giong");
    const admin = buildPrincipal({ sub: "m1", realm_access: { roles: ["masterdata_admin"] } }, null);
    expect(hrefs(admin)).toContain("/du-lieu-nen/danh-muc-giong");
    expect(visibleNav(persona("setter")).map((g) => g.label)).toContain("7. Dữ liệu nền");
  });

  it("AH-7 AC5: Dữ liệu nền › Danh mục mùa vụ chỉ cho quản trị danh mục; vai khác không thấy", () => {
    expect(hrefs(persona("setter"))).toContain("/du-lieu-nen/danh-muc-mua-vu");
    expect(findNavItem("/du-lieu-nen/danh-muc-mua-vu")?.label).toBe("Danh mục mùa vụ");
    for (const id of ["htx", "approver", "tw", "tinh", "xa", "cuc"]) expect(hrefs(persona(id)), id).not.toContain("/du-lieu-nen/danh-muc-mua-vu");
    const admin = buildPrincipal({ sub: "m1", realm_access: { roles: ["masterdata_admin"] } }, null);
    expect(hrefs(admin)).toContain("/du-lieu-nen/danh-muc-mua-vu");
  });

  it("GIS US-CFG-01: Quy chuẩn hiển thị bản đồ cho người đặt/duyệt tham số; không cho cán bộ KN", () => {
    expect(hrefs(persona("setter"))).toContain("/quan-tri/hien-thi");
    expect(hrefs(persona("approver"))).toContain("/quan-tri/hien-thi");
    for (const id of ["tw", "tinh", "xa", "cuc", "htx", "account-tinh"]) expect(hrefs(persona(id))).not.toContain("/quan-tri/hien-thi");
  });

  it("GIS US-SYNC-01: Giám sát đồng bộ cho platform_admin / device_admin", () => {
    expect(hrefs(persona("setter"))).toContain("/quan-tri/giam-sat-dong-bo");
    expect(hrefs(persona("approver"))).not.toContain("/quan-tri/giam-sat-dong-bo");
    expect(hrefs(persona("tinh"))).not.toContain("/quan-tri/giam-sat-dong-bo");
    const dev = buildPrincipal({ sub: "d1", realm_access: { roles: ["device_admin"] } }, null);
    expect(hrefs(dev)).toContain("/quan-tri/giam-sat-dong-bo");
    expect(findNavItem("/quan-tri/giam-sat-dong-bo")?.label).toBe("Giám sát đồng bộ");
  });

  it("chưa xác định phạm vi và không có vai → không thấy gì (mặc định chặn)", () => {
    const p = buildPrincipal({ sub: "u1" }, null);
    expect(hrefs(p)).toEqual([]);
    expect(homeFor(p)).toBe("/khong-co-quyen");
    expect(hrefs(null)).toEqual([]);
  });

  it("findNavItem khớp tiền tố dài nhất; trang chi tiết thừa hưởng quyền của danh sách", () => {
    expect(findNavItem("/htx/abc")?.href).toBe("/htx");
    expect(findNavItem("/")?.href).toBe("/");
    expect(findNavItem("/quan-tri/tham-so")?.href).toBe("/quan-tri/tham-so");
    const item = findNavItem("/cgh");
    expect(item && canSee(item, persona("tinh"))).toBe(false);
  });
});

describe("menu App HTX theo vai", () => {
  const HTX_PAGES = [
    "/htx/dashboard",
    "/htx/nong-ho",
    "/htx/lo-ruong",
    "/htx/mua-vu",
    "/htx/nhat-ky",
    "/htx/phe-duyet",
    "/htx/san-luong",
    "/htx/lich-bao-cao",
    "/htx/tai-khoan-nong-dan",
    "/htx/yeu-cau-ho-tro",
    "/htx/thu-gom",
  ];
  const MANAGER_ONLY = ["/htx/phe-duyet", "/htx/lich-bao-cao", "/htx/thu-gom"];

  it("Quản lý HTX chỉ thấy màn hình HTX của mình, vào thẳng bảng điều hành sản xuất", () => {
    const p = persona("htx");
    expect(hrefs(p)).toEqual(HTX_PAGES);
    expect(homeFor(p)).toBe("/htx/dashboard");
  });

  it("cán bộ KN xem được (chỉ đọc) nhưng không có màn hình phê duyệt và lịch báo cáo HTX", () => {
    for (const id of ["xa", "tinh", "tw"]) {
      const h = hrefs(persona(id));
      expect(h).toEqual(expect.arrayContaining(HTX_PAGES.filter((x) => !MANAGER_ONLY.includes(x))));
      for (const x of MANAGER_ONLY) expect(h).not.toContain(x);
    }
    expect(hrefs(persona("cuc")).some((x) => x.startsWith("/htx"))).toBe(false);
  });

  it("AH-24 AC4: nhãn điều hướng tiếng Việt; màn quản lý vụ gọi là Vụ canh tác, không còn Mùa vụ", () => {
    expect(findNavItem("/htx/mua-vu")?.label).toBe("Vụ canh tác");
    for (const id of ["htx", "setter"]) {
      const labels = visibleNav(persona(id)).flatMap((g) => g.items.map((i) => i.label));
      expect(labels).toContain("Vụ canh tác");
      expect(labels).not.toContain("Mùa vụ");
      expect(labels.filter((l) => l.includes("Dashboard"))).toEqual([]);
    }
  });

  it("AH-8: Quy trình SOP nằm ở nhóm Dữ liệu nền, đường dẫn cũ ở Quản trị không còn", () => {
    expect(hrefs(persona("setter"))).toContain("/du-lieu-nen/quy-trinh-sop");
    expect(hrefs(persona("approver"))).toContain("/du-lieu-nen/quy-trinh-sop");
    expect(hrefs(persona("htx"))).not.toContain("/du-lieu-nen/quy-trinh-sop");
    for (const id of ["setter", "approver", "htx"]) expect(hrefs(persona(id))).not.toContain("/quan-tri/quy-trinh");
    const group = visibleNav(persona("setter")).find((g) => g.items.some((i) => i.href === "/du-lieu-nen/quy-trinh-sop"));
    expect(group?.label).toBe("7. Dữ liệu nền");
    expect(findNavItem("/du-lieu-nen/quy-trinh-sop")?.label).toBe("Quy trình SOP");
  });
});

describe("FN-05: menu theo ma trận ba vai App HTX", () => {
  const HTX_PAGES = [
    "/htx/dashboard",
    "/htx/nong-ho",
    "/htx/lo-ruong",
    "/htx/mua-vu",
    "/htx/nhat-ky",
    "/htx/phe-duyet",
    "/htx/san-luong",
    "/htx/lich-bao-cao",
    "/htx/tai-khoan-nong-dan",
    "/htx/yeu-cau-ho-tro",
    "/htx/thu-gom",
  ];
  const withRoles = (...roles: string[]) => buildPrincipal({ sub: "u", realm_access: { roles } }, null);

  it("[FN-05/AC-01] Thành viên HTX chỉ thấy Quản lý nông hộ, không thấy Tài khoản cán bộ, Danh mục dùng chung", () => {
    const member = persona("htx-member");
    expect(hrefs(member)).toEqual(["/htx/nong-ho"]);
    expect(hrefs(member)).not.toContain("/quan-tri/tai-khoan");
    expect(hrefs(member)).not.toContain("/quan-tri/danh-muc");
  });

  it("[FN-05/AC-02] Đại diện HTX không thấy Danh mục dùng chung (Đơn vị hành chính) và vẫn giữ đủ menu HTX", () => {
    const rep = persona("htx");
    expect(hrefs(rep)).not.toContain("/quan-tri/danh-muc");
    expect(hrefs(rep)).toEqual(HTX_PAGES);
    // Kiêm Đại diện HTX và Thành viên HTX (BR-02) → menu như Đại diện HTX.
    const both = { ...rep, roles: [...rep.roles, "htx_member"] };
    expect(hrefs(both)).toEqual(HTX_PAGES);
    // Admin giữ cả hai mục.
    expect(hrefs(persona("setter"))).toEqual(expect.arrayContaining(["/htx/nong-ho", "/quan-tri/tai-khoan", "/quan-tri/danh-muc"]));
  });

  it("[FN-05/AC-03] homeFor(Thành viên HTX) = /htx/nong-ho; canSee(/quan-tri/tai-khoan) = false", () => {
    const member = persona("htx-member");
    expect(homeFor(member)).toBe("/htx/nong-ho");
    const accounts = findNavItem("/quan-tri/tai-khoan");
    expect(accounts && canSee(accounts, member)).toBe(false);
    const units = findNavItem("/quan-tri/danh-muc");
    expect(units && canSee(units, member)).toBe(false);
  });

  it("vai ngoài ma trận giữ danh sách vai sẵn có; vai App HTX bị ma trận chặn dù có vai gốc", () => {
    expect(hrefs(withRoles("account_admin"))).toContain("/quan-tri/tai-khoan");
    expect(hrefs(withRoles("masterdata_admin"))).toContain("/quan-tri/danh-muc");
    expect(hrefs(withRoles("htx_member", "masterdata_admin"))).not.toContain("/quan-tri/danh-muc");
    expect(homeFor(withRoles("htx_member", "plot_viewer"))).toBe("/htx/nong-ho");
  });
});

describe("<Sidebar>", () => {
  // Khung P6: phạm vi người dùng chuyển lên menu tài khoản ở thanh trên (src/shell/AccountMenu.tsx).
  it("menu cũ: hiển thị nhóm đánh số của người dùng tỉnh, mục chi tiết thuộc mục danh sách", () => {
    const p = persona("tinh");
    render(<Sidebar menu={legacyMenu(visibleNav(p))} pathname="/htx/123" />);
    const nav = screen.getByRole("navigation");
    expect(within(nav).getByText("1. Điều hành")).toBeInTheDocument();
    expect(within(nav).queryByText("6. Quản trị")).toBeNull();
    expect(within(nav).queryByRole("link", { name: /Bản đồ CGH/ })).toBeNull();
    expect(within(nav).getByRole("link", { name: /Quản lý HTX/ })).toHaveAttribute("aria-current", "page");
  });

  it("Cục chỉ có cổng CGH và báo cáo", () => {
    const p = persona("cuc");
    render(<Sidebar menu={legacyMenu(visibleNav(p))} pathname="/cgh" />);
    expect(screen.getAllByRole("link")).toHaveLength(2);
    expect(screen.getByRole("link", { name: /Bản đồ CGH/ })).toHaveAttribute("aria-current", "page");
    expect(screen.getByRole("link", { name: /Báo cáo/ })).toBeInTheDocument();
  });
});

describe("W1.4 (R-MOD-08): áº©n mÃ n cá»§a module chÆ°a báº­t cho Ä‘Æ¡n vá»‹", () => {
  const off = (...m: string[]) => disabledModules(m.map((module) => ({ module, enabled: false })));

  it("khÃ´ng táº¯t gÃ¬ â†’ menu nhÆ° cÅ©; táº¯t pf/geo â†’ áº©n báº£n Ä‘á»“, giá»¯ mÃ n lÃµi", () => {
    const tw = persona("tw");
    expect(visibleNav(tw, off())).toEqual(visibleNav(tw));
    const h = visibleNav(tw, off("pf/geo")).flatMap((g) => g.items.map((i) => i.href));
    expect(h).not.toContain("/ban-do");
    expect(h).not.toContain("/ban-do-gis");
    expect(h).toEqual(expect.arrayContaining(["/", "/htx", "/nhiem-vu"]));
  });

  it("canSee cháº·n mÃ n cá»§a module táº¯t (Ä‘Æ°á»ng dáº«n gÃµ tay); Cá»¥c khÃ´ng bá»‹ Ä‘Æ°a tá»›i /cgh khi pf/mechanization táº¯t", () => {
    const cuc = persona("cuc");
    const cgh = findNavItem("/cgh");
    expect(cgh && canSee(cgh, cuc)).toBe(true);
    expect(cgh && canSee(cgh, cuc, off("pf/mechanization"))).toBe(false);
    expect(homeFor(cuc, off("pf/mechanization"))).not.toBe("/cgh");
  });

  it("disabledModules chá»‰ láº¥y module enabled = false; module gáº¯n á»Ÿ nav lÃ  tÃªn repo pf/*", () => {
    expect([...disabledModules([{ module: "pf/geo", enabled: false }, { module: "pf/party", enabled: true }])]).toEqual(["pf/geo"]);
    expect([...disabledModules(undefined)]).toEqual([]);
    for (const g of NAV_GROUPS) for (const i of g.items) if (i.module) expect(i.module).toMatch(/^pf\/[a-z][a-z0-9_]*$/);
  });
});
