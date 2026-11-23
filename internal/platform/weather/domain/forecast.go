package domain

import (
	"math"
	"sort"
	"time"
)

// VN là múi giờ Việt Nam (GMT+7, không giờ mùa hè) — ngày, buổi và mốc 23:30 đều theo múi này.
var VN = time.FixedZone("ICT", 7*3600)

// DayOf trả ngày (YYYY-MM-DD) theo giờ Việt Nam.
func DayOf(t time.Time) string { return t.In(VN).Format(time.DateOnly) }

// Reading là số liệu tại một thời điểm (hiện tại hoặc một giờ). Đơn vị cố định (FN-TT-01/BR-04): °C, mm, km/h, %, độ.
// nil: nhà cung cấp không có giá trị.
type Reading struct {
	Time              time.Time `json:"time"`
	TemperatureC      *float64  `json:"temperature_c,omitempty"`
	HumidityPct       *float64  `json:"humidity_pct,omitempty"`
	PrecipitationMM   *float64  `json:"precipitation_mm,omitempty"`
	PrecipitationProb *float64  `json:"precipitation_probability,omitempty"`
	WindSpeedKmh      *float64  `json:"wind_speed_kmh,omitempty"`
	WindDirDeg        *float64  `json:"wind_direction_deg,omitempty"`
	WeatherCode       *int      `json:"weather_code,omitempty"`
}

// DaySummary là số liệu cả ngày.
type DaySummary struct {
	Day                  string   `json:"day"` // YYYY-MM-DD giờ Việt Nam
	TempMinC             *float64 `json:"temperature_min_c,omitempty"`
	TempMaxC             *float64 `json:"temperature_max_c,omitempty"`
	PrecipitationMM      *float64 `json:"precipitation_mm,omitempty"`
	PrecipitationProbMax *float64 `json:"precipitation_probability_max,omitempty"`
	WindMaxKmh           *float64 `json:"wind_speed_max_kmh,omitempty"`
	WindDirDeg           *float64 `json:"wind_direction_deg,omitempty"`
	HumidityAvg          *float64 `json:"humidity_pct,omitempty"`
	WeatherCode          *int     `json:"weather_code,omitempty"`
}

// Forecast là số liệu đã chuẩn hóa của một lần lấy, độc lập với nhà cung cấp.
type Forecast struct {
	Provider string `json:"provider"`
	// Observed: nhà cung cấp có số liệu THỰC ĐO cho ngày đã qua (FN-TT-03/BR-05). Open-Meteo không có (số liệu quá
	// khứ của nó là mô hình), nên ngày đã qua dùng "Dự báo cuối ngày".
	Observed bool         `json:"observed"`
	Current  *Reading     `json:"current,omitempty"`
	Hourly   []Reading    `json:"hourly,omitempty"`
	Daily    []DaySummary `json:"daily"`
}

// HourlyOn trả số liệu theo giờ của một ngày (giờ Việt Nam), theo thời gian tăng dần.
func (f Forecast) HourlyOn(day string) []Reading {
	var out []Reading
	for _, h := range f.Hourly {
		if DayOf(h.Time) == day {
			out = append(out, h)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Time.Before(out[j].Time) })
	return out
}

// DayOn trả số liệu cả ngày của một ngày; false nếu bản dự báo không có ngày đó (FN-TT-02/AC-03).
func (f Forecast) DayOn(day string) (DaySummary, bool) {
	for _, d := range f.Daily {
		if d.Day == day {
			return d, true
		}
	}
	return DaySummary{}, false
}

// Now trả số liệu hiện tại: Current nếu có, không thì giờ gần nhất không muộn hơn now.
func (f Forecast) Now(now time.Time) *Reading {
	if f.Current != nil {
		return f.Current
	}
	var best *Reading
	for i := range f.Hourly {
		h := &f.Hourly[i]
		if !h.Time.After(now) && (best == nil || h.Time.After(best.Time)) {
			best = h
		}
	}
	return best
}

// Buổi trong ngày, mỗi buổi 6 giờ (FN-TT-02/BR-03, AC-04).
const (
	PeriodNight     = "Đêm"   // 00–06
	PeriodMorning   = "Sáng"  // 06–12
	PeriodAfternoon = "Chiều" // 12–18
	PeriodEvening   = "Tối"   // 18–24
)

// Period là số liệu tổng hợp của một buổi từ số liệu theo giờ (FN-TT-02/BR-03): nhiệt độ trung bình làm tròn số
// nguyên, lượng mưa cộng dồn 1 chữ số thập phân, khả năng mưa lớn nhất, gió lớn nhất.
type Period struct {
	Name                 string   `json:"name"`
	TemperatureC         *int     `json:"temperature_c,omitempty"`
	PrecipitationMM      *float64 `json:"precipitation_mm,omitempty"`
	PrecipitationProbMax *int     `json:"precipitation_probability_max,omitempty"`
	WindMaxKmh           *int     `json:"wind_speed_max_kmh,omitempty"`
	WeatherCode          *int     `json:"weather_code,omitempty"`
}

