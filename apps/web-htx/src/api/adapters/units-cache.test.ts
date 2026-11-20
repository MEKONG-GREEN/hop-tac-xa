import { describe, expect, it, vi } from "vitest";
import { createApiClient } from "../client";
import { invalidateUnitResolver, unitResolver } from "./units";

const unit = (name: string) => ({ code: "P-LOCAL", name, level: "province", parent_code: null, valid_from: "2026-11-19", valid_to: null, successor_codes: [], active: true });

describe("AH-271: mutable unit labels and session isolation", () => {
  it("sau sửa danh mục, resolver đã được giữ vẫn lấy tên mới", async () => {
    let name = "Tên cũ", requests = 0;
    const api = createApiClient({ baseUrl: "https://api.test", getAccessToken: () => "test", fetch: async () => { requests++; return Response.json(unit(name)); } });
    const held = unitResolver(api);
    expect((await held.get("P-LOCAL"))?.name).toBe("Tên cũ");
    name = "Tên đã sửa"; invalidateUnitResolver(api);
    expect((await held.get("P-LOCAL"))?.name).toBe("Tên đã sửa"); expect(requests).toBe(2);
  });
  it("phản hồi từ phiên cũ về muộn không đi vào nhãn của phiên mới", async () => {
    let token = "old", requests = 0, finish: ((r: Response) => void) | undefined;
    const api = createApiClient({ baseUrl: "https://api.test", getAccessToken: () => token, fetch: async (request) => {
      requests++;
      if (request.headers.get("Authorization") === "Bearer old") return new Promise<Response>((resolve) => { finish = resolve; });
      return Response.json(unit("Tên của tenant mới"));
    } });
    const held = unitResolver(api), pending = held.get("P-LOCAL");
    await vi.waitFor(() => expect(finish).toBeDefined());
    token = "new"; invalidateUnitResolver(api); finish!(Response.json(unit("Tên riêng của tenant cũ")));
    expect((await pending)?.name).toBe("Tên của tenant mới");
    expect((await held.get("P-LOCAL"))?.name).toBe("Tên của tenant mới"); expect(requests).toBe(2);
  });
  it("cache nhãn hết hạn trong một phút để lượt tải sau thấy sửa từ nơi khác", async () => {
    let now = 1000, name = "Ban đầu";
    const clock = vi.spyOn(Date, "now").mockImplementation(() => now);
    try {
      const api = createApiClient({ baseUrl: "https://api.test", getAccessToken: () => "test", fetch: async () => Response.json(unit(name)) });
      const held = unitResolver(api);
      expect((await held.get("P-LOCAL"))?.name).toBe("Ban đầu");
      name = "Đã đổi ở nơi khác"; now += 60_001;
      expect((await held.get("P-LOCAL"))?.name).toBe("Đã đổi ở nơi khác");
    } finally { clock.mockRestore(); }
  });
});
