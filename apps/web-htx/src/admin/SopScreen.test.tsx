import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createAdminApiClient, createApiClient } from "@/api/client";
import { mockAdminFetchNoDelay, mockFetchNoDelay } from "@/api/mock/handler";
import { SopScreen } from "./SopScreen";

// Điều khiển lỗi giả lập: ép máy chủ trả 422 theo trường, hoặc mất phản hồi sau khi máy chủ đã ghi.
let forced: { match: (req: Request) => boolean; res: Response } | null = null;
let loseResponse: ((req: Request) => boolean) | null = null;
const sent: { url: string; key: string | null }[] = [];

const problem422 = (code: string, errors: { field: string; reason: string }[]) =>
  new Response(JSON.stringify({ type: "about:blank", title: "Dữ liệu không hợp lệ", status: 422, code, errors }), {
    status: 422,
    headers: { "Content-Type": "application/problem+json" },
  });

function adminFor(token: string) {
  return createAdminApiClient({
    baseUrl: "http://mock",
    getAccessToken: () => token,
    fetch: async (req) => {
      if (req.method === "POST") sent.push({ url: req.url, key: req.headers.get("Idempotency-Key") });
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

function actAs(token: string, roles: string[]) {
  auth = { ...auth, adminApi: adminFor(token), api: createApiClient({ baseUrl: "http://mock", getAccessToken: () => token, fetch: mockFetchNoDelay }), principal: { roles, mfa: true } };
}

beforeEach(() => {
  forced = null;
  loseResponse = null;
  sent.length = 0;
  actAs("mock.setter", ["platform_admin", "sop_admin"]);
});

const user = userEvent.setup();

async function openNew() {
  await user.click(await screen.findByRole("button", { name: /Soạn SOP/ }));
  await screen.findByRole("region", { name: "Thông tin chung" });
}

async function fillInfo(name: string) {
  await user.selectOptions(screen.getByLabelText(/Loại cây/), "LUA");
  await user.type(screen.getByLabelText(/Tên SOP/), name);
  await user.type(screen.getByLabelText(/Tên mốc gốc/), "Ngày xuống giống");
  await user.type(screen.getByLabelText("Tên giai đoạn 1"), "Chuẩn bị");
}

async function openLibraryItem(name: string) {
  await user.click(await screen.findByRole("button", { name }));
  await screen.findByRole("region", { name: "Thông tin chung" });
}

describe("Màn Quy trình SOP (AH-8)", () => {
  it("AC1: thư viện liệt kê tên, loại cây, giống, trạng thái, phiên bản; soạn và lưu → SOP ở trạng thái Nháp", async () => {
    render(<SopScreen />);
    const lib = await screen.findByRole("region", { name: "Thư viện SOP" });
    expect(await within(lib).findByText("Quy trình lúa thơm ST25")).toBeTruthy();
    expect(within(lib).getAllByText("Đang áp dụng").length).toBeGreaterThan(0);
    expect(within(lib).getAllByText("Chung").length).toBeGreaterThan(0);

    await openNew();
    await fillInfo("SOP thử màn hình AC1");
    await user.click(screen.getByRole("button", { name: "Lưu nháp" }));
    expect((await screen.findByRole("status")).textContent).toContain("Đã lưu nháp");
    const info = await screen.findByRole("region", { name: "Thông tin chung" });
    expect(within(info).getByLabelText(/Tên SOP/)).toHaveProperty("value", "SOP thử màn hình AC1");
    expect(screen.getAllByText("Nháp").length).toBeGreaterThan(0); // chip đầu trang và dòng phiên bản 1 trong lịch sử
    // Chưa phát hành: nút Phát hành có nhưng SOP còn nháp; thông tin chung còn sửa được.
    expect(screen.getByLabelText(/Tên SOP/)).toHaveProperty("disabled", false);
  });

  it("AC2: máy chủ từ chối theo trường → đúng ô tô đỏ kèm lý do, mọi ô giữ nguyên", async () => {
    render(<SopScreen />);
    await openNew();
    await fillInfo("SOP thử lỗi máy chủ");
    forced = {
      match: (r) => r.method === "POST" && r.url.endsWith("/v1/cropcycle/sops"),
      res: problem422("validation_failed", [
        { field: "stages[0].name", reason: "tên giai đoạn trùng trong quy trình" },
        { field: "name", reason: "bắt buộc, tối đa 200 ký tự" },
      ]),
    };
    await user.click(screen.getByRole("button", { name: "Lưu nháp" }));
    const stageName = await screen.findByLabelText("Tên giai đoạn 1");
    await waitFor(() => expect(stageName.getAttribute("aria-invalid")).toBe("true"));
    expect(screen.getAllByText("tên giai đoạn trùng trong quy trình").length).toBeGreaterThan(0);
    expect(screen.getByLabelText(/Tên SOP/).getAttribute("aria-invalid")).toBe("true");
    expect(screen.getByLabelText(/Tên mốc gốc/).getAttribute("aria-invalid")).toBeNull();
    expect((stageName as HTMLInputElement).value).toBe("Chuẩn bị");
  });

  it("AC2: kiểm phía màn trước khi gửi — Từ > Đến, liều ≤ 0, thiếu đơn vị tô đỏ đúng ô", async () => {
    render(<SopScreen />);
    await openNew();
    await fillInfo("SOP kiểm phía màn");
    await user.type(screen.getByLabelText("Từ (ngày) giai đoạn 1"), "9");
    await user.type(screen.getByLabelText("Đến (ngày) giai đoạn 1"), "2");
    await user.click(screen.getByRole("button", { name: "+ Thêm việc vào giai đoạn 1" }));
    await user.type(screen.getByLabelText("Nội dung việc 1.1"), "Bón thúc");
    await user.click(screen.getByRole("button", { name: "+ Thêm vật tư cho việc 1.1" }));
    await user.type(screen.getByLabelText("Tên vật tư 1 của việc 1.1"), "Phân tự pha");
    await user.type(screen.getByLabelText("Liều/ha vật tư 1 của việc 1.1"), "0");
    const before = sent.length;
    await user.click(screen.getByRole("button", { name: "Lưu nháp" }));
    await waitFor(() => expect(screen.getByLabelText("Đến (ngày) giai đoạn 1").getAttribute("aria-invalid")).toBe("true"));
    expect(screen.getByLabelText("Liều/ha vật tư 1 của việc 1.1").getAttribute("aria-invalid")).toBe("true");
    expect(screen.getByLabelText("Đơn vị vật tư 1 của việc 1.1").getAttribute("aria-invalid")).toBe("true");
    expect(sent.length).toBe(before); // không gửi khi còn lỗi
  });

  it("AC3: bốn loại cửa sổ; Theo giai đoạn chỉ chọn được khi giai đoạn đã có khoảng ngày", async () => {
    render(<SopScreen />);
    await openNew();
    await user.click(screen.getByRole("button", { name: "+ Thêm việc vào giai đoạn 1" }));
    const kind = screen.getByLabelText("Cửa sổ việc 1.1") as HTMLSelectElement;
    expect([...kind.options].map((o) => o.value)).toEqual(["date_range", "open", "stage", "none"]);
    expect(kind.value).toBe("none");
    expect(kind.options[2]?.disabled).toBe(true); // giai đoạn chưa có Từ–Đến

    await user.type(screen.getByLabelText("Từ (ngày) giai đoạn 1"), "-3");
    await user.type(screen.getByLabelText("Đến (ngày) giai đoạn 1"), "10");
    expect(kind.options[2]?.disabled).toBe(false);

    await user.selectOptions(kind, "date_range");
    expect(screen.getByLabelText("Mốc việc 1.1")).toBeTruthy();
    expect([...(screen.getByLabelText("Mốc việc 1.1") as HTMLSelectElement).options].map((o) => o.value)).toEqual(["nxg", "nth", "ntt"]);
    await user.type(screen.getByLabelText("Từ (ngày) việc 1.1"), "-5");
    expect(screen.getByLabelText("Đến (ngày) việc 1.1")).toBeTruthy();

    await user.selectOptions(kind, "open");
    expect(screen.getByLabelText("Từ (ngày) việc 1.1")).toBeTruthy();
    expect(screen.queryByLabelText("Đến (ngày) việc 1.1")).toBeNull();

    await user.selectOptions(kind, "stage");
    expect(screen.queryByLabelText("Từ (ngày) việc 1.1")).toBeNull();
    expect(screen.getByText(/Lấy khoảng ngày -3 … 10 của giai đoạn/)).toBeTruthy();

    await user.selectOptions(kind, "none");
    expect(screen.queryByLabelText("Mốc việc 1.1")).toBeNull();
    expect(screen.queryByLabelText("Từ (ngày) việc 1.1")).toBeNull();
  });

  it("AC3: vật tư chọn từ danh mục tự điền đơn vị; liều nhập kiểu Việt được chuẩn hóa", async () => {
    render(<SopScreen />);
    await openNew();
    await user.click(screen.getByRole("button", { name: "+ Thêm việc vào giai đoạn 1" }));
    await user.click(screen.getByRole("button", { name: "+ Thêm vật tư cho việc 1.1" }));
    await user.selectOptions(screen.getByLabelText("Vật tư 1 của việc 1.1"), "phan_ure");
    expect((screen.getByLabelText("Đơn vị vật tư 1 của việc 1.1") as HTMLInputElement).value).toBe("tấn");
    expect(screen.queryByLabelText("Tên vật tư 1 của việc 1.1")).toBeNull();
    const dose = screen.getByLabelText("Liều/ha vật tư 1 của việc 1.1") as HTMLInputElement;
    await user.type(dose, "0,5");
    await user.tab();
    expect(dose.value).toBe("0.5");
  });

  it("AC5 + AC4: chặn phát hành khi Thu hoạch cuối không có cửa sổ ngày; sửa xong thì chính người soạn phát hành một bước", async () => {
    render(<SopScreen />);
    await openLibraryItem("Quy trình OM18 vùng ngập (bản nháp)");
    const kind = screen.getByLabelText("Cửa sổ việc 3.1");
    await user.selectOptions(kind, "none");
    await user.click(screen.getByRole("button", { name: "Lưu nháp" }));
    await screen.findByRole("status");
    await user.click(screen.getByRole("button", { name: "Phát hành" }));
    const alert = await screen.findByText("Chưa phát hành được");
    expect(alert.closest("[role=alert]")?.textContent).toContain("Việc Thu hoạch cuối");
    expect(screen.getByLabelText("Cửa sổ việc 3.1").getAttribute("aria-invalid")).toBe("true");
    expect(screen.queryByText("Đang áp dụng")).toBeNull();

    // Máy chủ cũng chặn (kể cả khi màn bị qua mặt): trả sop_harvest_window theo trường.
    forced = {
      match: (r) => r.url.endsWith("/publish"),
      res: problem422("cropcycle.sop_harvest_window", [{ field: "steps[7].window", reason: "cần cửa sổ Theo ngày so với mốc gốc hoặc Theo giai đoạn" }]),
    };
    await user.selectOptions(screen.getByLabelText("Cửa sổ việc 3.1"), "date_range");
    await user.clear(screen.getByLabelText("Từ (ngày) việc 3.1"));
    await user.type(screen.getByLabelText("Từ (ngày) việc 3.1"), "97");
    await user.clear(screen.getByLabelText("Đến (ngày) việc 3.1"));
    await user.type(screen.getByLabelText("Đến (ngày) việc 3.1"), "102");
    await user.click(screen.getByRole("button", { name: "Lưu nháp" }));
    await waitFor(() => expect(screen.getByRole("button", { name: "Phát hành" })).toHaveProperty("disabled", false));
    await user.click(screen.getByRole("button", { name: "Phát hành" }));
    expect((await screen.findAllByText(/cropcycle\.sop_harvest_window/)).length).toBeGreaterThan(0);
    expect(screen.getByLabelText("Cửa sổ việc 3.1").getAttribute("aria-invalid")).toBe("true");

    // Hợp lệ → phát hành một bước (người soạn là người phát hành), SOP Đang áp dụng.
    await user.click(screen.getByRole("button", { name: "Phát hành" }));
    expect((await screen.findByRole("status")).textContent).toContain("Đã phát hành");
    expect((await screen.findAllByText("Đang áp dụng")).length).toBeGreaterThan(0);
    expect(await screen.findByRole("button", { name: "Tạo phiên bản mới" })).toBeTruthy();
    expect(screen.getByLabelText(/Tên SOP/)).toHaveProperty("disabled", true); // thông tin chung khóa sau phát hành
  });

  it("Nhân bản (HTX US-CAT-03): tên trùng bị chặn; bản sao là nháp giữ nguyên giai đoạn và việc", async () => {
    render(<SopScreen />);
    await openLibraryItem("Quy trình lúa chất lượng cao, phát thải thấp");
    await user.click(screen.getByRole("button", { name: "Nhân bản" }));
    const dlg = await screen.findByRole("dialog");
    await user.clear(within(dlg).getByLabelText("Tên SOP mới"));
    await user.type(within(dlg).getByLabelText("Tên SOP mới"), "Quy trình lúa thơm ST25");
    await user.click(within(dlg).getByRole("button", { name: "Nhân bản" }));
    expect((await within(dlg).findByRole("alert")).textContent).toContain("Tên SOP này đã tồn tại, vui lòng chọn tên khác.");
    await user.clear(within(dlg).getByLabelText("Tên SOP mới"));
    await user.type(within(dlg).getByLabelText("Tên SOP mới"), "Quy trình CLC cho OM18");
    await user.click(within(dlg).getByRole("button", { name: "Nhân bản" }));
    expect((await screen.findByRole("status")).textContent).toContain("Đã nhân bản");
    expect(await screen.findByLabelText("Tên giai đoạn 3")).toHaveProperty("value", "Thu hoạch");
    expect(screen.getByLabelText("Nội dung việc 3.1")).toHaveProperty("value", "Thu hoạch bằng máy gặt đập liên hợp");
    expect(screen.getAllByText("Nháp").length).toBeGreaterThan(0);
  });

  it("AC6: vai param_approver chỉ xem — không có nút soạn, lưu, phát hành", async () => {
    actAs("mock.approver", ["param_approver"]);
    render(<SopScreen />);
    await screen.findByRole("region", { name: "Thư viện SOP" });
    expect(screen.queryByRole("button", { name: /Soạn SOP/ })).toBeNull();
    await openLibraryItem("Quy trình lúa thơm ST25");
    expect(screen.queryByRole("button", { name: "Lưu nháp" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Phát hành" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Tạo phiên bản mới" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Ngừng áp dụng" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Nhân bản" })).toBeNull(); // AH-21 AC4
    expect(screen.queryByRole("button", { name: "Xóa" })).toBeNull();
    expect(screen.getByText(/chỉ xem SOP/)).toBeTruthy();
    expect(screen.getByLabelText("Nội dung việc 1.1")).toHaveProperty("disabled", true);
  });

  it("AH-21 AC1: nhân bản sang giống khác — hộp thoại lấy sẵn loại cây, giống, mốc gốc của nguồn; bản sao là Nháp đủ giai đoạn và việc", async () => {
    render(<SopScreen />);
    await openLibraryItem("Quy trình lúa thơm ST25");
    await user.click(screen.getByRole("button", { name: "Nhân bản" }));
    const dlg = await screen.findByRole("dialog");
    expect(within(dlg).getByLabelText(/Loại cây/)).toHaveProperty("value", "LUA");
    expect(within(dlg).getByLabelText(/Giống/)).toHaveProperty("value", ""); // nguồn là SOP chung của loại cây
    expect(within(dlg).getByLabelText(/Tên mốc gốc/)).toHaveProperty("value", "Ngày xuống giống");
    await user.selectOptions(within(dlg).getByLabelText(/Giống/), "OM5451");
    await user.clear(within(dlg).getByLabelText("Tên SOP mới"));
    await user.type(within(dlg).getByLabelText("Tên SOP mới"), "Quy trình ST25 cho OM5451");
    await user.click(within(dlg).getByRole("button", { name: "Nhân bản" }));
    expect((await screen.findByRole("status")).textContent).toContain("Đã nhân bản");
    // The new editor mounts before its asynchronous catalog options arrive.
    await waitFor(() => expect(screen.getByLabelText(/Giống/)).toHaveProperty("value", "OM5451"));
    expect(screen.getByLabelText("Tên giai đoạn 3")).toHaveProperty("value", "Thu hoạch");
    expect(screen.getAllByText("Nháp").length).toBeGreaterThan(0);
    const nhanBan = sent.find((s) => s.url.endsWith("/clone"));
    expect(nhanBan?.key).toBeTruthy();
  });

  it("AH-21 AC2: phiên bản mới → bản cũ Ngừng áp dụng, bản mới Đang áp dụng, có người phát hành; xem bản cũ ở chế độ chỉ đọc", async () => {
    render(<SopScreen />);
    await openLibraryItem("Quy trình lúa thơm ST25");
    await user.click(screen.getByRole("button", { name: "Tạo phiên bản mới" }));
    expect((await screen.findByRole("status")).textContent).toContain("Đã tạo bản nháp phiên bản mới");
    await user.click(await screen.findByRole("button", { name: "Phát hành" }));
    expect((await screen.findByRole("status")).textContent).toContain("Đã phát hành");

    const hist = await screen.findByRole("region", { name: "Lịch sử phiên bản" });
    const rows = within(hist).getAllByRole("row").slice(1);
    expect(rows).toHaveLength(2);
    expect(within(rows[0]!).getByText("v1")).toBeTruthy();
    expect(within(rows[0]!).getByText("Ngừng áp dụng")).toBeTruthy();
    expect(within(rows[1]!).getByText("v2")).toBeTruthy();
    expect(within(rows[1]!).getByText("Đang áp dụng")).toBeTruthy();
    expect(within(rows[1]!).getByText("Quản trị viên A")).toBeTruthy(); // tên người phát hành do máy chủ tra

    await user.click(within(hist).getByRole("button", { name: "Xem phiên bản 1" }));
    const dlg = await screen.findByRole("dialog");
    expect(within(dlg).getByText("Phiên bản 1 (chỉ đọc)")).toBeTruthy();
    expect(within(dlg).getByLabelText("Nội dung việc 1.1")).toHaveProperty("disabled", true);
  });

  it("AH-21 AC3: SOP không có thao tác Ẩn; Ngừng áp dụng có lý do thì SOP vẫn hiện trong thư viện với trạng thái Ngừng áp dụng", async () => {
    render(<SopScreen />);
    await openLibraryItem("Quy trình lúa thơm ST25");
    expect(screen.queryByRole("button", { name: /^Ẩn/ })).toBeNull();
    await user.click(screen.getByRole("button", { name: "Ngừng áp dụng" }));
    const dlg = await screen.findByRole("dialog");
    await user.type(within(dlg).getByLabelText(/Lý do/), "Thay bằng quy trình mới của vụ sau");
    await user.click(within(dlg).getByRole("button", { name: "Ngừng áp dụng" }));
    expect((await screen.findByRole("status")).textContent).toContain("Đã ngừng áp dụng");
    await user.click(screen.getByRole("button", { name: /Thư viện SOP/ }));
    const lib = await screen.findByRole("region", { name: "Thư viện SOP" });
    const row = within(lib).getByText("Quy trình lúa thơm ST25").closest("tr")!;
    expect(within(row).getByText("Ngừng áp dụng")).toBeTruthy();
  });

  it("AH-21 Q-46: chỉ SOP Nháp chưa phát hành mới có nút Xóa; xóa có xác nhận, một Idempotency-Key, SOP biến mất khỏi thư viện", async () => {
    render(<SopScreen />);
    await openLibraryItem("Quy trình lúa thơm ST25");
    expect(screen.queryByRole("button", { name: "Xóa" })).toBeNull(); // đã phát hành
    await user.click(screen.getByRole("button", { name: /Thư viện SOP/ }));

    await user.click(await screen.findByRole("button", { name: "Soạn SOP" }));
    await screen.findByRole("region", { name: "Thông tin chung" });
    await fillInfo("SOP nháp sẽ xóa");
    await user.click(screen.getByRole("button", { name: "Lưu nháp" }));
    await screen.findByRole("status");
    await user.click(await screen.findByRole("button", { name: "Xóa" }));
    const dlg = await screen.findByRole("dialog");
    expect(within(dlg).getByText(/Xóa hẳn SOP nháp này/)).toBeTruthy();
    await user.click(within(dlg).getByRole("button", { name: "Xóa" }));
    expect((await screen.findByRole("status")).textContent).toContain("Đã xóa SOP nháp");
    const lib = await screen.findByRole("region", { name: "Thư viện SOP" });
    expect(within(lib).queryByText("SOP nháp sẽ xóa")).toBeNull();
    const del = sent.filter((s) => s.url.endsWith("/delete"));
    expect(del).toHaveLength(1);
    expect(del[0]!.key).toBeTruthy();
  });

  it("Idempotency: mất phản hồi sau khi máy chủ đã ghi → bấm lại gửi CÙNG khóa, không tạo SOP thứ hai; thao tác sau dùng khóa khác", async () => {
    render(<SopScreen />);
    await openNew();
    await fillInfo("SOP thử khóa idempotency");
    loseResponse = (r) => r.method === "POST" && r.url.endsWith("/v1/cropcycle/sops");
    await user.click(screen.getByRole("button", { name: "Lưu nháp" }));
    await screen.findByRole("alert");
    await user.click(screen.getByRole("button", { name: "Lưu nháp" }));
    expect((await screen.findByRole("status")).textContent).toContain("Đã lưu nháp");
    const creates = sent.filter((s) => s.url.endsWith("/v1/cropcycle/sops"));
    expect(creates).toHaveLength(2);
    expect(creates[0]!.key).toBeTruthy();
    expect(creates[1]!.key).toBe(creates[0]!.key);
    // Thư viện chỉ có một SOP tên đó.
    await user.click(screen.getByRole("button", { name: /Thư viện SOP/ }));
    const lib = await screen.findByRole("region", { name: "Thư viện SOP" });
    expect(within(lib).getAllByText("SOP thử khóa idempotency")).toHaveLength(1);
    // Thao tác kế tiếp (SOP khác) → khóa mới.
    await openNew();
    await fillInfo("SOP thử khóa idempotency 2");
    await user.click(screen.getByRole("button", { name: "Lưu nháp" }));
    await screen.findByRole("status");
    const all = sent.filter((s) => s.url.endsWith("/v1/cropcycle/sops"));
    expect(all.at(-1)!.key).not.toBe(creates[0]!.key);
  });
});
