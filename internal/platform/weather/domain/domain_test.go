package domain

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func f(v float64) *float64 { return &v }

// FN-TT-02/AC-04: buổi chiều có 6 giờ mưa 2; 4; 0; 6; 5; 5 mm → 22,0 mm; BR-03: nhiệt độ trung bình làm tròn số
// nguyên, khả năng mưa và gió lấy lớn nhất.
func TestPeriods_AC04(t *testing.T) {
	var hours []Reading
	rain := []float64{2, 4, 0, 6, 5, 5}
	temp := []float64{31, 32, 33, 32, 30, 29.6}
	for i := range rain {
		hours = append(hours, Reading{Time: time.Date(2026, 11, 23, 12+i, 0, 0, 0, VN), PrecipitationMM: f(rain[i]),
			TemperatureC: f(temp[i]), PrecipitationProb: f(float64(10 * i)), WindSpeedKmh: f(float64(5 + i))})
	}
	hours = append(hours, Reading{Time: time.Date(2026, 11, 23, 7, 0, 0, 0, VN), PrecipitationMM: f(0.1), TemperatureC: f(26)})
	ps := Periods(hours)
	if len(ps) != 2 || ps[0].Name != PeriodMorning || ps[1].Name != PeriodAfternoon {
		t.Fatalf("buổi: %+v", ps)
	}
	a := ps[1]
	if *a.PrecipitationMM != 22.0 || *a.TemperatureC != 31 || *a.PrecipitationProbMax != 50 || *a.WindMaxKmh != 10 {
		t.Fatalf("chiều: mưa %v nhiệt %v xác suất %v gió %v", *a.PrecipitationMM, *a.TemperatureC, *a.PrecipitationProbMax, *a.WindMaxKmh)
	}
}

func TestRound1VaWindDirection(t *testing.T) {
	if Round1(0.1+0.2) != 0.3 || Round1(0.25) != 0.3 || Round1(22) != 22 {
		t.Fatal("Round1")
	}
	for deg, want := range map[float64]string{0: "B", 44: "ĐB", 90: "Đ", 135: "ĐN", 180: "N", 225: "TN", 270: "T", 315: "TB", 350: "B", -45: "TB", 22.4: "B", 22.5: "ĐB"} {
		if got := WindDirection(deg); got != want {
			t.Errorf("%v°: %s, muốn %s", deg, got, want)
		}
	}
	if Condition(0) != "Trời quang" || Condition(95) != "Dông" || Condition(1234) != "Không rõ" {
		t.Fatal("Condition")
	}
}

// FN-TT-03/AC-01 (tần suất mới có hiệu lực ở lần lấy kế tiếp), AC-05 (lỗi 3 lần liên tiếp → đúng 1 thông báo),
// EX-03 (vượt hạn mức → 3 giờ/lần tới 00:00 hôm sau).
func TestFetchState(t *testing.T) {
	t0 := time.Date(2026, 11, 23, 9, 0, 0, 0, VN)
	var s FetchState
	if !s.Due(t0, time.Hour) {
		t.Fatal("chưa lấy lần nào thì đến lượt ngay")
	}
	s = s.Succeeded(t0)
	if s.Due(t0.Add(59*time.Minute), time.Hour) || !s.Due(t0.Add(time.Hour), time.Hour) || !s.Due(t0.Add(30*time.Minute), 30*time.Minute) {
		t.Fatal("tần suất")
	}
	notifications := 0
	for i := 1; i <= 5; i++ {
		var n bool
		s, n = s.Failed(t0.Add(time.Duration(i)*time.Hour), ErrCodeUnavailable)
		if n {
			notifications++
			if i != 3 {
				t.Fatalf("thông báo ở lần lỗi %d", i)
			}
		}
	}
	if notifications != 1 || s.Failures != 5 {
		t.Fatalf("thông báo %d, lỗi %d", notifications, s.Failures)
	}
	s = s.Succeeded(t0.Add(6 * time.Hour))
	if s.Failures != 0 || s.Notified {
		t.Fatal("thành công phải mở lại chuỗi lỗi")
	}
	quotaAt := time.Date(2026, 11, 23, 14, 10, 0, 0, VN)
	s, _ = s.Failed(quotaAt, ErrCodeQuota)
	if want := time.Date(2026, 11, 24, 0, 0, 0, 0, VN); !s.BackoffUntil.Equal(want) {
		t.Fatalf("giãn tới %v", s.BackoffUntil)
	}
	if s.Due(quotaAt.Add(time.Hour), time.Hour) || !s.Due(quotaAt.Add(3*time.Hour), time.Hour) {
		t.Fatal("đang giãn: 3 giờ/lần")
	}
	late := time.Date(2026, 11, 23, 23, 0, 0, 0, VN)
	s = s.Succeeded(late)
	if !s.Due(time.Date(2026, 11, 24, 0, 0, 0, 0, VN), time.Hour) {
		t.Fatal("qua 00:00 hôm sau trở lại tần suất cấu hình")
	}
}

func TestRecordsDay(t *testing.T) {
	if !RecordsDay(time.Date(2026, 11, 23, 23, 29, 0, 0, VN)) || RecordsDay(time.Date(2026, 11, 23, 23, 30, 0, 0, VN)) {
		t.Fatal("mốc 23:30")
	}
	if !RecordsDay(time.Date(2026, 11, 23, 16, 0, 0, 0, time.UTC)) { // 23:00 giờ Việt Nam
		t.Fatal("theo giờ Việt Nam")
	}
}

// BR-02: vị trí cách nhau dưới 1 km dùng chung một điểm; tọa độ làm thô 0,01°.
func TestGrid(t *testing.T) {
	d := func(s string) decimal.Decimal { return decimal.RequireFromString(s) }
	p := Snap(Point{Lat: d("10.034567"), Lon: d("105.781234")})
	if p.Lat.String() != "10.03" || p.Lon.String() != "105.78" {
		t.Fatalf("snap %v %v", p.Lat, p.Lon)
	}
	pts := []Point{{d("10.03"), d("105.78")}, {d("10.20"), d("105.90")}}
	if Nearest(Point{d("10.035"), d("105.785")}, pts) != 0 { // ≈ 0,8 km
		t.Fatal("gần dưới 1 km phải dùng chung")
	}
	if Nearest(Point{d("10.05"), d("105.78")}, pts) != -1 { // ≈ 2,2 km
		t.Fatal("xa hơn 1 km không dùng chung")
	}
}

func TestConfigNormalize(t *testing.T) {
	key := " abcdefgh1234 "
	in, err := ConfigInput{Provider: ProviderOpenMeteo, RefreshMinutes: 60, ForecastDays: 14, APIKey: &key}.Normalize(false)
	if err != nil || *in.APIKey != "abcdefgh1234" || Last4(*in.APIKey) != "1234" || MaskedKey("1234") != "••••1234" {
		t.Fatalf("%+v %v", in, err)
	}
	for _, bad := range []ConfigInput{
		{Provider: ProviderFake, RefreshMinutes: 60, ForecastDays: 7},
		{Provider: "openweather", RefreshMinutes: 60, ForecastDays: 7},
		{Provider: ProviderOpenMeteo, RefreshMinutes: 45, ForecastDays: 7},
		{Provider: ProviderOpenMeteo, RefreshMinutes: 60, ForecastDays: 15},
	} {
		if _, err := bad.Normalize(false); err == nil {
			t.Errorf("phải từ chối %+v", bad)
		}
	}
	if Last4("abc") != "c" {
		t.Fatal("khóa ngắn chỉ lộ 1 ký tự")
	}
}
