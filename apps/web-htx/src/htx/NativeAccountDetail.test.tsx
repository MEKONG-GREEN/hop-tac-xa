import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { createAdminApiClient, createApiClient, unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { COOPERATIVES } from "@/api/mock/data";
import { mockAdminFetchNoDelay, mockFetchNoDelay } from "@/api/mock/handler";
import { resetNativeAccountMock, seedNativeAccount } from "@/api/mock/native-accounts";
import { MOCK_PERSONAS, mockToken } from "@/api/mock/personas";
import type { AccountsUser } from "@/api/types";
import { NativeAccountDetail } from "./NativeAccountDetail";

// AH-276 (FN-07/AC-06, AC-07, AC-08, QTPQ-Q-21..Q-23).
const setter = MOCK_PERSONAS.find((p) => p.id === "setter")!;
const auth = {
  api: createApiClient({ baseUrl: "https://api.test", getAccessToken: () => mockToken("setter"), fetch: mockFetchNoDelay }),
  adminApi: createAdminApiClient({ baseUrl: "https://admin.test", getAccessToken: () => mockToken("setter"), fetch: mockAdminFetchNoDelay }),
  principal: { subject: setter.me.actor_ref },
};
vi.mock("@/auth/AuthProvider", () => ({ useAuth: () => auth }));
beforeEach(resetNativeAccountMock);

const coops = COOPERATIVES.filter((c) => c.api.active).map((c) => c.api);
async function createRep(): Promise<AccountsUser> {
  const res = await unwrap(auth.adminApi.POST("/v1/accounts/htx-users", { params: { header: { "Idempotency-Key": newIdempotencyKey() } }, body: { username: "edit-rep", full_name: "Đại diện cũ", phone: "0912000001", cooperative_ref: coops[0]!.ref, roles: ["coop_manager", "htx_member"] } }));
  return res.user;
}

it("[AC-06, QTPQ-Q-21] edits name and cooperative; username and main role are read-only", async () => {
  const account = await createRep();
  const onChanged = vi.fn();
  const user = userEvent.setup();
  render(<NativeAccountDetail user={account} coops={coops} onChanged={onChanged} onClose={() => {}} />);
  await user.click(screen.getByRole("button", { name: "Sửa" }));
  const form = screen.getByRole("form", { name: "Sửa tài khoản HTX" });
  expect(within(form).getByLabelText("Tên đăng nhập")).toBeDisabled();
  expect(within(form).getByLabelText("Vai trò")).toBeDisabled();
  await user.clear(within(form).getByLabelText("Họ tên"));
  await user.type(within(form).getByLabelText("Họ tên"), "Đại diện mới");
  await user.selectOptions(within(form).getByLabelText("HTX"), coops[1]!.ref);
  await user.click(within(form).getByRole("button", { name: "Lưu" }));
  expect(await screen.findByRole("status")).toHaveTextContent("Tài khoản phải đăng nhập lại và chỉ thấy dữ liệu HTX mới.");
  const saved = onChanged.mock.calls.at(-1)![0] as AccountsUser;
  expect(saved.org_ref).toBe(coops[1]!.ref);
  expect(saved.roles).toEqual(["coop_manager", "htx_member"]);
  expect(saved.username).toBe("edit-rep");
});

it("[EX-04] shows the phone format message at the field", async () => {
  const account = await createRep();
  const user = userEvent.setup();
  render(<NativeAccountDetail user={account} coops={coops} onChanged={() => {}} onClose={() => {}} />);
  await user.click(screen.getByRole("button", { name: "Sửa" }));
  const form = screen.getByRole("form", { name: "Sửa tài khoản HTX" });
  await user.clear(within(form).getByLabelText("Số điện thoại"));
  await user.type(within(form).getByLabelText("Số điện thoại"), "12345");
  await user.click(within(form).getByRole("button", { name: "Lưu" }));
  const messages = await within(form).findAllByText("SĐT gồm 10 chữ số, bắt đầu bằng 0");
  expect(messages.some((m) => m.classList.contains("err"))).toBe(true);
});

it("[AC-07] locks and unlocks another account", async () => {
  const account = await createRep();
  const onChanged = vi.fn();
  const user = userEvent.setup();
  const { rerender } = render(<NativeAccountDetail user={account} coops={coops} onChanged={onChanged} onClose={() => {}} />);
  await user.click(screen.getByRole("button", { name: "Khóa" }));
  expect(await screen.findByRole("status")).toHaveTextContent("Đã khóa");
  const locked = onChanged.mock.calls.at(-1)![0] as AccountsUser;
  expect(locked.enabled).toBe(false);
  rerender(<NativeAccountDetail user={locked} coops={coops} onChanged={onChanged} onClose={() => {}} />);
  await user.click(screen.getByRole("button", { name: "Mở khóa" }));
  expect(await screen.findByRole("status")).toHaveTextContent("Đã mở khóa");
  expect((onChanged.mock.calls.at(-1)![0] as AccountsUser).enabled).toBe(true);
});

it("[AC-08, BR-07] own account shows (bạn) and has no lock button", () => {
  const own: AccountsUser = { id: setter.me.actor_ref, username: "admin-self", first_name: "Admin", last_name: "", phone: "0912000009", email: "a@b.vn", email_verified: true, org_ref: null, roles: ["platform_admin"], enabled: true, lock_cause: null, required_actions: [], created_at: new Date().toISOString(), tracked: true };
  seedNativeAccount(setter.me.tenant_id, own);
  render(<NativeAccountDetail user={own} coops={coops} onChanged={() => {}} onClose={() => {}} />);
  expect(screen.getByText("Admin (bạn)")).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Khóa" })).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Sửa" })).toBeInTheDocument();
});

it("[AH-281, FN-10/AC-05] resets the password after confirmation and shows the temporary password once", async () => {
  const account = await createRep();
  const user = userEvent.setup();
  render(<NativeAccountDetail user={account} coops={coops} onChanged={() => {}} onClose={() => {}} />);
  await user.click(screen.getByRole("button", { name: "Đặt lại mật khẩu" }));
  const dialog = screen.getByRole("dialog", { name: "Đặt lại mật khẩu" });
  await user.click(within(dialog).getByRole("button", { name: "Đặt lại" }));
  const status = await screen.findByRole("status");
  expect(within(status).getByLabelText("Mật khẩu tạm").textContent).toMatch(/^[0-9a-f]{8}$/);
  expect(status).toHaveTextContent("phải đổi mật khẩu ở lần đăng nhập kế tiếp");
});

it("[AH-281, BR-03] hides the reset button on the own row and on locked accounts", async () => {
  const account = await createRep();
  const { unmount } = render(<NativeAccountDetail user={{ ...account, id: setter.me.actor_ref }} coops={coops} onChanged={() => {}} onClose={() => {}} />);
  expect(screen.queryByRole("button", { name: "Đặt lại mật khẩu" })).toBeNull();
  unmount();
  render(<NativeAccountDetail user={{ ...account, enabled: false }} coops={coops} onChanged={() => {}} onClose={() => {}} />);
  expect(screen.queryByRole("button", { name: "Đặt lại mật khẩu" })).toBeNull();
});
