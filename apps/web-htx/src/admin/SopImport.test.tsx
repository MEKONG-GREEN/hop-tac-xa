import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createAdminApiClient, createApiClient } from "@/api/client";
import { mockAdminFetchNoDelay, mockFetchNoDelay } from "@/api/mock/handler";
import { SopScreen } from "./SopScreen";

// [AH-38 FN-01.4/AC-04] Hộp thoại "Nhập từ Excel" của màn Quy trình SOP.
const saved: { name: string }[] = [];
vi.mock("@/lib/files", async (orig) => ({
  ...(await orig<Record<string, unknown>>()),
  saveBlob: (_blob: Blob, name: string) => {
    saved.push({ name });
  },
}));

let forced: { match: (req: Request) => boolean; res: Response } | null = null;
let loseResponse: ((req: Request) => boolean) | null = null;
const sent: { method: string; url: string; key: string | null }[] = [];

function adminFor(token: string) {
  return createAdminApiClient({
    baseUrl: "http://mock",
    getAccessToken: () => token,
    fetch: async (req) => {
      sent.push({ method: req.method, url: req.url, key: req.headers.get("Idempotency-Key") });
      if (forced?.match(req)) {
        const r = forced.res;
        forced = null;
        return r;
      }
      const res = await mockAdminFetchNoDelay(req);
      if (loseResponse?.(req)) {
        loseResponse = null;
        throw new TypeError("network response lost");
      }
      return res;
    },
  });
}

let auth = {
  api: createApiClient({ baseUrl: "http://mock", getAccessToken: () => "mock.setter", fetch: mockFetchNoDelay }),
  adminApi: adminFor("mock.setter"),
  principal: { roles: ["platform_admin", "sop_admin"] as string[], mfa: true },
};
vi.mock("@/auth/AuthProvider", () => ({ useAuth: () => auth }));

beforeEach(() => {
  forced = null;
  loseResponse = null;
  sent.length = 0;
  saved.length = 0;
  auth = { ...auth, adminApi: adminFor("mock.setter"), principal: { roles: ["platform_admin", "sop_admin"], mfa: true } };
});

const user = userEvent.setup();
const imports = () => sent.filter((s) => s.method === "POST" && s.url.endsWith("/v1/cropcycle/sop-imports"));

// Tệp .xlsx giả (zip "PK"): giả lập chỉ nhận dạng theo chữ ký, không giải nén.
const xlsx = () => new File([new Uint8Array([0x50, 0x4b, 3, 4, 1, 2, 3])], "sop-lua.xlsx");

async function openDialog() {
  await user.click(await screen.findByRole("button", { name: "Nhập từ Excel" }));
  return screen.findByRole("dialog");
}

async function pickAndImport(dlg: HTMLElement, file: File = xlsx()) {
  await user.upload(within(dlg).getByLabelText("Tệp SOP (.xlsx)"), file);
  await user.click(within(dlg).getByRole("button", { name: "Nhập" }));
}

