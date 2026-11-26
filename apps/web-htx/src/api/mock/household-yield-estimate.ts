// AH-87: sản lượng lúa, rơm dự kiến của vụ theo hộ cho bản demo. Số thập phân trên BigInt (không float); bất biến
// thật (sổ chỉ ghi thêm, kiểm số, quyền) nằm ở máy chủ. Hệ số rơm demo dùng mặc định 35 %.
import type { components } from "../generated/application";
import { Decimal } from "@/lib/exactDecimal";
import { CATALOGS } from "./data";

export type YieldEstimate = components["schemas"]["HouseholdYieldEstimate"];

export const MOCK_CONVENTIONAL_YIELD = "6";
export const MOCK_STRAW_PCT = "35";
const HUNDRED = Decimal.fromInt(100);

function dec(raw: string): Decimal {
  const d = Decimal.parse(raw);
  if (!d) throw new RangeError(`số không hợp lệ: ${raw}`);
  return d;
}

/** Chuỗi 2 chữ số lẻ, làm tròn half-up (như StringFixed(2) của máy chủ). */
export function tonnes(d: Decimal): string {
  const c = d.round(2).shift(2).intPart();
  const neg = c < 0n, a = neg ? -c : c;
  return `${neg ? "-" : ""}${a / 100n}.${(a % 100n).toString().padStart(2, "0")}`;
}

export function isRiceCrop(crop: string): boolean {
  return CATALOGS.crop.some((c) => c.code === crop && c.attrs.is_rice === "true");
}

/** Tính lại giá trị hệ thống; rơm theo lúa đang có hiệu lực (giá trị nhập, nếu không có thì giá trị tính). */
export function recomputeEstimate(e: YieldEstimate): YieldEstimate {
  const computedRice = tonnes(dec(e.area_ha).mul(dec(e.conventional_yield_t_per_ha)));
  const rice = e.entered_rice_t ?? computedRice;
  const computedStraw = e.is_rice && e.straw_pct ? tonnes(dec(rice).mul(dec(e.straw_pct)).divRound(HUNDRED, 8)) : null;
  return { ...e, computed_rice_t: computedRice, computed_straw_t: computedStraw, rice_t: rice,
    straw_t: e.is_rice ? e.entered_straw_t ?? computedStraw : null, entered_straw_t: e.is_rice ? e.entered_straw_t : null };
}

export function initialEstimate(crop: string, area: string, actor: string, at: string): YieldEstimate {
  const rice = isRiceCrop(crop);
  return recomputeEstimate({ seq: 1, is_rice: rice, area_ha: tonnes(dec(area)), conventional_yield_t_per_ha: MOCK_CONVENTIONAL_YIELD,
    straw_pct: rice ? MOCK_STRAW_PCT : null, computed_rice_t: "0.00", computed_straw_t: null, entered_rice_t: null, entered_straw_t: null,
    rice_t: "0.00", straw_t: null, updated_at: at, updated_by: actor });
}

/** Kiểm giá trị tấn như máy chủ (KH-26): không âm, tối đa 2 chữ số lẻ, không quá 99999999.99. */
export function tonnesError(raw: string): string | null {
  if (!/^[0-9]{1,30}(\.[0-9]{1,2})?$/.test(raw)) return "Nhập số tấn không âm, tối đa 2 chữ số thập phân";
  if (dec(raw).gt(dec("99999999.99"))) return "Số tấn vượt giới hạn lưu trữ (tối đa 99999999.99)";
  return null;
}
