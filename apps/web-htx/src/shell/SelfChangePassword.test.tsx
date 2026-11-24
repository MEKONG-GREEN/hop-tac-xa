import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it } from "vitest";
import { createApiClient } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { MOCK_WRONG_CURRENT_PASSWORD } from "@/api/mock/native-accounts";
import { mockToken } from "@/api/mock/personas";
import { SelfChangePassword } from "./SelfChangePassword";

// AH-281 (FN-10/AC-N4, AC-N5): tự đổi mật khẩu.
const api = createApiClient({ baseUrl: "https://api.test", getAccessToken: () => mockToken("setter"), fetch: mockFetchNoDelay });

async function fill(current: string, next: string, confirm: string) {
  const user = userEvent.setup();
  render(<SelfChangePassword api={api} onClose={() => {}} />);
  const form = screen.getByRole("form", { name: "Đổi mật khẩu" });
  if (current) await user.type(within(form).getByLabelText("Mật khẩu hiện tại"), current);
  if (next) await user.type(within(form).getByLabelText("Mật khẩu mới"), next);
  if (confirm) await user.type(within(form).getByLabelText("Nhập lại mật khẩu mới"), confirm);
  await user.click(within(form).getByRole("button", { name: "Đổi mật khẩu" }));
  return form;
}

it.each([
  ["short", "matkhau-cu", "12345", "12345", "Mật khẩu tối thiểu 06 ký tự"],
  ["mismatch", "matkhau-cu", "matkhau1", "matkhau2", "Mật khẩu nhập lại không khớp"],
  ["same as current", "matkhau1", "matkhau1", "matkhau1", "Mật khẩu mới phải khác mật khẩu hiện tại"],
])("[AC-N5] rejects %s at the field", async (_name, current, next, confirm, message) => {
  const form = await fill(current, next, confirm);
  expect(await within(form).findByText(message)).toHaveClass("err");
});

it("[AC-N5] shows the server message when the current password is wrong", async () => {
  const form = await fill(MOCK_WRONG_CURRENT_PASSWORD, "matkhau1", "matkhau1");
  expect(await within(form).findByText("Mật khẩu hiện tại không đúng")).toHaveClass("err");
});

it("[AC-N4] changes the password", async () => {
  await fill("matkhau-cu", "matkhau1", "matkhau1");
  expect(await screen.findByRole("status")).toHaveTextContent("Đã đổi mật khẩu");
});
