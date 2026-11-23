import type { components } from "@/api/generated/application";

export type WeatherToday = components["schemas"]["WeatherToday"];
export type WeatherCalendar = components["schemas"]["WeatherCalendar"];
export type WeatherDay = WeatherCalendar["days"][number];
export type WeatherLunar = components["schemas"]["WeatherLunar"];

const TZ = "Asia/Ho_Chi_Minh";
const mm1 = new Intl.NumberFormat("vi-VN", { minimumFractionDigits: 1, maximumFractionDigits: 1 });

/** Lượng mưa 1 chữ số thập phân, dấu phẩy thập phân (22,0 mm — FN-TT-02/AC-04). */
export const rainText = (v: number | null | undefined) => (v == null ? "—" : `${mm1.format(v)} mm`);
export const tempText = (v: number | null | undefined) => (v == null ? "—" : `${v}°C`);
export const pctText = (v: number | null | undefined) => (v == null ? "—" : `${v}%`);
export const windText = (v: number | null | undefined, dir?: string) => (v == null ? "—" : `${v} km/h${dir ? ` ${dir}` : ""}`);

/** "Cập nhật lúc hh:mm dd/mm" theo giờ Việt Nam (FN-TT-01/EX-01). */
export function updatedText(iso: string | null | undefined): string {
  if (!iso) return "";
  const d = new Date(iso);
  const p = Object.fromEntries(new Intl.DateTimeFormat("vi-VN", { timeZone: TZ, hour: "2-digit", minute: "2-digit", day: "2-digit", month: "2-digit", hourCycle: "h23" })
    .formatToParts(d).map((x) => [x.type, x.value]));
  return `Cập nhật lúc ${p.hour}:${p.minute} ${p.day}/${p.month}`;
}

/** "dd/mm (dd/mm ÂL)" — UX-TP-04. */
export function dualDate(date: string, lunar: WeatherLunar): string {
  const [, m, d] = date.split("-");
  return `${d}/${m} (${lunar.label} ÂL)`;
}

const WEEKDAYS = ["Chủ nhật", "Thứ Hai", "Thứ Ba", "Thứ Tư", "Thứ Năm", "Thứ Sáu", "Thứ Bảy"];
/** "Thứ Hai, 23/11/2026" của một ngày YYYY-MM-DD. */
export function longDate(date: string): string {
  const [y, m, d] = date.split("-").map(Number) as [number, number, number];
  return `${WEEKDAYS[new Date(Date.UTC(y, m - 1, d)).getUTCDay()]}, ${String(d).padStart(2, "0")}/${String(m).padStart(2, "0")}/${y}`;
}
