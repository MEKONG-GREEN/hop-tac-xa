import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createApiClient } from "@/api/client";
import InboxPage from "@/app/thong-bao/page";

const SEASON = "01927b3e-0ff1-7000-8000-00000000abcd";
const TASK = "01927b3e-0ff1-7000-8000-00000000dcba";
const CAL = "01927b3e-0ff1-7000-8000-00000000ca11";

const push = vi.fn();
vi.mock("next/navigation", () => ({ useRouter: () => ({ push }) }));

const calls: { method: string; path: string }[] = [];
const auth = {
  api: createApiClient({ baseUrl: "https://api.test", getAccessToken: () => "t", fetch: async () => new Response("{}") }),
  adminApi: undefined as unknown,
  principal: { roles: ["coop_manager"], mfa: true },
};
vi.mock("@/auth/AuthProvider", () => ({ useAuth: () => auth }));

const n = (id: string, kind: string, deep_link: string, created_at: string, read_at: string | null, title: string) => ({
  id,
  kind,
  severity: "normal",
  title,
  body: "Nội dung",
  deep_link,
  subject_ref: null,
  created_at,
  read_at,
  topic: false,
});

// Đồng hồ giả: 10:00 ngày 01/10/2026 giờ Việt Nam.
const NOW = new Date("2026-10-01T03:00:00Z");
const items = [
  n("n1", "season_task_due", `/cropcycle/seasons/${SEASON}/tasks/${TASK}`, "2026-10-01T01:30:00Z", null, "Việc đến hạn: Bón phân"),
  n("n2", "season_calendar_published", `/cropcycle/calendars/${CAL}`, "2026-09-30T05:00:00Z", "2026-09-30T06:00:00Z", "Lịch thời vụ mới: Lúa"),
];

function useFakeApi() {
  auth.api = createApiClient({
    baseUrl: "https://api.test",
    getAccessToken: () => "t",
    fetch: async (req) => {
      const path = new URL(req.url).pathname;
      calls.push({ method: req.method, path });
      const json = (b: unknown) => new Response(JSON.stringify(b), { status: 200, headers: { "content-type": "application/json" } });
      if (req.method === "GET") return json({ items, next_cursor: null });
      if (path.endsWith("/read-all")) return json({ marked: 1 });
      return json({ ...items[0], read_at: "2026-10-01T03:00:00Z" });
    },
  });
}

describe("AH-22 AC1/AC2: trang trung tâm thông báo", () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ["Date"], now: NOW });
    calls.length = 0;
    push.mockClear();
    useFakeApi();
  });
  afterEach(() => {
    cleanup();
    vi.useRealTimers();
  });

  it("AH22_AC1: nhóm theo ngày, giờ tạo, trạng thái đọc, nút Đọc tất cả", async () => {
    render(<InboxPage />);
    expect(await screen.findByRole("heading", { name: "Hôm nay" })).toBeTruthy();
    expect(screen.getByRole("heading", { name: "Hôm qua" })).toBeTruthy();
    expect(screen.getByText("08:30")).toBeTruthy(); // 01:30Z = 08:30 giờ Việt Nam
    expect(screen.getByText("12:00")).toBeTruthy();
    expect(screen.getByText("Chưa đọc")).toBeTruthy();
    expect(screen.getByText("Đã đọc")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Đọc tất cả" }));
    await waitFor(() => expect(calls.some((c) => c.method === "POST" && c.path === "/v1/notifications/read-all")).toBe(true));
  });

  it("AH22_AC2: bấm thông báo việc → đánh dấu đã đọc rồi mở vụ", async () => {
    render(<InboxPage />);
    const today = (await screen.findByRole("heading", { name: "Hôm nay" })).closest("section")!;
    fireEvent.click(within(today).getByRole("button", { name: /Việc đến hạn: Bón phân/ }));
    await waitFor(() => expect(push).toHaveBeenCalledWith(`/htx/mua-vu?season=${SEASON}`));
    expect(calls.some((c) => c.method === "POST" && c.path === "/v1/notifications/n1/read")).toBe(true);
  });

  it("AH22_AC2: thông báo lịch chưa có màn đích → ở lại trung tâm, ghi 'Chưa có màn hình liên kết.'", async () => {
    render(<InboxPage />);
    const yesterday = (await screen.findByRole("heading", { name: "Hôm qua" })).closest("section")!;
    expect(within(yesterday).getByText("Chưa có màn hình liên kết.")).toBeTruthy();
    fireEvent.click(within(yesterday).getByRole("button", { name: /Lịch thời vụ mới: Lúa/ }));
    await waitFor(() => expect(calls.filter((c) => c.method === "GET").length).toBeGreaterThan(1)); // tải lại danh sách
    expect(push).not.toHaveBeenCalled();
  });
});
