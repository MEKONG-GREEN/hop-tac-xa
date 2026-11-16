import { render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { createApiClient } from "@/api/client";
import { SeasonDetailDialog } from "./SeasonDetailDialog";

const SEASON = "01927b3e-0ff1-7000-8000-00000000abcd";

const detail = {
  season_ref: SEASON,
  season_code: "DX",
  season_year: 2026,
  plot_code: "L-01",
  progress: { sop_steps: 5, sop_steps_done: 2, pending_logs: 1 },
  events: [{ id: "e1", action: "opened", event_time: "2026-10-01T00:00:00Z", variety_code: "ST25", sowing_date: "2026-10-05", correction_reason: null }],
};

function clientWith(respond: (req: Request) => Response) {
  return createApiClient({ baseUrl: "https://api.test", getAccessToken: () => "t", fetch: async (req) => respond(req) });
}

const auth = { api: clientWith(() => new Response("{}")) };
vi.mock("@/auth/AuthProvider", () => ({ useAuth: () => auth }));

describe("AH-22 AC2: mở vụ từ thông báo", () => {
  it("AH22_AC2: mã vụ hợp lệ → hiện tiêu đề vụ và lịch sử, mùa vụ và giống hiện theo tên trong danh mục", async () => {
    const json = (body: unknown) => new Response(JSON.stringify(body), { status: 200, headers: { "content-type": "application/json" } });
    auth.api = clientWith((req) => {
      const path = new URL(req.url).pathname;
      if (path === "/v1/masterdata/catalogs/rice_variety") return json({ items: [{ code: "ST25", name: "Lúa ST25" }] });
      if (path === "/v1/masterdata/catalogs/season") return json({ items: [{ code: "DX", name: "Đông Xuân" }] });
      expect(path).toBe(`/v1/cropcycle/seasons/${SEASON}`);
      return json(detail);
    });
    render(<SeasonDetailDialog seasonRef={SEASON} onClose={() => {}} />);
    expect(await screen.findByText("Vụ Đông Xuân 2026 — lô L-01")).toBeTruthy();
    expect(screen.getByLabelText("Lịch sử vụ").textContent).toContain("Mở vụ");
    await waitFor(() => expect(screen.getByLabelText("Lịch sử vụ").textContent).toContain("giống Lúa ST25"));
    expect(screen.getByLabelText("Lịch sử vụ").textContent).toContain("sạ 05/10/2026");
  });

  it("AH22_AC2: vụ không tồn tại (404) → hiện lỗi", async () => {
    auth.api = clientWith(
      () =>
        new Response(JSON.stringify({ type: "about:blank", title: "Không tìm thấy vụ", status: 404, code: "cropcycle.season_not_found" }), {
          status: 404,
          headers: { "content-type": "application/problem+json" },
        }),
    );
    render(<SeasonDetailDialog seasonRef={SEASON} onClose={() => {}} />);
    expect(await screen.findByRole("alert")).toBeTruthy();
    expect(screen.getByText("Chi tiết vụ")).toBeTruthy();
  });
});
