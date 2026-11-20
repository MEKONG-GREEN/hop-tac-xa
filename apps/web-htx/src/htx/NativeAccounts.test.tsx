import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { createAdminApiClient, createApiClient, unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { COOPERATIVES } from "@/api/mock/data";
import { mockAdminFetchNoDelay, mockFetchNoDelay } from "@/api/mock/handler";
import { resetNativeAccountMock } from "@/api/mock/native-accounts";
import { mockToken } from "@/api/mock/personas";
import { NativeAccounts } from "./NativeAccounts";

const auth = {
  api: createApiClient({ baseUrl: "https://api.test", getAccessToken: () => mockToken("setter"), fetch: mockFetchNoDelay }),
  adminApi: createAdminApiClient({ baseUrl: "https://admin.test", getAccessToken: () => mockToken("setter"), fetch: mockAdminFetchNoDelay }),
};
vi.mock("@/auth/AuthProvider", () => ({ useAuth: () => auth }));
beforeEach(resetNativeAccountMock);

it("creates one-cooperative dual-role accounts and displays the temporary password once", async () => {
  const user = userEvent.setup(); render(<NativeAccounts />);
  await screen.findByText("Không có bản ghi phù hợp");
  await user.click(screen.getByRole("button", { name: "Tạo tài khoản" }));
  const form = screen.getByRole("form", { name: "Tạo tài khoản HTX" });
  await user.type(within(form).getByLabelText("Tên đăng nhập"), "native-web");
  await user.type(within(form).getByLabelText("Họ tên"), "Đại diện thử nghiệm");
  await user.type(within(form).getByLabelText("Số điện thoại"), "0912345678");
  await user.click(within(form).getByLabelText("Đồng thời là Thành viên HTX"));
  await user.selectOptions(within(form).getByLabelText("HTX"), COOPERATIVES.find((c) => c.api.active)!.api.ref);
  await user.click(within(form).getByRole("button", { name: "Lưu tài khoản" }));
  expect(await screen.findByLabelText("Mật khẩu tạm")).toHaveTextContent(/^[A-Za-z0-9]{8}$/);
  expect(screen.getByText("Đại diện HTX, Thành viên HTX")).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Đóng thông tin mật khẩu" }));
  expect(screen.queryByLabelText("Mật khẩu tạm")).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: /đặt lại mật khẩu/i })).not.toBeInTheDocument();
});

it("uses exact phone search, role counts and ten-row pagination", async () => {
  const coop = COOPERATIVES.find((c) => c.api.active)!.api.ref;
  for (let i = 0; i < 12; i++) await unwrap(auth.adminApi.POST("/v1/accounts/htx-users", { params: { header: { "Idempotency-Key": newIdempotencyKey() } }, body: { username: `native-page-${String(i).padStart(2, "0")}`, full_name: `Người thử ${i}`, phone: `09000000${String(i).padStart(2, "0")}`, cooperative_ref: coop, roles: ["htx_member"] } }));
  const user = userEvent.setup();render(<NativeAccounts />);
  await screen.findByText("Người thử 0");
  const table = screen.getByRole("table", { name: "Tài khoản HTX" });
  expect(within(table).getAllByRole("row")).toHaveLength(11);
  await user.type(screen.getByPlaceholderText("Tên, tên đăng nhập hoặc đúng SĐT"), "09000000");
  expect(await screen.findByText("Không có bản ghi phù hợp")).toBeInTheDocument();
  await user.clear(screen.getByPlaceholderText("Tên, tên đăng nhập hoặc đúng SĐT"));
  await user.type(screen.getByPlaceholderText("Tên, tên đăng nhập hoặc đúng SĐT"), "0900000001");
  expect(screen.getByText("Người thử 1")).toBeInTheDocument();
  expect(within(screen.getByRole("table", { name: "Tài khoản HTX" })).getAllByRole("row")).toHaveLength(2);
});