// Periods tổng hợp số liệu theo giờ của MỘT ngày thành bốn buổi; buổi không có giờ nào thì bỏ.
func Periods(hours []Reading) []Period {
	names := []string{PeriodNight, PeriodMorning, PeriodAfternoon, PeriodEvening}
	buckets := make([][]Reading, 4)
	for _, h := range hours {
		i := h.Time.In(VN).Hour() / 6
		buckets[i] = append(buckets[i], h)
	}
	var out []Period
	for i, b := range buckets {
		if len(b) == 0 {
			continue
		}
		p := Period{Name: names[i]}
		var tSum float64
		var tN int
		var rain *float64
		var prob, wind *float64
		codes := map[int]int{}
		for _, h := range b {
			if h.TemperatureC != nil {
				tSum += *h.TemperatureC
				tN++
			}
			if h.PrecipitationMM != nil {
				v := *h.PrecipitationMM
				if rain != nil {
					v += *rain
				}
				rain = &v
			}
			prob = maxPtr(prob, h.PrecipitationProb)
			wind = maxPtr(wind, h.WindSpeedKmh)
			if h.WeatherCode != nil {
				codes[*h.WeatherCode]++
			}
		}
		if tN > 0 {
			v := RoundInt(tSum / float64(tN))
			p.TemperatureC = &v
		}
		if rain != nil {
			v := Round1(*rain)
			p.PrecipitationMM = &v
		}
		if prob != nil {
			v := RoundInt(*prob)
			p.PrecipitationProbMax = &v
		}
		if wind != nil {
			v := RoundInt(*wind)
			p.WindMaxKmh = &v
		}
		if c, ok := dominantCode(codes); ok {
			p.WeatherCode = &c
		}
		out = append(out, p)
	}
	return out
}

func maxPtr(cur, v *float64) *float64 {
	if v == nil {
		return cur
	}
	if cur == nil || *v > *cur {
		x := *v
		return &x
	}
	return cur
}

// dominantCode: mã thời tiết nặng nhất của buổi (mã WMO lớn hơn là hiện tượng mạnh hơn trong cùng nhóm).
func dominantCode(codes map[int]int) (int, bool) {
	best, ok := -1, false
	for c := range codes {
		if c > best {
			best, ok = c, true
		}
	}
	return best, ok
}

// RoundInt làm tròn nửa ra xa số 0 về số nguyên.
func RoundInt(v float64) int { return int(math.Round(v)) }

// Round1 làm tròn 1 chữ số thập phân (nửa ra xa số 0). Cộng 1e-9 trước để 0,25 tính bằng số thực vẫn lên 0,3.
func Round1(v float64) float64 {
	if v < 0 {
		return -Round1(-v)
	}
	return math.Round(v*10+1e-9) / 10
}

// WindDirection trả hướng gió viết tắt tiếng Việt (FN-TT-01/BR-04): B, ĐB, Đ, ĐN, N, TN, T, TB — 8 cung 45°, độ là
// hướng gió THỔI TỪ đó (chuẩn khí tượng).
func WindDirection(deg float64) string {
	dirs := []string{"B", "ĐB", "Đ", "ĐN", "N", "TN", "T", "TB"}
	d := math.Mod(deg, 360)
	if d < 0 {
		d += 360
	}
	return dirs[int(math.Floor((d+22.5)/45))%8]
}

// Condition trả tình trạng trời tiếng Việt của mã thời tiết WMO 4677 (mã Open-Meteo dùng). Chỉ mô tả, không lời khuyên
// hay cảnh báo (FN-TT-01/BR-02).
func Condition(code int) string {
	switch {
	case code == 0:
		return "Trời quang"
	case code == 1:
		return "Ít mây"
	case code == 2:
		return "Có mây"
	case code == 3:
		return "Nhiều mây"
	case code == 45 || code == 48:
		return "Sương mù"
	case code >= 51 && code <= 57:
		return "Mưa phùn"
	case code == 61 || code == 80:
		return "Mưa nhỏ"
	case code == 63 || code == 81:
		return "Mưa vừa"
	case code == 65 || code == 82:
		return "Mưa to"
	case code == 66 || code == 67:
		return "Mưa lạnh"
	case code >= 71 && code <= 77 || code == 85 || code == 86:
		return "Tuyết"
	case code == 95:
		return "Dông"
	case code == 96 || code == 99:
		return "Dông kèm mưa đá"
	}
	return "Không rõ"
}
