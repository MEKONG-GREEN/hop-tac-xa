// FN-01 (ADR-0060): trang Đăng nhập ở màn riêng — kiểm theo tiêu chí chấp nhận ở mức giao diện (máy chủ có test riêng).
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { SignInOutcome } from "@/auth/AuthProvider";
import { buildPrincipal } from "@/auth/principal";
import { homeFor } from "@/nav/nav";
import LoginPage from "./page";

const nav = vi.hoisted(() => ({ replace: vi.fn() }));
vi.mock("next/navigation", () => ({
  useRouter: () => ({ replace: nav.replace }),
  useSearchParams: () => new URLSearchParams(""),
}));
vi.mock("@/config", async (orig) => ({ config: { ...((await orig()) as { config: object }).config, mock: false, support: null } }));

const signIn = vi.fn<(identifier: string, password: string, otp?: string) => Promise<SignInOutcome>>();
const login = vi.fn(async () => undefined);
const auth = {
  status: "anonymous" as "anonymous" | "authenticated" | "loading",
  principal: null as ReturnType<typeof buildPrincipal> | null,
  kn: { status: "legacy", assignments: [], context: null, menu: null, badges: null },
  dataConsent: { status: "not_required" },
  signIn,
  login,
  loginAsPersona: vi.fn(),
};
vi.mock("@/auth/AuthProvider", () => ({ useAuth: () => auth }));

async function fill(user: ReturnType<typeof userEvent.setup>, identifier: string, password: string) {
  if (identifier) await user.type(screen.getByLabelText(/Tên đăng nhập hoặc số điện thoại/), identifier);
  if (password) await user.type(screen.getByLabelText(/^Mật khẩu/), password);
}

const submit = (user: ReturnType<typeof userEvent.setup>) => user.click(screen.getByRole("button", { name: "Đăng nhập" }));

beforeEach(() => {
  vi.clearAllMocks();
  window.sessionStorage.clear();
  auth.status = "anonymous";
  auth.principal = null;
});

