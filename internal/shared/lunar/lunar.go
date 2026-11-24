// Package lunar đổi ngày dương lịch sang âm lịch Việt Nam (TECH-TP-02, BRD Thời tiết & Thu gom phụ phẩm): thuật toán
// thiên văn của Hồ Ngọc Đức, múi giờ GMT+7. Tháng âm bắt đầu ở ngày có điểm sóc (trăng mới) theo giờ Việt Nam; năm có 13
// tháng thì tháng nhuận là tháng đầu tiên không chứa trung khí. Kết quả độc lập với nhà cung cấp thời tiết.
//
// Phạm vi dùng: năm 1900–2100 (đủ cho lịch vạn niên và lưu số liệu thời tiết 24 tháng). Ngoài phạm vi vẫn tính được
// nhưng sai số thiên văn tăng dần.
package lunar

import (
	"fmt"
	"math"
	"time"
)

// TimeZone là múi giờ của lịch Việt Nam (giờ).
const TimeZone = 7.0

// Date là một ngày âm lịch.
type Date struct {
	Day   int  // 1–30
	Month int  // 1–12
	Year  int  // năm âm lịch (đổi ở Tết, không ở 01/01 dương lịch)
	Leap  bool // tháng nhuận
}

// String trả dạng "dd/mm" (tháng nhuận thêm " nhuận"), dùng ở UX-TP-04 "dd/mm (dd/mm ÂL)".
func (d Date) String() string {
	s := fmt.Sprintf("%02d/%02d", d.Day, d.Month)
	if d.Leap {
		s += " nhuận"
	}
	return s
}

// FromSolar đổi ngày dương lịch (theo lịch của t ở giờ Việt Nam: chỉ dùng năm, tháng, ngày) sang âm lịch.
func FromSolar(year int, month time.Month, day int) Date {
	return convert(day, int(month), year, TimeZone)
}

// Of đổi một thời điểm sang ngày âm lịch theo giờ Việt Nam (UTC+7), bất kể múi giờ của t.
func Of(t time.Time) Date {
	vn := t.UTC().Add(7 * time.Hour)
	return FromSolar(vn.Year(), vn.Month(), vn.Day())
}

func floor(x float64) int { return int(math.Floor(x)) }

// jdFromDate: số ngày Julius của ngày dương lịch (lịch Gregory từ 15/10/1582).
func jdFromDate(dd, mm, yy int) int {
	a := (14 - mm) / 12
	y := yy + 4800 - a
	m := mm + 12*a - 3
	jd := dd + (153*m+2)/5 + 365*y + y/4 - y/100 + y/400 - 32045
	if jd < 2299161 {
		jd = dd + (153*m+2)/5 + 365*y + y/4 - 32083
	}
	return jd
}

// newMoon: thời điểm sóc thứ k tính từ 1/1/1900 (ngày Julius, giờ UT).
func newMoon(k int) float64 {
	kf := float64(k)
	t := kf / 1236.85
	t2 := t * t
	t3 := t2 * t
	dr := math.Pi / 180
	jd1 := 2415020.75933 + 29.53058868*kf + 0.0001178*t2 - 0.000000155*t3
	jd1 += 0.00033 * math.Sin((166.56+132.87*t-0.009173*t2)*dr)
	m := 359.2242 + 29.10535608*kf - 0.0000333*t2 - 0.00000347*t3
	mpr := 306.0253 + 385.81691806*kf + 0.0107306*t2 + 0.00001236*t3
	f := 21.2964 + 390.67050646*kf - 0.0016528*t2 - 0.00000239*t3
	c1 := (0.1734-0.000393*t)*math.Sin(m*dr) + 0.0021*math.Sin(2*dr*m)
	c1 = c1 - 0.4068*math.Sin(mpr*dr) + 0.0161*math.Sin(dr*2*mpr)
	c1 -= 0.0004 * math.Sin(dr*3*mpr)
	c1 = c1 + 0.0104*math.Sin(dr*2*f) - 0.0051*math.Sin(dr*(m+mpr))
	c1 = c1 - 0.0074*math.Sin(dr*(m-mpr)) + 0.0004*math.Sin(dr*(2*f+m))
	c1 = c1 - 0.0004*math.Sin(dr*(2*f-m)) - 0.0006*math.Sin(dr*(2*f+mpr))
	c1 = c1 + 0.0010*math.Sin(dr*(2*f-mpr)) + 0.0005*math.Sin(dr*(2*mpr+m))
	var deltat float64
	if t < -11 {
		deltat = 0.001 + 0.000839*t + 0.0002261*t2 - 0.00000845*t3 - 0.000000081*t*t3
	} else {
		deltat = -0.000278 + 0.000265*t + 0.000262*t2
	}
	return jd1 + c1 - deltat
}

