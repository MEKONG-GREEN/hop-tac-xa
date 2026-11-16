import { render, renderHook, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createAdminApiClient, createApiClient } from "@/api/client";
import { CATALOGS } from "@/api/mock/data";
import { seasonUseCount } from "@/api/mock/cropcycle";
import { mockAdminFetchNoDelay, mockFetchNoDelay } from "@/api/mock/handler";
import { useCatalog, useCatalogNames } from "@/htx/common";
import { SeasonCatalog } from "./SeasonCatalogScreen";

// Điều khiển lỗi giả lập: mất phản hồi sau khi máy chủ đã ghi (AC1: khóa dùng lại khi thử lại).
let loseResponse = false;
const created: { key: string | null }[] = [];

const isCreate = (req: Request) => req.method === "POST" && req.url.endsWith("/catalogs/season/items");
const api = createApiClient({ baseUrl: "http://mock", getAccessToken: () => "mock.setter", fetch: mockFetchNoDelay });
const adminApi = createAdminApiClient({
  baseUrl: "http://mock",
  getAccessToken: () => "mock.setter",
  fetch: async (req) => {
    if (isCreate(req)) created.push({ key: req.headers.get("Idempotency-Key") });
    const res = await mockAdminFetchNoDelay(req);
    if (loseResponse && isCreate(req)) {
      loseResponse = false;
      throw new TypeError("network response lost");
    }
    return res;
  },
});
const auth = { api, adminApi, principal: { roles: ["platform_admin"] as string[], mfa: true } };
vi.mock("@/auth/AuthProvider", () => ({ useAuth: () => auth }));

const snapshot = structuredClone(CATALOGS.season);
beforeEach(() => {
  loseResponse = false;
  created.length = 0;
});
afterEach(() => {
  CATALOGS.season.splice(0, CATALOGS.season.length, ...structuredClone(snapshot));
});

async function openAdd() {
  await userEvent.click(await screen.findByRole("button", { name: "Thêm mùa vụ" }));
  return screen.findByRole("dialog");
}

async function fillName(dlg: HTMLElement, name: string) {
  await userEvent.clear(within(dlg).getByLabelText(/Tên mùa vụ/));
  await userEvent.type(within(dlg).getByLabelText(/Tên mùa vụ/), name);
}

const save = (dlg: HTMLElement) => userEvent.click(within(dlg).getByRole("button", { name: "Lưu" }));

