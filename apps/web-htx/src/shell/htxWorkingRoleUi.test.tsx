// AH-270 (FN-01/AC-06, AC-07, AC-N1): chọn và đổi vai trò, HTX làm việc của tài khoản Đại diện HTX kiêm Thành viên HTX.
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { MOCK_PERSONAS } from "@/api/mock/personas";
import { AuthContextForTests, type AuthContextValue, type KnSessionState } from "@/auth/AuthProvider";
import { buildPrincipal } from "@/auth/principal";
import { AppShell } from "@/components/AppShell";
import { HtxRolePicker } from "./HtxRolePicker";

vi.mock("next/navigation", () => ({ usePathname: () => "/htx", useRouter: () => ({ replace: vi.fn(), push: vi.fn() }) }));

it("[AC-06] mỗi thẻ vai trò ghi tên HTX làm việc; chọn thẻ gọi đúng vai", async () => {
  const user = userEvent.setup();
  const select = vi.fn(async () => undefined);
  render(<HtxRolePicker roles={["coop_manager", "htx_member"]} select={select} htxName="HTX Tân Phú" />);
  expect(screen.getByRole("heading", { name: "Chọn vai trò và HTX làm việc" })).toBeInTheDocument();
  const cards = screen.getAllByRole("button");
  expect(cards).toHaveLength(2);
  for (const card of cards) expect(card).toHaveTextContent("HTX Tân Phú");
  await user.click(screen.getByRole("button", { name: /Thành viên HTX/ }));
  expect(select).toHaveBeenCalledWith("htx_member");
});

it("[AC-07, AC-N1] đổi vai trò qua menu tài khoản hỏi xác nhận; Hủy giữ nguyên, Tiếp tục về màn chọn", async () => {
  const user = userEvent.setup();
  const p = MOCK_PERSONAS.find((x) => x.id === "htx")!;
  const principal = buildPrincipal({ sub: p.me.actor_ref, name: p.displayName, amr: p.amr, realm_access: { roles: ["coop_manager", "htx_member"] } }, p.me);
  const chooseAgain = vi.fn();
  const value = {
    status: "authenticated",
    principal,
    scopeWarning: null,
    kn: { status: "legacy", assignments: [], context: null, menu: null, badges: null } satisfies KnSessionState,
    dataConsent: { status: "not_required" },
    api: { GET: vi.fn(async () => ({ data: { items: [] }, response: { ok: true, status: 200 } })) },
    htxRoles: { roles: ["coop_manager", "htx_member"], selected: "coop_manager", select: vi.fn(), chooseAgain },
    logout: vi.fn(),
    selectContext: vi.fn(),
    changePassword: vi.fn(),
  } as unknown as AuthContextValue;
  render(<AuthContextForTests.Provider value={value}><AppShell><p>Đang nhập dở</p></AppShell></AuthContextForTests.Provider>);
  await user.click(screen.getByRole("button", { name: /^Tài khoản:/ }));
  await user.click(screen.getByRole("menuitem", { name: "Đổi vai trò, HTX làm việc" }));
  const dialog = screen.getByRole("dialog", { name: "Đổi vai trò, HTX làm việc" });
  expect(within(dialog).getByText("Dữ liệu chưa lưu sẽ mất. Tiếp tục đổi vai trò?")).toBeInTheDocument();
  await user.click(within(dialog).getByRole("button", { name: "Hủy" }));
  expect(chooseAgain).not.toHaveBeenCalled();
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: /^Tài khoản:/ }));
  await user.click(screen.getByRole("menuitem", { name: "Đổi vai trò, HTX làm việc" }));
  await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Tiếp tục" }));
  expect(chooseAgain).toHaveBeenCalledTimes(1);
});

it("[AH-222 UX-TP-09, FN-TK-01/EX-03] thẻ vai dùng lần trước có dấu; vai vừa bị ngừng hiện câu báo", () => {
  render(<HtxRolePicker roles={["coop_manager", "htx_member"]} select={vi.fn()} htxName="HTX Tân Phú" lastUsed="htx_member" notice="Vai trò đang dùng đã bị ngừng. Chọn lại vai trò làm việc." />);
  expect(screen.getByRole("button", { name: /Thành viên HTX/ })).toHaveTextContent("Dùng lần trước");
  expect(screen.getByRole("button", { name: /Quản lý HTX/ })).not.toHaveTextContent("Dùng lần trước");
  expect(screen.getByRole("status")).toHaveTextContent("Vai trò đang dùng đã bị ngừng. Chọn lại vai trò làm việc.");
});

it("[AH-222 FN-TK-01/EX-03, AC-01] còn một vai sau khi bị ngừng → vào thẳng ứng dụng kèm câu báo, đóng được", async () => {
  const user = userEvent.setup();
  const p = MOCK_PERSONAS.find((x) => x.id === "htx")!;
  const principal = buildPrincipal({ sub: p.me.actor_ref, name: p.displayName, amr: p.amr, realm_access: { roles: ["coop_manager"] } }, p.me);
  const clearNotice = vi.fn();
  const value = {
    status: "authenticated",
    principal,
    scopeWarning: null,
    kn: { status: "legacy", assignments: [], context: null, menu: null, badges: null } satisfies KnSessionState,
    dataConsent: { status: "not_required" },
    api: { GET: vi.fn(async () => ({ data: { items: [] }, response: { ok: true, status: 200 } })) },
    htxRoles: { roles: ["coop_manager"], selected: "coop_manager", select: vi.fn(), chooseAgain: vi.fn(), notice: "Vai trò đang dùng đã bị ngừng. Chọn lại vai trò làm việc.", clearNotice },
    logout: vi.fn(),
    selectContext: vi.fn(),
    changePassword: vi.fn(),
  } as unknown as AuthContextValue;
  render(<AuthContextForTests.Provider value={value}><AppShell><p>Trang chủ</p></AppShell></AuthContextForTests.Provider>);
  expect(screen.queryByRole("heading", { name: "Chọn vai trò và HTX làm việc" })).not.toBeInTheDocument();
  const banner = screen.getAllByRole("status").find((n) => n.textContent?.includes("đã bị ngừng"))!;
  await user.click(within(banner).getByRole("button", { name: "Đóng" }));
  expect(clearNotice).toHaveBeenCalledTimes(1);
});
