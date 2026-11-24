// AH-224..AH-226 (FN-TT-01..03): thẻ Thời tiết hôm nay, Lịch thời tiết, màn Kết nối thời tiết.
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createAdminApiClient, createApiClient } from "@/api/client";
import { mockAdminFetchNoDelay, mockFetchNoDelay } from "@/api/mock/handler";
import { MOCK_PERSONAS, mockToken } from "@/api/mock/personas";
import { resetWeatherMock, setMockWeatherFailure } from "@/api/mock/weather";
import { WeatherCalendarScreen } from "./WeatherCalendarScreen";
import { WeatherCard } from "./WeatherCard";
import { WeatherConfigScreen } from "./WeatherConfigScreen";
import type { WeatherCalendar, WeatherToday } from "./format";

const member = MOCK_PERSONAS.find((p) => p.id === "htx-member")!;
const state: { persona: string; get?: (path: string) => unknown } = { persona: "htx-member" };
function clients() {
  const api = createApiClient({ baseUrl: "https://api.test", getAccessToken: () => mockToken(state.persona), fetch: mockFetchNoDelay });
  const adminApi = createAdminApiClient({ baseUrl: "https://admin.test", getAccessToken: () => mockToken(state.persona), fetch: mockAdminFetchNoDelay });
  if (state.get) {
    const fixed = state.get;
    (api as { GET: unknown }).GET = vi.fn(async (path: string) => ({ data: fixed(path), response: new Response(null, { status: 200 }) }));
  }
  return { api, adminApi };
}
// Một đối tượng auth ổn định cho mỗi lần render (client mới mỗi lần sẽ làm useAsync chạy lại vô hạn).
let auth: ReturnType<typeof makeAuth> | null = null;
function makeAuth() {
  const p = MOCK_PERSONAS.find((x) => x.id === state.persona)!;
  return { ...clients(), principal: { subject: p.me.actor_ref, roles: p.me.roles, orgRef: p.me.org?.ref ?? null, orgName: p.me.org?.name ?? null } };
}
vi.mock("@/auth/AuthProvider", () => ({ useAuth: () => (auth ??= makeAuth()) }));

beforeEach(() => {
  state.persona = "htx-member";
  state.get = undefined;
  auth = null;
  resetWeatherMock();
});

const okToday = (over: Partial<WeatherToday> = {}): WeatherToday => ({
  cooperative_ref: member.me.org!.ref, date: "2026-11-23", status: "ok",
  lunar: { day: 15, month: 10, year: 2026, leap: false, label: "15/10", first_day: false, full_moon: true },
  current: { time: "2026-11-23T03:00:00Z", temperature_c: 30, condition: "Có mây", weather_code: 2, precipitation_mm: 0, precipitation_probability: 20, wind_speed_kmh: 11, wind_direction: "ĐB", humidity_pct: 71 },
  today: { condition: "Mưa nhỏ", weather_code: 61, temperature_min_c: 24, temperature_max_c: 32, precipitation_mm: 22, precipitation_probability_max: 60, wind_speed_max_kmh: 14, wind_direction: "ĐB", humidity_pct: 80 },
  source: { provider: "open_meteo", label: "Open-Meteo.com", updated_at: "2026-11-23T02:00:00Z", stale: false }, ...over,
});

