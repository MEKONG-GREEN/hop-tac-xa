import { renderHook, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { createApiClient } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { useCatalog, useCatalogNames } from "./common";

const auth = { api: createApiClient({ baseUrl: "http://mock", getAccessToken: () => "mock.htx", fetch: mockFetchNoDelay }) };
vi.mock("@/auth/AuthProvider", () => ({ useAuth: () => auth }));

describe("AH-2 AC4: vụ cũ hiển thị đúng tên giống", () => {
  it("useCatalogNames trả cả tên của giống đã ẩn (IR50404), còn danh sách chọn chỉ có giống đang hiện", async () => {
    const names = renderHook(() => useCatalogNames("rice_variety"));
    await waitFor(() => expect(names.result.current.size).toBeGreaterThan(0));
    expect(names.result.current.get("IR50404")).toBe("IR50404");
    expect(names.result.current.get("DT8")).toBe("Đài Thơm 8");

    const options = renderHook(() => useCatalog("rice_variety"));
    await waitFor(() => expect(options.result.current.length).toBeGreaterThan(0));
    expect(options.result.current.some((i) => i.code === "IR50404")).toBe(false);
  });
});
