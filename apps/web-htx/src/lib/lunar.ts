// Âm lịch Việt Nam (TECH-TP-02): cùng thuật toán thiên văn của Hồ Ngọc Đức, múi giờ GMT+7, với
// internal/shared/lunar (máy chủ). Máy chủ là nguồn chính của ngày âm trong API thời tiết; bản này dùng cho mock và
// màn hình cần đổi ngày khi không gọi API.

export interface LunarDate {
  day: number;
  month: number;
  year: number;
  leap: boolean;
}

const TZ = 7;
const floor = Math.floor;

function jdFromDate(dd: number, mm: number, yy: number): number {
  const a = floor((14 - mm) / 12);
  const y = yy + 4800 - a;
  const m = mm + 12 * a - 3;
  let jd = dd + floor((153 * m + 2) / 5) + 365 * y + floor(y / 4) - floor(y / 100) + floor(y / 400) - 32045;
  if (jd < 2299161) jd = dd + floor((153 * m + 2) / 5) + 365 * y + floor(y / 4) - 32083;
  return jd;
}

function newMoon(k: number): number {
  const t = k / 1236.85, t2 = t * t, t3 = t2 * t, dr = Math.PI / 180;
  let jd1 = 2415020.75933 + 29.53058868 * k + 0.0001178 * t2 - 0.000000155 * t3;
  jd1 += 0.00033 * Math.sin((166.56 + 132.87 * t - 0.009173 * t2) * dr);
  const m = 359.2242 + 29.10535608 * k - 0.0000333 * t2 - 0.00000347 * t3;
  const mpr = 306.0253 + 385.81691806 * k + 0.0107306 * t2 + 0.00001236 * t3;
  const f = 21.2964 + 390.67050646 * k - 0.0016528 * t2 - 0.00000239 * t3;
  let c1 = (0.1734 - 0.000393 * t) * Math.sin(m * dr) + 0.0021 * Math.sin(2 * dr * m);
  c1 = c1 - 0.4068 * Math.sin(mpr * dr) + 0.0161 * Math.sin(dr * 2 * mpr);
  c1 -= 0.0004 * Math.sin(dr * 3 * mpr);
  c1 = c1 + 0.0104 * Math.sin(dr * 2 * f) - 0.0051 * Math.sin(dr * (m + mpr));
  c1 = c1 - 0.0074 * Math.sin(dr * (m - mpr)) + 0.0004 * Math.sin(dr * (2 * f + m));
  c1 = c1 - 0.0004 * Math.sin(dr * (2 * f - m)) - 0.0006 * Math.sin(dr * (2 * f + mpr));
  c1 = c1 + 0.001 * Math.sin(dr * (2 * f - mpr)) + 0.0005 * Math.sin(dr * (2 * mpr + m));
  const deltat = t < -11 ? 0.001 + 0.000839 * t + 0.0002261 * t2 - 0.00000845 * t3 - 0.000000081 * t * t3 : -0.000278 + 0.000265 * t + 0.000262 * t2;
  return jd1 + c1 - deltat;
}

function sunLongitude(jdn: number): number {
  const t = (jdn - 2451545.0) / 36525, t2 = t * t, dr = Math.PI / 180;
  const m = 357.5291 + 35999.0503 * t - 0.0001559 * t2 - 0.00000048 * t * t2;
  const l0 = 280.46645 + 36000.76983 * t + 0.0003032 * t2;
  let dl = (1.9146 - 0.004817 * t - 0.000014 * t2) * Math.sin(dr * m);
  dl += (0.019993 - 0.000101 * t) * Math.sin(dr * 2 * m) + 0.00029 * Math.sin(dr * 3 * m);
  const l = (l0 + dl) * dr;
  return l - Math.PI * 2 * floor(l / (Math.PI * 2));
}

const newMoonDay = (k: number) => floor(newMoon(k) + 0.5 + TZ / 24);
const sunSector = (day: number) => floor((sunLongitude(day - 0.5 - TZ / 24) / Math.PI) * 6);

function lunarMonth11(yy: number): number {
  const off = jdFromDate(31, 12, yy) - 2415021;
  const k = floor(off / 29.530588853);
  const nm = newMoonDay(k);
  return sunSector(nm) >= 9 ? newMoonDay(k - 1) : nm;
}

function leapMonthOffset(a11: number): number {
  const k = floor((a11 - 2415021.076998695) / 29.530588853 + 0.5);
  let i = 1;
  let arc = sunSector(newMoonDay(k + i));
  let last: number;
  do {
    last = arc;
    i++;
    arc = sunSector(newMoonDay(k + i));
  } while (arc !== last && i < 14);
  return i - 1;
}

/** Đổi ngày dương lịch (năm, tháng 1–12, ngày) sang âm lịch Việt Nam. */
export function solarToLunar(yy: number, mm: number, dd: number): LunarDate {
  const dayNumber = jdFromDate(dd, mm, yy);
  const k = floor((dayNumber - 2415021.076998695) / 29.530588853);
  let monthStart = newMoonDay(k + 1);
  if (monthStart > dayNumber) monthStart = newMoonDay(k);
  let a11 = lunarMonth11(yy);
  let b11 = a11;
  let year: number;
  if (a11 >= monthStart) {
    year = yy;
    a11 = lunarMonth11(yy - 1);
  } else {
    year = yy + 1;
    b11 = lunarMonth11(yy + 1);
  }
  const day = dayNumber - monthStart + 1;
  const diff = floor((monthStart - a11) / 29);
  let leap = false;
  let month = diff + 11;
  if (b11 - a11 > 365) {
    const leapDiff = leapMonthOffset(a11);
    if (diff >= leapDiff) {
      month = diff + 10;
      if (diff === leapDiff) leap = true;
    }
  }
  if (month > 12) month -= 12;
  if (month >= 11 && diff < 4) year--;
  return { day, month, year, leap };
}

/** Nhãn "dd/mm" (tháng nhuận thêm " nhuận") — UX-TP-04 "dd/mm (dd/mm ÂL)". */
export function lunarLabel(d: LunarDate): string {
  return `${String(d.day).padStart(2, "0")}/${String(d.month).padStart(2, "0")}${d.leap ? " nhuận" : ""}`;
}