describe("Màn Danh mục mùa vụ (AH-7)", () => {
  it("AC1: thêm mùa vụ, mục hiện ngay trong bảng và trong danh sách chọn Vụ/Năm; form không có ô mã", async () => {
    render(<SeasonCatalog />);
    const dlg = await openAdd();
    expect(within(dlg).queryByLabelText(/Mã/)).toBeNull();
    await fillName(dlg, "Vụ thử màn hình");
    await userEvent.type(within(dlg).getByLabelText("Mô tả"), "Mùa vụ thử");
    await userEvent.selectOptions(within(dlg).getByLabelText("Tháng bắt đầu"), "11");
    await userEvent.selectOptions(within(dlg).getByLabelText("Tháng kết thúc"), "3");
    await save(dlg);
    expect(await screen.findByText("Vụ thử màn hình")).toBeTruthy();
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(screen.getByRole("status").textContent).toContain("Đã thêm mùa vụ");
    const added = CATALOGS.season.find((s) => s.name === "Vụ thử màn hình")!;
    expect(added.attrs).toEqual({ description: "Mùa vụ thử", start_month: "11", end_month: "3" });
    expect(added.code).toMatch(/^[0-9A-F]{32}$/);
    const row = screen.getByText("Vụ thử màn hình").closest("tr")!;
    expect(within(row).getByText(/Dự kiến: Tháng 11 – Tháng 3/)).toBeTruthy();

    const picker = renderHook(() => useCatalog("season"));
    await waitFor(() => expect(picker.result.current.some((s) => s.name === "Vụ thử màn hình")).toBe(true));
  });

  it("AC1: chỉ chọn một tháng bị chặn trên form, không gửi lên máy chủ", async () => {
    render(<SeasonCatalog />);
    const dlg = await openAdd();
    await fillName(dlg, "Vụ một tháng");
    await userEvent.selectOptions(within(dlg).getByLabelText("Tháng bắt đầu"), "4");
    await save(dlg);
    expect(within(dlg).getByLabelText("Tháng kết thúc").getAttribute("aria-invalid")).toBe("true");
    expect(within(dlg).getByText(/Nhập cả tháng bắt đầu và tháng kết thúc/)).toBeTruthy();
    expect(created).toHaveLength(0);
  });

  it("AC1: mất phản hồi sau khi đã ghi → giữ form; bấm Lưu lại gửi đúng khóa cũ, chỉ có một mùa vụ", async () => {
    render(<SeasonCatalog />);
    const dlg = await openAdd();
    await fillName(dlg, "Vụ mất mạng");
    loseResponse = true;
    await save(dlg);
    expect(await within(dlg).findByText(/Không kết nối được máy chủ/)).toBeTruthy();
    expect((within(dlg).getByLabelText(/Tên mùa vụ/) as HTMLInputElement).value).toBe("Vụ mất mạng");
    await save(dlg);
    expect(await screen.findByText("Vụ mất mạng")).toBeTruthy();
    expect(created).toHaveLength(2);
    expect(created[1]!.key).toBeTruthy();
    expect(created[1]!.key).toBe(created[0]!.key);
    expect(CATALOGS.season.filter((s) => s.name === "Vụ mất mạng")).toHaveLength(1);
  });

  it("AC2: tên trùng (bỏ khoảng trắng thừa, không phân biệt hoa thường) → ô Tên báo 'Tên đã tồn tại', giữ chữ đã nhập", async () => {
    render(<SeasonCatalog />);
    const dlg = await openAdd();
    await fillName(dlg, "đông   XUÂN");
    await save(dlg);
    const name = await within(dlg).findByLabelText(/Tên mùa vụ/);
    await waitFor(() => expect(name.getAttribute("aria-invalid")).toBe("true"));
    expect(within(dlg).getByText(/Tên đã tồn tại/)).toBeTruthy();
    expect((name as HTMLInputElement).value).toBe("đông   XUÂN");
    expect(CATALOGS.season.filter((s) => s.name === "đông   XUÂN")).toHaveLength(0);
  });

  it("AC3: đổi tên mùa vụ đang được vụ dùng → bảng và danh sách chọn hiện tên mới; mã và vụ đã tạo không đổi", async () => {
    expect(seasonUseCount("DX")).toBeGreaterThan(0); // dữ liệu giả lập có vụ dùng DX
    const before = seasonUseCount("DX");
    render(<SeasonCatalog />);
    await userEvent.click(await screen.findByRole("button", { name: "Sửa Đông Xuân" }));
    const dlg = await screen.findByRole("dialog");
    await fillName(dlg, "Đông Xuân đổi tên");
    await save(dlg);
    expect(await screen.findByText("Đông Xuân đổi tên")).toBeTruthy();
    expect(CATALOGS.season.find((s) => s.code === "DX")?.name).toBe("Đông Xuân đổi tên");
    const names = renderHook(() => useCatalogNames("season"));
    await waitFor(() => expect(names.result.current.get("DX")).toBe("Đông Xuân đổi tên"));
    const picker = renderHook(() => useCatalog("season"));
    await waitFor(() => expect(picker.result.current.find((s) => s.code === "DX")?.name).toBe("Đông Xuân đổi tên"));
    expect(seasonUseCount("DX")).toBe(before);
  });

  it("AC4: xóa mùa vụ đang dùng → báo chỉ được ẩn; không còn trong danh sách chọn nhưng vụ cũ vẫn có tên; cột Đang dùng là 'N vụ'", async () => {
    render(<SeasonCatalog />);
    const used = (await screen.findByText("Hè Thu")).closest("tr")!;
    expect(within(used).getByText(/^\d+ vụ$/)).toBeTruthy();
    expect(used.textContent).not.toMatch(/lịch/);
    await userEvent.click(within(used).getByRole("button", { name: "Xóa Hè Thu" }));
    await userEvent.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "Xóa" }));
    await waitFor(() => expect(screen.getByRole("status").textContent).toContain("chỉ được ẩn, không xóa được"));
    const row = (await screen.findByText("Hè Thu")).closest("tr")!;
    expect(within(row).getByText("Đã ẩn")).toBeTruthy();
    expect(CATALOGS.season.find((s) => s.code === "HT")?.active).toBe(false);

    const picker = renderHook(() => useCatalog("season"));
    await waitFor(() => expect(picker.result.current.length).toBeGreaterThan(0));
    expect(picker.result.current.some((s) => s.code === "HT")).toBe(false);
    const names = renderHook(() => useCatalogNames("season"));
    await waitFor(() => expect(names.result.current.get("HT")).toBe("Hè Thu"));
  });

  it("AC4: xóa mùa vụ chưa dùng ở đâu → biến khỏi danh sách", async () => {
    render(<SeasonCatalog />);
    const dlg = await openAdd();
    await fillName(dlg, "Vụ sẽ xóa");
    await save(dlg);
    await userEvent.click(await screen.findByRole("button", { name: "Xóa Vụ sẽ xóa" }));
    await userEvent.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "Xóa" }));
    await waitFor(() => expect(screen.queryByText("Vụ sẽ xóa")).toBeNull());
    expect(screen.getByRole("status").textContent).toContain("Đã xóa");
  });

  it("AC4: ẩn không cần lý do, rồi hiện lại được", async () => {
    render(<SeasonCatalog />);
    await userEvent.click(await screen.findByRole("button", { name: "Ẩn Thu Đông" }));
    await userEvent.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "Ẩn" }));
    await waitFor(() => expect(CATALOGS.season.find((s) => s.code === "TD")?.active).toBe(false));
    await userEvent.click(await screen.findByRole("button", { name: "Hiện Thu Đông" }));
    await userEvent.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "Hiện lại" }));
    await waitFor(() => expect(CATALOGS.season.find((s) => s.code === "TD")?.active).toBe(true));
  });

  it("AC5: vai không phải quản trị danh mục (HTX) gọi API ghi bị từ chối 403", async () => {
    const htx = createAdminApiClient({ baseUrl: "http://mock", getAccessToken: () => "mock.htx", fetch: mockAdminFetchNoDelay });
    const res = await htx.POST("/v1/masterdata/catalogs/{catalog}/items", {
      params: { path: { catalog: "season" }, header: { "Idempotency-Key": "ah7-ac5" } },
      body: { name: "Trái phép", attrs: {} },
    });
    expect(res.response.status).toBe(403);
    expect(CATALOGS.season.some((s) => s.name === "Trái phép")).toBe(false);
  });
});

