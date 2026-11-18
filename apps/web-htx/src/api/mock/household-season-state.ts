// Decimal budget for demo sessions; the actual invariant lives in PostgreSQL.
const allocated = new Map<string, bigint>();
export function scaledArea(raw: string): bigint {
  const [whole = "0", fraction = ""] = raw.split(".");
  return BigInt(whole) * 10000n + BigInt(fraction.padEnd(4, "0").slice(0, 4));
}
export function decimalArea(value: bigint): string { return `${value / 10000n}.${(value % 10000n).toString().padStart(4, "0")}`; }
export function allocateHouseholdMock(coop: string, household: string, area: string): void {
  const key = `${coop}/${household}`; allocated.set(key, (allocated.get(key) ?? 0n) + scaledArea(area));
}
export function householdMockArea<T extends { household_ref: string; active_area_ha: string; remaining_area_ha: string | null; can_open_season: boolean; open_season_blocked_reason: string }>(coop: string, h: T): T {
  const used = allocated.get(`${coop}/${h.household_ref}`) ?? 0n;
  if (!used || h.remaining_area_ha === null) return h;
  const remaining = scaledArea(h.remaining_area_ha) - used;
  return { ...h, active_area_ha: decimalArea(scaledArea(h.active_area_ha) + used), remaining_area_ha: decimalArea(remaining > 0n ? remaining : 0n), can_open_season: h.can_open_season && remaining > 0n, open_season_blocked_reason: remaining > 0n ? h.open_season_blocked_reason : "Hết diện tích để mở vụ." };
}
