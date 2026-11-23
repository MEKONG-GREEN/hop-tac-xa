// Cùng bộ mốc với internal/shared/lunar (TECH-TP-02): ví dụ BRD, Tết, tháng nhuận 2025 (6) và 2028 (5).
import { expect, it } from "vitest";
import { lunarLabel, solarToLunar } from "./lunar";

it.each([
  [2026, 11, 3, "25/09", 2026],
  [2026, 11, 9, "01/10", 2026],
  [2026, 11, 23, "15/10", 2026],
  [2026, 2, 17, "01/01", 2026],
  [2026, 2, 16, "29/12", 2025],
  [2027, 2, 6, "01/01", 2027],
  [2028, 1, 26, "01/01", 2028],
  [2025, 7, 25, "01/06 nhuận", 2025],
  [2025, 8, 23, "01/07", 2025],
  [2028, 6, 23, "01/05 nhuận", 2028],
])("%i-%i-%i → %s ÂL", (y, m, d, label, year) => {
  const l = solarToLunar(y, m, d);
  expect(lunarLabel(l)).toBe(label);
  expect(l.year).toBe(year);
});
