import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createAdminApiClient, createApiClient } from "@/api/client";
import { CATALOGS } from "@/api/mock/data";
import { mockAdminFetchNoDelay, mockFetchNoDelay } from "@/api/mock/handler";
import { CropCatalog } from "./CropCatalogScreen";

// Điều khiển lỗi giả lập: mất phản hồi sau khi máy chủ đã ghi (AC6), hoặc ép máy chủ trả 422 theo trường (AC3).
let loseResponse = false;
let forced: Response | null = null;
const created: { key: string | null }[] = [];

const isCreate = (req: Request) => req.method === "POST" && req.url.endsWith("/catalogs/rice_variety/items");
const api = createApiClient({ baseUrl: "http://mock", getAccessToken: () => "mock.setter", fetch: mockFetchNoDelay });
const adminApi = createAdminApiClient({
  baseUrl: "http://mock",
  getAccessToken: () => "mock.setter",
  fetch: async (req) => {
    if (isCreate(req)) {
      created.push({ key: req.headers.get("Idempotency-Key") });
      if (forced) {
        const r = forced;
        forced = null;
        return r;
      }
    }
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

const snapshot = structuredClone({ crop: CATALOGS.crop, rice_variety: CATALOGS.rice_variety });
beforeEach(() => {
  loseResponse = false;
  forced = null;
  created.length = 0;
});
afterEach(() => {
  CATALOGS.crop.splice(0, CATALOGS.crop.length, ...structuredClone(snapshot.crop));
  CATALOGS.rice_variety.splice(0, CATALOGS.rice_variety.length, ...structuredClone(snapshot.rice_variety));
});

async function openAddVariety() {
  await userEvent.click(await screen.findByRole("button", { name: "Thêm giống" }));
  return screen.findByRole("dialog");
}

async function fillVariety(dlg: HTMLElement, name: string, days: string, crop = "LUA") {
  await userEvent.selectOptions(within(dlg).getByLabelText(/Loại cây/), crop);
  await userEvent.clear(within(dlg).getByLabelText(/Tên giống/));
  await userEvent.type(within(dlg).getByLabelText(/Tên giống/), name);
  await userEvent.clear(within(dlg).getByLabelText(/Thời gian sinh trưởng/));
  await userEvent.type(within(dlg).getByLabelText(/Thời gian sinh trưởng/), days);
}

const save = (dlg: HTMLElement) => userEvent.click(within(dlg).getByRole("button", { name: "Lưu" }));

describe("Màn Danh mục cây trồng & giống (AH-2)", () => {
  it("AC1: thêm giống, giống xuất hiện ngay trong bảng; form không có ô mã và năng suất", async () => {
    render(<CropCatalog />);
    const dlg = await openAddVariety();
    expect(within(dlg).queryByLabelText(/Mã/)).toBeNull();
    expect(within(dlg).queryByLabelText(/Năng suất/)).toBeNull();
    await fillVariety(dlg, "Giống thử màn hình", "93");
    await save(dlg);
    expect(await screen.findByText("Giống thử màn hình")).toBeTruthy();
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(screen.getByRole("status").textContent).toContain("Đã thêm giống");
    const added = CATALOGS.rice_variety.find((v) => v.name === "Giống thử màn hình")!;
    expect(added.attrs).toEqual({ crop_code: "LUA", growth_days: "93" });
    expect(added.code).toMatch(/^[0-9a-f-]{36}$/);
  });

  it("AC2: tên trùng trong cùng loại cây → ô Tên tô đỏ kèm 'Tên đã tồn tại'", async () => {
    render(<CropCatalog />);
    const dlg = await openAddVariety();
    // "Đài Thơm 8" đã có trong Lúa: khoảng trắng thừa và chữ hoa/thường không tạo ra tên khác.
    await fillVariety(dlg, "đài   THƠM 8", "95");
    await save(dlg);
    const name = await within(dlg).findByLabelText(/Tên giống/);
    await waitFor(() => expect(name.getAttribute("aria-invalid")).toBe("true"));
    expect(within(dlg).getByText(/Tên đã tồn tại/)).toBeTruthy();
    expect((name as HTMLInputElement).value).toBe("đài   THƠM 8");
  });

  it("AC3: thời gian sinh trưởng ≤ 0 bị chặn trên form; lỗi 422 của máy chủ tô đỏ đúng ô kèm hướng dẫn", async () => {
    render(<CropCatalog />);
    const dlg = await openAddVariety();
    await fillVariety(dlg, "Giống sai số ngày", "0");
    await save(dlg);
    const days = within(dlg).getByLabelText(/Thời gian sinh trưởng/);
    expect(days.getAttribute("aria-invalid")).toBe("true");
    expect(within(dlg).getByText("Phải lớn hơn 0.")).toBeTruthy();
    expect(created).toHaveLength(0);

    // Máy chủ vẫn là nơi kiểm cuối: ép phản hồi 422 theo trường.
    await userEvent.clear(days);
    await userEvent.type(days, "95");
    forced = new Response(
      JSON.stringify({
        type: "https://errors.agrigreen.vn/validation_failed",
        title: "Dữ liệu không hợp lệ",
        status: 422,
        code: "validation_failed",
        errors: [{ field: "attrs.growth_days", reason: "Thời gian sinh trưởng phải là số nguyên lớn hơn 0 (ngày)" }],
      }),
      { status: 422, headers: { "Content-Type": "application/problem+json" } },
    );
    await save(dlg);
    expect(await within(dlg).findByText("Thời gian sinh trưởng phải là số nguyên lớn hơn 0 (ngày)")).toBeTruthy();
    await waitFor(() => expect(within(dlg).getByLabelText(/Thời gian sinh trưởng/).getAttribute("aria-invalid")).toBe("true"));
  });

  it("AC4: xóa giống đang gắn vụ → thông báo chỉ được ẩn, trạng thái Đã ẩn, tên giống vẫn còn", async () => {
    render(<CropCatalog />);
    await userEvent.click(await screen.findByRole("button", { name: "Xóa OM5451" }));
    const dlg = await screen.findByRole("dialog");
    await userEvent.click(within(dlg).getByRole("button", { name: "Xóa" }));
    await waitFor(() => expect(screen.getByRole("status").textContent).toContain("chỉ được ẩn, không xóa"));
    const row = (await screen.findByText("OM5451")).closest("tr")!;
    expect(within(row).getByText("Đã ẩn")).toBeTruthy();
    expect(CATALOGS.rice_variety.find((v) => v.code === "OM5451")?.active).toBe(false);
  });

  it("xóa giống chưa dùng ở đâu → biến khỏi danh sách", async () => {
    render(<CropCatalog />);
    const dlg = await openAddVariety();
    await fillVariety(dlg, "Giống sẽ xóa", "80");
    await save(dlg);
    await userEvent.click(await screen.findByRole("button", { name: "Xóa Giống sẽ xóa" }));
    await userEvent.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "Xóa" }));
    await waitFor(() => expect(screen.queryByText("Giống sẽ xóa")).toBeNull());
    expect(screen.getByRole("status").textContent).toContain("Đã xóa");
  });

  it("AC6: mất mạng khi lưu → báo không kết nối được, giữ nguyên form; lưu lại cùng khóa, chỉ tạo một giống", async () => {
    render(<CropCatalog />);
    const dlg = await openAddVariety();
    await fillVariety(dlg, "Giống mất mạng", "91");
    loseResponse = true;
    await save(dlg);
    expect(await within(dlg).findByText(/Không kết nối được máy chủ/)).toBeTruthy();
    expect((within(dlg).getByLabelText(/Tên giống/) as HTMLInputElement).value).toBe("Giống mất mạng");
    expect((within(dlg).getByLabelText(/Thời gian sinh trưởng/) as HTMLInputElement).value).toBe("91");
    expect((within(dlg).getByLabelText(/Loại cây/) as HTMLSelectElement).value).toBe("LUA");

    await save(dlg);
    expect(await screen.findByText("Giống mất mạng")).toBeTruthy();
    expect(created).toHaveLength(2);
    expect(created[1]!.key).toBeTruthy();
    expect(created[1]!.key).toBe(created[0]!.key);
    expect(CATALOGS.rice_variety.filter((v) => v.name === "Giống mất mạng")).toHaveLength(1);

    // Sau khi thành công, lần thêm kế tiếp là thao tác mới → khóa khác.
    const dlg2 = await openAddVariety();
    await fillVariety(dlg2, "Giống thứ hai", "92");
    await save(dlg2);
    await screen.findByText("Giống thứ hai");
    expect(created).toHaveLength(3);
    expect(created[2]!.key).not.toBe(created[0]!.key);
  });

  it("tab Loại cây: thêm loại cây có cờ là lúa; ẩn loại cây còn giống đang hiện bị chặn kèm số giống", async () => {
    render(<CropCatalog />);
    await userEvent.click(await screen.findByRole("tab", { name: "Loại cây" }));
    await userEvent.click(await screen.findByRole("button", { name: "Thêm loại cây" }));
    const dlg = await screen.findByRole("dialog");
    await userEvent.type(within(dlg).getByLabelText(/Tên loại cây/), "Ngô thử");
    await userEvent.click(within(dlg).getByLabelText("Là lúa"));
    await save(dlg);
    const row = (await screen.findByText("Ngô thử")).closest("tr")!;
    expect(within(row).getByText("Có")).toBeTruthy();
    expect(CATALOGS.crop.find((c) => c.name === "Ngô thử")?.attrs).toEqual({ is_rice: "true" });

    await userEvent.click(screen.getByRole("button", { name: "Ẩn Lúa" }));
    await userEvent.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "Ẩn" }));
    expect(await screen.findByText(/Loại cây còn \d+ giống đang hiển thị/)).toBeTruthy();
    expect(CATALOGS.crop.find((c) => c.code === "LUA")?.active).toBe(true);
  });

  it("mục crop cũ có nhóm cha không hiện ở danh sách loại cây", async () => {
    render(<CropCatalog />);
    await userEvent.click(await screen.findByRole("tab", { name: "Loại cây" }));
    expect(await screen.findByText("Cây ăn quả")).toBeTruthy();
    expect(screen.queryByText("Rau muống")).toBeNull();
  });
});
