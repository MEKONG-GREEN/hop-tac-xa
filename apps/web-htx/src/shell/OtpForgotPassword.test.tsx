// AH-280 (FN-10/AC-01..AC-04, EX-03, EX-06): quên mật khẩu bằng mã OTP qua SMS ở realm HTX.
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { OtpForgotPassword } from "./OtpForgotPassword";

function reply(status: number, body: unknown) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": status >= 400 ? "application/problem+json" : "application/json" } });
}

it("gửi mã tới số đã che; mã sai báo còn n lần; đúng thì đặt mật khẩu mới", async () => {
  const sent: { path: string; body: Record<string, unknown> }[] = [];
  const replies = [
    reply(200, { status: "otp_sent", challenge_id: "0190f0c4-0000-7000-8000-0000000000c1", phone_hint: "0913***222", expires_in: 300, resend_after: 60 }),
    reply(401, { code: "accounts.password_otp_invalid", status: 401, detail: "Mã OTP không đúng. Còn 4 lần thử", remaining_attempts: 4 }),
    reply(200, { status: "password_reset" }),
  ];
  const fetch = vi.fn(async (req: Request) => {
    sent.push({ path: new URL(req.url).pathname, body: await req.json() });
    expect(req.headers.get("Idempotency-Key")).toBeNull();
    return replies.shift()!;
  });
  const user = userEvent.setup();
  render(<OtpForgotPassword fetch={fetch} />);
  await user.click(screen.getByRole("button", { name: "Gửi mã OTP" }));
  expect(screen.getByText("Nhập tên đăng nhập hoặc số điện thoại")).toBeInTheDocument();
  await user.type(screen.getByLabelText(/Tên đăng nhập hoặc số điện thoại/), "tanphu");
  await user.click(screen.getByRole("button", { name: "Gửi mã OTP" }));
  expect(await screen.findByText(/0913\*\*\*222/)).toBeInTheDocument();
  expect(screen.getByRole("button", { name: /Gửi lại mã sau/ })).toBeDisabled();
  await user.type(screen.getByLabelText(/^Mã OTP/), "123456");
  await user.type(screen.getByLabelText(/^Mật khẩu mới/), "12345");
  await user.type(screen.getByLabelText(/^Nhập lại mật khẩu mới/), "12345");
  await user.click(screen.getByRole("button", { name: "Đặt mật khẩu mới" }));
  expect(screen.getByText("Mật khẩu tối thiểu 06 ký tự")).toBeInTheDocument();
  expect(fetch).toHaveBeenCalledTimes(1);
  await user.clear(screen.getByLabelText(/^Mật khẩu mới/));
  await user.type(screen.getByLabelText(/^Mật khẩu mới/), "matkhau1");
  await user.clear(screen.getByLabelText(/^Nhập lại mật khẩu mới/));
  await user.type(screen.getByLabelText(/^Nhập lại mật khẩu mới/), "matkhau1");
  await user.click(screen.getByRole("button", { name: "Đặt mật khẩu mới" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("Mã OTP không đúng. Còn 4 lần thử");
  await user.click(screen.getByRole("button", { name: "Đặt mật khẩu mới" }));
  expect(await screen.findByText("Đã đặt mật khẩu mới. Đăng nhập bằng mật khẩu mới.")).toBeInTheDocument();
  expect(sent.map((s) => s.path)).toEqual(["/v1/public/auth/password-otp", "/v1/public/auth/password-otp/reset", "/v1/public/auth/password-otp/reset"]);
  expect(sent[2]!.body).toEqual({ identifier: "tanphu", challenge_id: "0190f0c4-0000-7000-8000-0000000000c1", otp: "123456", new_password: "matkhau1", confirm_password: "matkhau1" });
});

it("[AC-N2, EX-01, AC-04] kênh tắt, không tìm thấy, tài khoản khóa → câu của máy chủ, không sang bước nhập mã", async () => {
  for (const [status, detail] of [
    [503, "Chưa gửi được mã OTP qua SMS. Vui lòng liên hệ người cấp tài khoản để được cấp mật khẩu tạm."],
    [404, "Không tìm thấy tài khoản với tên đăng nhập hoặc số điện thoại này"],
    [403, "Tài khoản đã bị khóa hoặc chưa có số điện thoại. Vui lòng liên hệ người cấp tài khoản."],
  ] as const) {
    const user = userEvent.setup();
    const { unmount } = render(<OtpForgotPassword fetch={async () => reply(status, { status, detail })} />);
    await user.type(screen.getByLabelText(/Tên đăng nhập hoặc số điện thoại/), "tanphu");
    await user.click(screen.getByRole("button", { name: "Gửi mã OTP" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(detail);
    expect(screen.queryByLabelText(/^Mã OTP/)).not.toBeInTheDocument();
    unmount();
  }
});