describe("Nhập SOP từ Excel (AH-38)", () => {
  it("AC4: nút Nhập từ Excel chỉ có cho vai soạn SOP, không có cho vai chỉ đọc", async () => {
    const { unmount } = render(<SopScreen />);
    expect(await screen.findByRole("button", { name: "Nhập từ Excel" })).toBeTruthy();
    unmount();
    auth = { ...auth, adminApi: adminFor("mock.approver"), principal: { roles: ["param_approver"], mfa: true } };
    render(<SopScreen />);
    await screen.findByRole("region", { name: "Thư viện SOP" });
    expect(screen.queryByRole("button", { name: "Nhập từ Excel" })).toBeNull();
  });

  it("AC1: nhập .xlsx thành công → mở màn soạn SOP Nháp vừa tạo", async () => {
    render(<SopScreen />);
    const dlg = await openDialog();
    expect(within(dlg).getByText(/Kết quả là SOP Nháp/)).toBeTruthy();
    expect(within(dlg).getByRole("button", { name: "Nhập" })).toHaveProperty("disabled", true); // chưa chọn tệp
    await pickAndImport(dlg);
    expect((await screen.findByRole("status")).textContent).toContain("Đã nhập SOP Nháp từ Excel");
    expect(await screen.findByLabelText(/Tên SOP/)).toHaveProperty("value", "SOP nhập từ Excel (giả lập) 1");
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(imports()).toHaveLength(1);
  });

  it("AC2: 422 sop_import_rows → bảng Sheet/Dòng/Cột/Lý do, không mở SOP; sai định dạng tệp bị chặn ở web", async () => {
    render(<SopScreen />);
    const dlg = await openDialog();
    // Bỏ lọc accept của trình duyệt để mô phỏng người dùng chọn "Tất cả tệp".
    await userEvent.setup({ applyAccept: false }).upload(within(dlg).getByLabelText("Tệp SOP (.xlsx)"), new File(["a,b\n1,2\n"], "sop.csv", { type: "text/csv" }));
    expect((await within(dlg).findByText(/chỉ nhận Excel \.xlsx/)).textContent).toBeTruthy();
    expect(within(dlg).getByRole("button", { name: "Nhập" })).toHaveProperty("disabled", true);

    forced = {
      match: (r) => r.method === "POST" && r.url.endsWith("/v1/cropcycle/sop-imports"),
      res: new Response(
        JSON.stringify({
          type: "about:blank", title: "Tệp có dòng lỗi", status: 422, code: "cropcycle.sop_import_rows",
          rows: [
            { sheet: "Giai đoạn", row: 3, column: "Tên giai đoạn", reason: "bắt buộc, tối đa 200 ký tự" },
            { sheet: "Việc", row: 7, column: "Loại cửa sổ", reason: "Loại cửa sổ \"Hàng tuần\" không hợp lệ" },
          ],
        }),
        { status: 422, headers: { "Content-Type": "application/problem+json" } },
      ),
    };
    await pickAndImport(dlg);
    const table = await within(dlg).findByRole("table", { name: /Các dòng lỗi trong tệp \(2\)/ });
    const cells = within(table).getAllByRole("row").map((r) => within(r).queryAllByRole("cell").map((c) => c.textContent));
    expect(cells).toContainEqual(["Giai đoạn", "3", "Tên giai đoạn", "bắt buộc, tối đa 200 ký tự"]);
    expect(cells).toContainEqual(["Việc", "7", "Loại cửa sổ", "Loại cửa sổ \"Hàng tuần\" không hợp lệ"]);
    expect(screen.getByRole("dialog")).toBeTruthy(); // vẫn ở hộp thoại để sửa tệp
    expect(screen.queryByLabelText(/Tên SOP/)).toBeNull();
  });

  it("API giả lập trả cả hai trường hợp: thân không phải .xlsx → 422 rows với ba dòng lỗi mẫu", async () => {
    render(<SopScreen />);
    const dlg = await openDialog();
    // Mô phỏng tệp .xlsx mà giả lập coi là "tệp có lỗi": ghi đè chữ ký sau khi web đã nhận dạng.
    forced = { match: (r) => r.method === "POST" && r.url.endsWith("/v1/cropcycle/sop-imports"), res: await mockRowsResponse() };
    await pickAndImport(dlg);
    const table = await within(dlg).findByRole("table");
    expect(within(table).getAllByRole("row")).toHaveLength(4); // tiêu đề + 3 dòng lỗi
  });

  it("Idempotency: lỗi mạng rồi bấm lại gửi CÙNG khóa; nhập xong, thao tác mới mang khóa khác", async () => {
    render(<SopScreen />);
    let dlg = await openDialog();
    loseResponse = (r) => r.method === "POST" && r.url.endsWith("/v1/cropcycle/sop-imports");
    await pickAndImport(dlg);
    await within(dlg).findByRole("alert");
    await user.click(within(dlg).getByRole("button", { name: "Nhập" }));
    expect((await screen.findByRole("status")).textContent).toContain("Đã nhập SOP Nháp từ Excel");
    expect(imports()).toHaveLength(2);
    expect(imports()[0]!.key).toBeTruthy();
    expect(imports()[1]!.key).toBe(imports()[0]!.key);

    await user.click(screen.getByRole("button", { name: /Thư viện SOP/ }));
    dlg = await openDialog();
    await pickAndImport(dlg);
    await waitFor(() => expect(imports()).toHaveLength(3));
    expect(imports()[2]!.key).not.toBe(imports()[0]!.key);
  });

  it("AC3: Tải file mẫu gọi đúng endpoint và lưu tệp theo tên máy chủ gửi", async () => {
    render(<SopScreen />);
    const dlg = await openDialog();
    await user.click(within(dlg).getByRole("button", { name: /Tải file mẫu/ }));
    await waitFor(() => expect(saved).toHaveLength(1));
    expect(sent.some((s) => s.method === "GET" && s.url.endsWith("/v1/cropcycle/sop-imports/template.xlsx"))).toBe(true);
    expect(saved[0]!.name).toBe("mau-nhap-sop-gia-lap.csv");
  });
});

/** Phản hồi của giả lập cho thân không phải .xlsx (ba dòng lỗi mẫu): gọi thẳng route giả lập với thân CSV. */
async function mockRowsResponse(): Promise<Response> {
  const req = new Request("http://mock/v1/cropcycle/sop-imports", {
    method: "POST",
    headers: { Authorization: "Bearer mock.setter", "Idempotency-Key": "mock-rows-0000000000", "Content-Type": "text/csv" },
    body: "a,b\n1,2\n",
  });
  return mockAdminFetchNoDelay(req);
}