describe("Mẫu danh sách UX-01 (AH-292)", () => {
  const tab = (re: RegExp) => screen.findByRole("tab", { name: re });

  it("cột đầu là mã; tab kèm số lượng", async () => {
    render(<SeasonCatalog />);
    expect(await tab(/^Tất cả 3$/)).toBeTruthy();
    expect(screen.getByRole("tab", { name: /^Đang hiện 3$/ })).toBeTruthy();
    expect(screen.getByRole("tab", { name: /^Đã ẩn 0$/ })).toBeTruthy();
    expect(screen.getAllByRole("columnheader")[0]).toHaveAccessibleName("Mã");
    expect(screen.queryByRole("columnheader", { name: "STT" })).toBeNull();
    for (const code of ["DX", "HT", "TD"]) expect(screen.getByText(code)).toBeTruthy();
  });

  it("ẩn một mùa vụ: số trên tab đổi; tab Đã ẩn chỉ còn mùa vụ đó", async () => {
    render(<SeasonCatalog />);
    await userEvent.click(await screen.findByRole("button", { name: "Ẩn Thu Đông" }));
    await userEvent.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "Ẩn" }));
    expect(await tab(/^Đang hiện 2$/)).toBeTruthy();
    expect(screen.getByRole("tab", { name: /^Đã ẩn 1$/ })).toBeTruthy();
    await userEvent.click(screen.getByRole("tab", { name: /^Đã ẩn 1$/ }));
    expect(screen.getByText("Thu Đông")).toBeTruthy();
    expect(screen.queryByText("Đông Xuân")).toBeNull();
  });

  it("tìm không dấu; số trên tab bằng số kết quả", async () => {
    render(<SeasonCatalog />);
    await userEvent.type(await screen.findByRole("searchbox"), "dong");
    expect(screen.getByText("Đông Xuân")).toBeTruthy();
    expect(screen.getByText("Thu Đông")).toBeTruthy();
    expect(screen.queryByText("Hè Thu")).toBeNull();
    expect(screen.getByRole("tab", { name: /^Tất cả 2$/ })).toBeTruthy();
    expect(screen.getByText("2 kết quả")).toBeTruthy();
  });

  it("tab rỗng khi chưa tìm hay lọc: báo theo mục, không nhắc tìm kiếm và bộ lọc", async () => {
    render(<SeasonCatalog />);
    await userEvent.click(await screen.findByRole("tab", { name: /^Đã ẩn 0$/ }));
    expect(screen.getByText("Không có mùa vụ nào ở mục này.")).toBeTruthy();
    expect(screen.queryByText("Không có mùa vụ phù hợp tìm kiếm và bộ lọc.")).toBeNull();
    await userEvent.type(screen.getByRole("searchbox"), "dong");
    expect(screen.getByText("Không có mùa vụ phù hợp tìm kiếm và bộ lọc.")).toBeTruthy();
  });

  it("bấm ô mã mở hộp Sửa; bấm nút Xóa cuối dòng chỉ mở hộp Xóa", async () => {
    render(<SeasonCatalog />);
    await userEvent.click(await screen.findByText("DX"));
    const edit = await screen.findByRole("dialog");
    expect(within(edit).getByRole("heading", { name: "Sửa mùa vụ" })).toBeTruthy();
    expect((within(edit).getByLabelText(/Tên mùa vụ/) as HTMLInputElement).value).toBe("Đông Xuân");
    await userEvent.keyboard("{Escape}");
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    await userEvent.click(screen.getByRole("button", { name: "Xóa Hè Thu" }));
    const del = await screen.findByRole("dialog");
    expect(within(del).getByRole("heading", { name: "Xóa mùa vụ" })).toBeTruthy();
    expect(screen.queryByRole("heading", { name: "Sửa mùa vụ" })).toBeNull();
  });
});
