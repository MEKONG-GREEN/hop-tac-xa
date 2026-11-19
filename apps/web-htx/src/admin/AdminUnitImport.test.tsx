import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { createAdminApiClient } from "@/api/client";
import { mockAdminFetchNoDelay } from "@/api/mock/handler";
import { mockToken } from "@/api/mock/personas";
import { AuthContextForTests, type AuthContextValue } from "@/auth/AuthProvider";
import { AdminUnitImport } from "./AdminUnitImport";

const saved: string[] = [];
vi.mock("@/lib/files", async (original) => ({ ...await original<Record<string, unknown>>(), saveBlob: (_blob: Blob, name: string) => saved.push(name) }));

describe("AH-271: nhập danh mục", () => {
  it("tải mẫu đúng định dạng và retry mất phản hồi giữ key, nhận lỗi theo dòng", async () => {
    const writes: { key: string | null; body: string }[] = []; const imported = vi.fn(); let lose = true;
    const adminApi = createAdminApiClient({ baseUrl: "https://admin.test", getAccessToken: () => mockToken("setter"), fetch: async (request) => {
      if (request.method === "POST") writes.push({ key: request.headers.get("Idempotency-Key"), body: await request.clone().text() });
      const response = await mockAdminFetchNoDelay(request);
      if (request.method === "POST" && lose) { lose = false; throw new TypeError("response lost"); }
      return response;
    } });
    render(<AuthContextForTests.Provider value={{ adminApi } as AuthContextValue}><AdminUnitImport onClose={() => {}} onImported={imported} /></AuthContextForTests.Provider>);
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "Tải file mẫu" }));
    await waitFor(() => expect(saved).toContain("don-vi-hanh-chinh-gia-lap.csv"));
    const suffix = crypto.randomUUID();
    const file = new File([`Tên,Cấp,Tỉnh / thành phố trực thuộc\nTỉnh ${suffix},Tỉnh,\nXã ${suffix},Xã,Tỉnh ${suffix}\nXã thiếu tỉnh,Xã,\n`], "don-vi.csv", { type: "text/csv" });
    await user.upload(screen.getByLabelText("Chọn tệp"), file);
    await waitFor(() => expect(screen.getByRole("button", { name: "Nhập" })).not.toBeDisabled());
    await user.click(screen.getByRole("button", { name: "Nhập" }));
    await screen.findByRole("alert");
    await user.click(screen.getByRole("button", { name: "Nhập" }));
    await screen.findByText("Đã thêm 2 dòng; 1 dòng lỗi.");
    expect(writes).toHaveLength(2); expect(writes[0]!.key).toBeTruthy(); expect(writes[1]).toEqual(writes[0]);
    expect(imported).toHaveBeenCalledTimes(1);
    expect(screen.getByRole("cell", { name: "4" })).toBeInTheDocument();
    expect(screen.getByRole("cell", { name: /Chọn tỉnh/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Nhập" })).toBeDisabled();
  });
});