// sunLongitude: kinh độ mặt trời (radian, 0–2π) ở ngày Julius jdn (UT).
func sunLongitude(jdn float64) float64 {
	t := (jdn - 2451545.0) / 36525
	t2 := t * t
	dr := math.Pi / 180
	m := 357.52910 + 35999.05030*t - 0.0001559*t2 - 0.00000048*t*t2
	l0 := 280.46645 + 36000.76983*t + 0.0003032*t2
	dl := (1.914600 - 0.004817*t - 0.000014*t2) * math.Sin(dr*m)
	dl += (0.019993-0.000101*t)*math.Sin(dr*2*m) + 0.000290*math.Sin(dr*3*m)
	l := (l0 + dl) * dr
	return l - math.Pi*2*float64(floor(l/(math.Pi*2)))
}

// newMoonDay: ngày (số Julius) có sóc thứ k theo múi giờ tz.
func newMoonDay(k int, tz float64) int { return floor(newMoon(k) + 0.5 + tz/24) }

// sunSector: cung hoàng đạo (0–11, mỗi cung 30°) của mặt trời lúc đầu ngày dayNumber theo múi giờ tz.
func sunSector(dayNumber int, tz float64) int {
	return floor(sunLongitude(float64(dayNumber)-0.5-tz/24) / math.Pi * 6)
}

// lunarMonth11: ngày bắt đầu tháng 11 âm lịch (tháng chứa Đông chí) của năm yy.
func lunarMonth11(yy int, tz float64) int {
	off := jdFromDate(31, 12, yy) - 2415021
	k := floor(float64(off) / 29.530588853)
	nm := newMoonDay(k, tz)
	if sunSector(nm, tz) >= 9 {
		nm = newMoonDay(k-1, tz)
	}
	return nm
}

// leapMonthOffset: vị trí (tính từ tháng 11 bắt đầu ở a11) của tháng nhuận trong năm âm có 13 tháng.
func leapMonthOffset(a11 int, tz float64) int {
	k := floor((float64(a11)-2415021.076998695)/29.530588853 + 0.5)
	i := 1
	arc := sunSector(newMoonDay(k+i, tz), tz)
	for {
		last := arc
		i++
		arc = sunSector(newMoonDay(k+i, tz), tz)
		if arc == last || i >= 14 {
			break
		}
	}
	return i - 1
}

func convert(dd, mm, yy int, tz float64) Date {
	dayNumber := jdFromDate(dd, mm, yy)
	k := floor((float64(dayNumber) - 2415021.076998695) / 29.530588853)
	monthStart := newMoonDay(k+1, tz)
	if monthStart > dayNumber {
		monthStart = newMoonDay(k, tz)
	}
	a11 := lunarMonth11(yy, tz)
	b11 := a11
	var year int
	if a11 >= monthStart {
		year = yy
		a11 = lunarMonth11(yy-1, tz)
	} else {
		year = yy + 1
		b11 = lunarMonth11(yy+1, tz)
	}
	day := dayNumber - monthStart + 1
	diff := floor(float64(monthStart-a11) / 29)
	leap := false
	month := diff + 11
	if b11-a11 > 365 {
		leapDiff := leapMonthOffset(a11, tz)
		if diff >= leapDiff {
			month = diff + 10
			if diff == leapDiff {
				leap = true
			}
		}
	}
	if month > 12 {
		month -= 12
	}
	if month >= 11 && diff < 4 {
		year--
	}
	return Date{Day: day, Month: month, Year: year, Leap: leap}
}