describe("Trang Đăng nhập (FN-01)", () => {
  it("AC-N1: bỏ trống tên đăng nhập hoặc mật khẩu → câu tại trường, không gửi yêu cầu đăng nhập", async () => {
    const user = userEvent.setup();
    render(<LoginPage />);
    await submit(user);
    expect(await screen.findByText("Nhập tên đăng nhập hoặc số điện thoại")).toBeInTheDocument();
    expect(screen.getByText("Nhập mật khẩu")).toBeInTheDocument();
    await fill(user, "an.nv", "");
    await submit(user);
    expect(screen.queryByText("Nhập tên đăng nhập hoặc số điện thoại")).toBeNull();
    expect(screen.getByText("Nhập mật khẩu")).toBeInTheDocument();
    expect(signIn).not.toHaveBeenCalled();
  });

  it("AC-02 / AC-08: sai mật khẩu → Còn n lần thử; lần thứ 4 → Còn 1 lần thử; khóa tạm → câu khóa 15 phút", async () => {
    const user = userEvent.setup();
    render(<LoginPage />);
    await fill(user, "an.nv", "sai");
    signIn.mockResolvedValueOnce({ kind: "invalid", remaining: 4 });
    await submit(user);
    expect(await screen.findByRole("alert")).toHaveTextContent("Sai tài khoản hoặc mật khẩu. Còn 4 lần thử.");
    signIn.mockResolvedValueOnce({ kind: "invalid", remaining: 1 });
    await submit(user);
    await waitFor(() => expect(screen.getByRole("alert")).toHaveTextContent("Còn 1 lần thử."));
    signIn.mockResolvedValueOnce({ kind: "locked", minutes: 15 });
    await submit(user);
    await waitFor(() =>
      expect(screen.getByRole("alert")).toHaveTextContent("Thử lại sau 15 phút hoặc dùng Quên mật khẩu?"),
    );
    expect(nav.replace).not.toHaveBeenCalled(); // không vào ứng dụng
  });

  it("AC-03: tài khoản đã khóa → câu EX-03", async () => {
    const user = userEvent.setup();
    render(<LoginPage />);
    await fill(user, "an.nv", "dung");
    signIn.mockResolvedValueOnce({ kind: "account_locked" });
    await submit(user);
    expect(await screen.findByRole("alert")).toHaveTextContent("Tài khoản đã bị khóa. Liên hệ người đã cấp tài khoản để mở khóa.");
  });

  it("AC-01: đăng nhập đúng → vào trang đầu tiên được xem (homeFor)", async () => {
    const user = userEvent.setup();
    const { rerender } = render(<LoginPage />);
    await fill(user, "0912345678", "Mat-khau-12!");
    signIn.mockImplementationOnce(async () => {
      // AuthProvider.signIn thiết lập phiên rồi trả authenticated.
      auth.status = "authenticated";
      auth.principal = buildPrincipal({ sub: "u", realm_access: { roles: ["coop_manager"] } }, null);
      return { kind: "authenticated" };
    });
    await submit(user);
    expect(signIn).toHaveBeenCalledWith("0912345678", "Mat-khau-12!", undefined);
    rerender(<LoginPage />);
    await waitFor(() => expect(nav.replace).toHaveBeenCalledWith(homeFor(auth.principal!)));
  });

  it("ô OTP tùy chọn: không nhập thì gửi không kèm mã; có nhập thì gửi kèm mã; sai thì nhắc nhập cả mã OTP", async () => {
    const user = userEvent.setup();
    render(<LoginPage />);
    await fill(user, "admin", "Mat-khau-12!");
    signIn.mockResolvedValueOnce({ kind: "invalid", remaining: 4 });
    await submit(user);
    expect(signIn).toHaveBeenLastCalledWith("admin", "Mat-khau-12!", undefined);
    expect(await screen.findByRole("alert")).toHaveTextContent("Nếu tài khoản đã bật xác thực hai bước, hãy nhập cả mã OTP.");
    await user.type(screen.getByLabelText(/^Mã OTP/), "123456");
    signIn.mockResolvedValueOnce({ kind: "invalid", remaining: 3 });
    await submit(user);
    expect(signIn).toHaveBeenLastCalledWith("admin", "Mat-khau-12!", "123456");
    expect(await screen.findByRole("alert")).toHaveTextContent("Sai tài khoản hoặc mật khẩu. Còn 3 lần thử.");
  });

  it("việc lần đầu: setup_required → nút gọi login(returnTo, tên đăng nhập) sang trang xác thực", async () => {
    const user = userEvent.setup();
    render(<LoginPage />);
    await fill(user, "admin", "Mat-khau-tam-1!");
    signIn.mockResolvedValueOnce({ kind: "setup_required" });
    await submit(user);
    await user.click(await screen.findByRole("button", { name: "Hoàn tất trên trang xác thực" }));
    expect(login).toHaveBeenCalledWith("/", "admin");
  });

  it("chống bấm hai lần: hai lần bấm nhanh chỉ gửi một yêu cầu, nút bị khóa trong lúc chờ", async () => {
    const user = userEvent.setup();
    render(<LoginPage />);
    await fill(user, "an.nv", "sai");
    let release: (o: SignInOutcome) => void = () => undefined;
    signIn.mockImplementationOnce(() => new Promise<SignInOutcome>((resolve) => (release = resolve)));
    const btn = screen.getByRole("button", { name: "Đăng nhập" });
    await user.dblClick(btn);
    expect(signIn).toHaveBeenCalledTimes(1);
    expect(screen.getByRole("button", { name: "Đang đăng nhập…" })).toBeDisabled();
    release({ kind: "invalid", remaining: 4 });
    await screen.findByRole("alert");
    expect(screen.getByRole("button", { name: "Đăng nhập" })).toBeEnabled();
    expect(signIn).toHaveBeenCalledTimes(1);
  });

  it("lỗi mạng, giới hạn tần suất, máy chủ chưa sẵn sàng → thông báo chung, không lộ chi tiết", async () => {
    const user = userEvent.setup();
    render(<LoginPage />);
    await fill(user, "an.nv", "x");
    for (const [kind, text] of [
      ["network", "Không kết nối được máy chủ đăng nhập"],
      ["rate_limited", "quá nhanh"],
      ["unavailable", "chưa dùng được"],
    ] as const) {
      signIn.mockResolvedValueOnce({ kind });
      await submit(user);
      await waitFor(() => expect(screen.getByRole("alert")).toHaveTextContent(text));
    }
  });
});