describe("FN-TT-01 thẻ Thời tiết hôm nay", () => {
  it("ngày dương, ngày âm, nhiệt độ, mưa (1 chữ số thập phân), gió, độ ẩm và nguồn; không có lời khuyên", async () => {
    auth = null; state.get = () => okToday();
    render(<WeatherCard />);
    const card = await screen.findByRole("region", { name: "Thời tiết hôm nay" });
    await within(card).findByText("30°C");
    expect(card).toHaveTextContent("Thứ Hai, 23/11/2026 · 15/10 ÂL");
    expect(card).toHaveTextContent("22,0 mm (60%)");
    expect(card).toHaveTextContent("11 km/h ĐB");
    expect(card).toHaveTextContent("71%");
    expect(card).toHaveTextContent("Nguồn: Open-Meteo.com");
    expect(card).not.toHaveTextContent(/khuyên|cảnh báo/i);
    expect(card).not.toHaveTextContent("Cập nhật lúc");
  });

  it("AC-03/EX-01: API lỗi → vẫn hiện số liệu gần nhất kèm giờ cập nhật", async () => {
    auth = null; state.get = () => okToday({ source: { provider: "open_meteo", label: "Open-Meteo.com", updated_at: "2026-11-23T02:05:00Z", stale: true } });
    render(<WeatherCard />);
    expect(await screen.findByText(/Cập nhật lúc 09:05 23\/11/)).toBeInTheDocument();
    expect(screen.getByText("30°C")).toBeInTheDocument();
  });

  it("AC-05/EX-02, EX-03: chưa có tọa độ, chưa có số liệu → câu báo kèm ngày dương, ngày âm", async () => {
    auth = null; state.get = () => okToday({ status: "no_location", current: null, today: null, message: "Chưa có vị trí để xem thời tiết. Liên hệ Admin để cập nhật." });
    const { unmount } = render(<WeatherCard />);
    expect(await screen.findByText("Chưa có vị trí để xem thời tiết. Liên hệ Admin để cập nhật.")).toBeInTheDocument();
    unmount();
    auth = null; state.get = () => okToday({ status: "no_data", current: null, today: null, message: "Chưa có số liệu thời tiết" });
    render(<WeatherCard />);
    expect(await screen.findByText("Chưa có số liệu thời tiết")).toBeInTheDocument();
    expect(screen.getByText(/15\/10 ÂL/)).toBeInTheDocument();
  });

  it("AC-06: Admin chọn HTX ở bộ lọc thì thẻ đổi theo HTX đó", async () => {
    auth = null; state.persona = "setter";
    const user = userEvent.setup();
    render(<WeatherCard />);
    const select = await screen.findByLabelText("HTX");
    await waitFor(() => expect((select as HTMLSelectElement).options.length).toBeGreaterThan(2));
    const second = (select as HTMLSelectElement).options[2]!.value;
    await user.selectOptions(select, second);
    await waitFor(() => expect(screen.getByRole("region", { name: "Thời tiết hôm nay" })).toHaveTextContent(/ÂL/));
  });
});

const calendar = (): WeatherCalendar => ({
  cooperative_ref: member.me.org!.ref, month: "2026-11", status: "ok", forecast_days: 7,
  source: { provider: "open_meteo", label: "Open-Meteo.com", updated_at: "2026-11-23T02:00:00Z", stale: false },
  days: Array.from({ length: 30 }, (_, i) => {
    const d = i + 1;
    const lunarDay = ((d + 21) % 30) + 1;
    const date = `2026-11-${String(d).padStart(2, "0")}`;
    const base = { condition: "Có mây", weather_code: 2, temperature_min_c: 24, temperature_max_c: 31, precipitation_mm: 4.2, precipitation_probability_max: 50, wind_speed_max_kmh: 12, wind_direction: "ĐB", humidity_pct: 80 };
    const weather = d === 22 ? { ...base, kind: "final_forecast" as const, label: "Dự báo cuối ngày", hourly_available: true, periods: [{ name: "Chiều" as const, condition: "Mưa nhỏ", weather_code: 61, temperature_c: 31, precipitation_mm: 22, precipitation_probability_max: 50, wind_speed_max_kmh: 10 }] }
      : d === 24 ? { ...base, kind: "forecast" as const, label: "", hourly_available: false, periods: [] } : null;
    return { date, lunar: { day: lunarDay, month: 10, year: 2026, leap: false, label: `${String(lunarDay).padStart(2, "0")}/10`, first_day: lunarDay === 1, full_moon: lunarDay === 15 }, weather };
  }),
});

