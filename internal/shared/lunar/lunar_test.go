package lunar

import (
	"testing"
	"time"
)

// Đối chiếu lịch vạn niên Việt Nam 2025–2028 (TECH-TP-02, FN-TT-01/AC-02, FN-TT-02/AC-02): ngày nêu trong BRD, mùng 1
// Tết, rằm, mùng 1 các tháng, và tháng nhuận (nhuận tháng 6 năm Ất Tỵ 2025, nhuận tháng 5 năm Mậu Thân 2028).
func TestFromSolar_LichVanNien(t *testing.T) {
	for _, tc := range []struct {
		y    int
		m    time.Month
		d    int
		want Date
	}{
		// Ví dụ trong BRD.
		{2026, time.November, 3, Date{Day: 25, Month: 9, Year: 2026}},
		{2026, time.November, 9, Date{Day: 1, Month: 10, Year: 2026}},
		{2026, time.November, 23, Date{Day: 15, Month: 10, Year: 2026}},
		// Tết Nguyên đán.
		{2025, time.January, 29, Date{Day: 1, Month: 1, Year: 2025}},
		{2026, time.February, 17, Date{Day: 1, Month: 1, Year: 2026}},
		{2027, time.February, 6, Date{Day: 1, Month: 1, Year: 2027}},
		{2028, time.January, 26, Date{Day: 1, Month: 1, Year: 2028}},
		{2029, time.February, 13, Date{Day: 1, Month: 1, Year: 2029}},
		// Ngày cuối năm âm (năm âm chưa đổi dù đã sang năm dương).
		{2026, time.February, 16, Date{Day: 29, Month: 12, Year: 2025}},
		{2027, time.January, 1, Date{Day: 24, Month: 11, Year: 2026}}, // tháng 11 bắt đầu 09/12/2026 (sóc)
		// Tháng nhuận 2025: 6 nhuận từ 25/07/2025 đến 22/08/2025.
		{2025, time.June, 25, Date{Day: 1, Month: 6, Year: 2025}},
		{2025, time.July, 24, Date{Day: 30, Month: 6, Year: 2025}},
		{2025, time.July, 25, Date{Day: 1, Month: 6, Year: 2025, Leap: true}},
		{2025, time.August, 22, Date{Day: 29, Month: 6, Year: 2025, Leap: true}},
		{2025, time.August, 23, Date{Day: 1, Month: 7, Year: 2025}},
		{2025, time.October, 6, Date{Day: 15, Month: 8, Year: 2025}}, // Tết Trung thu
		// Tháng nhuận 2028: 5 nhuận.
		{2028, time.May, 24, Date{Day: 1, Month: 5, Year: 2028}},
		{2028, time.June, 23, Date{Day: 1, Month: 5, Year: 2028, Leap: true}},
		{2028, time.July, 22, Date{Day: 1, Month: 6, Year: 2028}},
		// Các mốc khác.
		{2026, time.March, 3, Date{Day: 15, Month: 1, Year: 2026}}, // Rằm tháng Giêng
		{2027, time.September, 15, Date{Day: 15, Month: 8, Year: 2027}},
		{2028, time.December, 31, Date{Day: 16, Month: 11, Year: 2028}}, // tháng 11 bắt đầu 16/12/2028
	} {
		if got := FromSolar(tc.y, tc.m, tc.d); got != tc.want {
			t.Errorf("%04d-%02d-%02d: %+v, muốn %+v", tc.y, tc.m, tc.d, got, tc.want)
		}
	}
}

// Mọi ngày 2025–2028: ngày âm tăng đều 1, sang tháng mới ở ngày 29 hoặc 30, đúng một tháng nhuận ở 2025 và 2028.
func TestFromSolar_LienTuc(t *testing.T) {
	day := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	prev := FromSolar(day.Year(), day.Month(), day.Day())
	leapMonths := map[int]int{}
	for day = day.AddDate(0, 0, 1); day.Year() <= 2028; day = day.AddDate(0, 0, 1) {
		cur := FromSolar(day.Year(), day.Month(), day.Day())
		switch {
		case cur.Day == prev.Day+1 && cur.Month == prev.Month && cur.Leap == prev.Leap && cur.Year == prev.Year:
		case cur.Day == 1 && (prev.Day == 29 || prev.Day == 30):
			if cur.Leap {
				leapMonths[cur.Year] = cur.Month
				if cur.Month != prev.Month || prev.Leap {
					t.Fatalf("%s: tháng nhuận phải theo sau tháng thường cùng số: %+v sau %+v", day.Format("2006-01-02"), cur, prev)
				}
			}
		default:
			t.Fatalf("%s: %+v không nối tiếp %+v", day.Format("2006-01-02"), cur, prev)
		}
		prev = cur
	}
	if len(leapMonths) != 2 || leapMonths[2025] != 6 || leapMonths[2028] != 5 {
		t.Fatalf("tháng nhuận 2025–2028: %v", leapMonths)
	}
}

func TestOfDungGioVietNam(t *testing.T) {
	// 23:30 UTC ngày 08/11/2026 là 06:30 ngày 09/11/2026 giờ Việt Nam → mùng 1 tháng 10.
	if got := Of(time.Date(2026, 11, 8, 23, 30, 0, 0, time.UTC)); got != (Date{Day: 1, Month: 10, Year: 2026}) {
		t.Fatalf("%+v", got)
	}
	if s := (Date{Day: 1, Month: 6, Year: 2025, Leap: true}).String(); s != "01/06 nhuận" {
		t.Fatal(s)
	}
}
