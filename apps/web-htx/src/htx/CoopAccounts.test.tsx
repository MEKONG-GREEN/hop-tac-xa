// AH-277 (FN-08/AC-01, AC-02, AC-06, AC-N3): Đại diện HTX quản lý tài khoản Thành viên HTX của HTX mình.
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { createAdminApiClient, createApiClient } from "@/api/client";
import { mockAdminFetchNoDelay, mockFetchNoDelay } from "@/api/mock/handler";
import { resetNativeAccountMock, seedNativeAccount } from "@/api/mock/native-accounts";
import { MOCK_PERSONAS, mockToken } from "@/api/mock/personas";
import type { AccountsUser } from "@/api/types";
import { NativeAccounts } from "./NativeAccounts";

const rep = MOCK_PERSONAS.find((p) => p.id === "htx")!;
const auth = {
  api: createApiClient({ baseUrl: "https://api.test", getAccessToken: () => mockToken("htx"), fetch: mockFetchNoDelay }),
  adminApi: createAdminApiClient({ baseUrl: "https://admin.test", getAccessToken: () => mockToken("htx"), fetch: mockAdminFetchNoDelay }),
  principal: { subject: rep.me.actor_ref, orgRef: rep.me.org!.ref },
};
vi.mock("@/auth/AuthProvider", () => ({ useAuth: () => auth }));

const account = (over: Partial<AccountsUser>): AccountsUser => ({
  id: crypto.randomUUID(), username: "u", first_name: "Tên", last_name: "", phone: "0912000000", email: "", email_verified: false,
  org_ref: rep.me.org!.ref, roles: ["htx_member"], enabled: true, lock_cause: null, required_actions: [], created_at: new Date().toISOString(), tracked: true, ...over,
});

beforeEach(() => {
  resetNativeAccountMock();
  seedNativeAccount(rep.me.tenant_id, account({ id: rep.me.actor_ref, username: "dai-dien", first_name: "Đại diện", phone: "0912000001", roles: ["coop_manager"] }));
  seedNativeAccount(rep.me.tenant_id, account({ username: "tv-mot", first_name: "Thành viên một", phone: "0912000002" }));
  seedNativeAccount(rep.me.tenant_id, account({ username: "tv-khac", first_name: "HTX khác", phone: "0912000003", org_ref: crypto.randomUUID() }));
});

it("[AC-01, AC-02] chỉ thấy Thành viên HTX của HTX mình và dòng của mình; tạo chỉ Thành viên HTX, không chọn HTX", async () => {
  const user = userEvent.setup();
  render(<NativeAccounts scope="coop" />);
  expect(await screen.findByText("tv-mot")).toBeInTheDocument();
  expect(screen.getByText("dai-dien")).toBeInTheDocument();
  expect(screen.queryByText("tv-khac")).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Tạo tài khoản" }));
  const form = screen.getByRole("form", { name: "Tạo tài khoản HTX" });
  expect(within(form).queryByLabelText("Vai trò")).not.toBeInTheDocument();
  expect(within(form).queryByLabelText("HTX")).not.toBeInTheDocument();
  expect(within(form).getByText(/Vai trò: Thành viên HTX/)).toBeInTheDocument();
  await user.type(within(form).getByLabelText("Tên đăng nhập"), "tv-moi");
  await user.type(within(form).getByLabelText("Họ tên"), "Thành viên mới");
  await user.type(within(form).getByLabelText("Số điện thoại"), "0912000009");
  await user.click(within(form).getByRole("button", { name: "Lưu tài khoản" }));
  expect(await screen.findByLabelText("Mật khẩu tạm")).toHaveTextContent(/^[A-Za-z0-9]{8}$/);
});

it("[AC-06, AC-N3] dòng của mình: không có nút khóa, chỉ sửa được ô Kiêm Thành viên HTX", async () => {
  const user = userEvent.setup();
  render(<NativeAccounts scope="coop" />);
  await user.click(await screen.findByRole("button", { name: /Đại diện \(bạn\)/ }));
  const detail = screen.getByRole("region", { name: "Chi tiết tài khoản" });
  expect(within(detail).queryByRole("button", { name: "Khóa" })).not.toBeInTheDocument();
  await user.click(within(detail).getByRole("button", { name: "Sửa" }));
  const form = within(detail).getByRole("form", { name: "Sửa tài khoản HTX" });
  expect(within(form).getByLabelText("Họ tên")).toBeDisabled();
  expect(within(form).getByLabelText("Số điện thoại")).toBeDisabled();
  const dual = within(form).getByLabelText("Kiêm Thành viên HTX");
  expect(dual).toBeEnabled();
  await user.click(dual);
  await user.click(within(form).getByRole("button", { name: "Lưu" }));
  expect(await within(detail).findByText(/Đại diện HTX, Thành viên HTX/)).toBeInTheDocument();
});