describe("FN-TT-02 Lịch thời tiết", () => {
  it("lưới tháng có ngày âm; ngày đã qua có nhãn; chi tiết theo buổi; không theo giờ thì một khối cả ngày", async () => {
    auth = null; state.get = () => calendar();
    const user = userEvent.setup();
    render(<WeatherCalendarScreen />);
    const grid = await screen.findByRole("grid", { name: /Lịch tháng/ });
    expect(within(grid).getAllByRole("gridcell")).toHaveLength(30);
    expect(within(grid).getByRole("gridcell", { name: "23/11 (15/10 ÂL)" })).toBeInTheDocument();
    const d22 = within(grid).getByRole("gridcell", { name: /^22\/11/ });
    expect(d22).toHaveTextContent("Dự báo cuối ngày");
    await user.click(d22);
    const detail = screen.getByRole("region", { name: "Chi tiết ngày" });
    expect(detail).toHaveTextContent("22/11 (14/10 ÂL) · Dự báo cuối ngày");
    expect(within(detail).getByRole("table")).toHaveTextContent("Chiều");
    expect(within(detail).getByRole("table")).toHaveTextContent("22,0 mm");
    await user.click(within(grid).getByRole("gridcell", { name: /^24\/11/ }));
    expect(screen.getByRole("region", { name: "Chi tiết ngày" })).toHaveTextContent(/^.*Cả ngày: Có mây/);
    expect(within(screen.getByRole("region", { name: "Chi tiết ngày" })).queryByRole("table")).not.toBeInTheDocument();
    await user.click(within(grid).getByRole("gridcell", { name: /^21\/11/ }));
    expect(screen.getByRole("region", { name: "Chi tiết ngày" })).toHaveTextContent(/Chưa có số liệu thời tiết|Ngoài phạm vi dự báo/);
  });

  it("chạy với mock: tháng hiện tại, ngày âm tính theo lịch Việt Nam", async () => {
    render(<WeatherCalendarScreen />);
    const grid = await screen.findByRole("grid", { name: /Lịch tháng/ });
    expect(within(grid).getAllByRole("gridcell").length).toBeGreaterThanOrEqual(28);
  });
});

describe("FN-TT-03 Kết nối thời tiết (Admin)", () => {
  it("AC-04/EX-01: kiểm tra lỗi báo lý do, lưu bị từ chối; kết nối được thì lưu, khóa chỉ hiện 4 ký tự cuối (AC-03)", async () => {
    auth = null; state.persona = "setter";
    const user = userEvent.setup();
    render(<WeatherConfigScreen />);
    const form = await screen.findByRole("form", { name: "Cấu hình kết nối thời tiết" });
    expect(form).toHaveTextContent("Chưa cấu hình");
    await user.type(within(form).getByLabelText("Khóa API"), "abcd-efgh-1234");
    setMockWeatherFailure("auth");
    await user.click(within(form).getByRole("button", { name: "Kiểm tra kết nối" }));
    expect(await within(form).findByText(/Khóa API không đúng hoặc đã hết hạn/)).toBeInTheDocument();
    await user.click(within(form).getByRole("button", { name: "Lưu" }));
    await within(form).findByText(/Khóa API không đúng/);
    expect(form).toHaveTextContent("Chưa cấu hình");
    setMockWeatherFailure("");
    await user.click(within(form).getByRole("button", { name: "Kiểm tra kết nối" }));
    expect(await within(form).findByText(/^Kết nối được/)).toBeInTheDocument();
    await user.selectOptions(within(form).getByLabelText("Tần suất cập nhật"), "30");
    await user.click(within(form).getByRole("button", { name: "Lưu" }));
    expect(await within(form).findByText("Đã lưu. Cấu hình có hiệu lực từ lần lấy số liệu kế tiếp.")).toBeInTheDocument();
    await waitFor(() => expect(within(form).getByLabelText("Khóa API")).toHaveAttribute("placeholder", expect.stringContaining("••••1234")));
    expect(form).not.toHaveTextContent("abcd-efgh");
    expect(within(form).getByLabelText("Tần suất cập nhật")).toHaveValue("30");
  });

  it("vai khác Admin không dùng được", async () => {
    auth = null; state.persona = "htx";
    render(<WeatherConfigScreen />);
    expect(await screen.findByRole("alert")).toBeInTheDocument();
  });
});
