// AH-23 / REQ-011 / KH-24: màn Đồng ý thu thập dữ liệu — hiện nội dung + phiên bản, "Đồng ý" ghi bằng chứng, "Không đồng ý"
// đăng xuất (không ghi gì), lỗi thì chặn kèm Thử lại/Đăng xuất; 403 accounts.data_consent_required từ route ảnh/GPS hỏi lại.
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { authMiddleware } from "@/api/client";
import { ApiProblem } from "@/api/problem";
import DataConsentPage from "@/app/dong-y-thu-thap/page";
import { isBareRoute } from "@/components/AppShell";
import type { DataConsentState } from "./AuthProvider";

const replace = vi.fn();
vi.mock("next/navigation", () => ({
  useRouter: () => ({ replace }),
  useSearchParams: () => new URLSearchParams("returnTo=%2Fhtx%2Fmua-vu"),
}));

const auth = {
  status: "authenticated" as "authenticated" | "anonymous",
  dataConsent: { status: "unknown" } as DataConsentState,
  acceptDataConsent: vi.fn<(version: string) => Promise<void>>(async () => undefined),
  refreshDataConsent: vi.fn(async () => undefined),
  logout: vi.fn(async () => undefined),
};
vi.mock("@/auth/AuthProvider", () => ({ useAuth: () => auth }));

const TEXT = "Ứng dụng HTX thu thập ảnh hiện trường có gắn tọa độ GPS.\nDữ liệu được lưu theo Nghị định 13/2023/NĐ-CP.";

beforeEach(() => {
  vi.clearAllMocks();
  auth.status = "authenticated";
  auth.dataConsent = { status: "required", version: "tam-1", text: TEXT };
});

describe("Màn Đồng ý thu thập dữ liệu", () => {
  it("AC1: hiện tiêu đề, nội dung và phiên bản; có hai nút Đồng ý / Không đồng ý", () => {
    render(<DataConsentPage />);
    expect(screen.getByRole("heading", { name: "Đồng ý thu thập dữ liệu" })).toBeInTheDocument();
    expect(screen.getByText(/Ứng dụng HTX thu thập ảnh hiện trường/)).toBeInTheDocument();
    expect(screen.getByText("Phiên bản nội dung: tam-1")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Đồng ý" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Không đồng ý" })).toBeInTheDocument();
  });

  it("AC1/AC2: bấm Đồng ý → gọi acceptDataConsent với đúng phiên bản đã hiển thị", async () => {
    render(<DataConsentPage />);
    await userEvent.click(screen.getByRole("button", { name: "Đồng ý" }));
    expect(auth.acceptDataConsent).toHaveBeenCalledTimes(1);
    expect(auth.acceptDataConsent).toHaveBeenCalledWith("tam-1");
    expect(auth.logout).not.toHaveBeenCalled();
  });

  it("AC-N3: bấm Không đồng ý → đăng xuất, không ghi đồng ý", async () => {
    render(<DataConsentPage />);
    await userEvent.click(screen.getByRole("button", { name: "Không đồng ý" }));
    expect(auth.logout).toHaveBeenCalledTimes(1);
    expect(auth.acceptDataConsent).not.toHaveBeenCalled();
  });

  it("ghi đồng ý lỗi → báo lỗi, vẫn ở màn đồng ý", async () => {
    auth.acceptDataConsent.mockRejectedValueOnce(
      new ApiProblem({ type: "about:blank", title: "x", status: 503, code: "accounts.data_consent_unconfigured", trace_id: "t1" }),
    );
    render(<DataConsentPage />);
    await userEvent.click(screen.getByRole("button", { name: "Đồng ý" }));
    expect(await screen.findByRole("alert")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Đồng ý" })).toBeEnabled();
  });

  it("nội dung vừa đổi (409) → hiện thông báo kèm nội dung mới", () => {
    auth.dataConsent = { status: "required", version: "tam-2", text: "Nội dung mới", message: "Nội dung đã được cập nhật. Vui lòng đọc lại và xác nhận." };
    render(<DataConsentPage />);
    expect(screen.getByRole("status")).toHaveTextContent("Nội dung đã được cập nhật");
    expect(screen.getByText("Phiên bản nội dung: tam-2")).toBeInTheDocument();
  });

  it("không xác định được trạng thái (error) → chặn, có Thử lại và Đăng xuất", async () => {
    auth.dataConsent = { status: "error", problem: null };
    render(<DataConsentPage />);
    expect(screen.getByRole("alert")).toHaveTextContent("Chưa xác nhận được");
    expect(screen.queryByRole("button", { name: "Đồng ý" })).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: "Thử lại" }));
    expect(auth.refreshDataConsent).toHaveBeenCalledTimes(1);
    await userEvent.click(screen.getByRole("button", { name: "Đăng xuất" }));
    expect(auth.logout).toHaveBeenCalledTimes(1);
  });

  it("đã không cần đồng ý → về trang đang vào; chưa đăng nhập → về /login", () => {
    auth.dataConsent = { status: "not_required" };
    const { unmount } = render(<DataConsentPage />);
    expect(replace).toHaveBeenCalledWith("/htx/mua-vu");
    unmount();
    replace.mockClear();
    auth.status = "anonymous";
    render(<DataConsentPage />);
    expect(replace).toHaveBeenCalledWith("/login");
  });
});

describe("Khung ứng dụng và client", () => {
  it("/dong-y-thu-thap là trang không khung", () => {
    expect(isBareRoute("/dong-y-thu-thap")).toBe(true);
    expect(isBareRoute("/htx/mua-vu")).toBe(false);
  });

  it("403 accounts.data_consent_required → gọi onDataConsentRequired, phản hồi vẫn nguyên vẹn", async () => {
    const onDataConsentRequired = vi.fn();
    const mw = authMiddleware(() => "tok", undefined, undefined, onDataConsentRequired);
    const request = new Request("https://api.test/v1/files", { method: "POST", headers: { Authorization: "Bearer tok" } });
    const body = { code: "accounts.data_consent_required", status: 403 };
    const response = new Response(JSON.stringify(body), { status: 403, headers: { "Content-Type": "application/problem+json" } });
    const out = await mw.onResponse?.({ request, response, schemaPath: "", params: {}, id: "1", options: {} as never });
    expect(onDataConsentRequired).toHaveBeenCalledTimes(1);
    expect(await (out as Response).json()).toEqual(body);
  });

  it("403 mã khác hoặc không phải JSON → không gọi", async () => {
    const onDataConsentRequired = vi.fn();
    const mw = authMiddleware(() => "tok", undefined, undefined, onDataConsentRequired);
    const request = new Request("https://api.test/v1/files", { method: "POST" });
    const call = (response: Response) => mw.onResponse?.({ request, response, schemaPath: "", params: {}, id: "1", options: {} as never });
    await call(new Response(JSON.stringify({ code: "forbidden" }), { status: 403 }));
    await call(new Response("không phải json", { status: 403 }));
    await call(new Response(JSON.stringify({ code: "accounts.data_consent_required" }), { status: 409 }));
    expect(onDataConsentRequired).not.toHaveBeenCalled();
  });
});
