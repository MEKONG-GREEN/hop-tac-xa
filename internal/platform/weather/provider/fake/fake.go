// Package fake là nhà cung cấp thời tiết giả cho môi trường dev, demo và kiểm thử (cấm ở production — ADR-0065). Số liệu
// sinh tất định từ tọa độ và giờ, có số liệu theo giờ, không có thực đo.
package fake

import (
	"context"
	"math"
	"sync"
	"time"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/weather/domain"
)

// Provider là nhà cung cấp giả. Fail: mã lỗi trả cho mọi lần gọi (kiểm thử EX-01..03); rỗng = thành công.
type Provider struct {
	Now      func() time.Time
	Hourless bool // không có số liệu theo giờ (FN-TT-02/EX-01)

	mu    sync.Mutex
	Fail  string
	Calls int
}

// SetFail đặt mã lỗi cho các lần gọi sau (an toàn khi gọi song song).
func (p *Provider) SetFail(code string) { p.mu.Lock(); p.Fail = code; p.mu.Unlock() }

// CallCount trả số lần đã gọi.
func (p *Provider) CallCount() int { p.mu.Lock(); defer p.mu.Unlock(); return p.Calls }

// Fetch sinh dự báo days ngày từ đầu ngày hôm nay (giờ Việt Nam).
func (p *Provider) Fetch(_ context.Context, pt domain.Point, days int) (domain.Forecast, error) {
	p.mu.Lock()
	p.Calls++
	fail := p.Fail
	p.mu.Unlock()
	if fail != "" {
		return domain.Forecast{}, domain.ProviderError{Code: fail}
	}
	now := time.Now()
	if p.Now != nil {
		now = p.Now()
	}
	seed := pt.Lat.InexactFloat64()*7 + pt.Lon.InexactFloat64()*3
	vn := now.In(domain.VN)
	start := time.Date(vn.Year(), vn.Month(), vn.Day(), 0, 0, 0, 0, domain.VN)
	f := domain.Forecast{Provider: domain.ProviderFake}
	for d := 0; d < days; d++ {
		var hours []domain.Reading
		for h := 0; h < 24; h++ {
			t := start.Add(time.Duration(d*24+h) * time.Hour)
			x := seed + float64(d*24+h)
			temp := round1(27 + 4*math.Sin(float64(h-9)*math.Pi/12) + math.Sin(x)/2)
			rain := 0.0
			if h >= 13 && h <= 17 && int(x)%3 == 0 {
				rain = float64(int(x)%6) + 0.5
			}
			prob := float64(int(x*13) % 90)
			wind := round1(8 + 6*math.Abs(math.Sin(x/5)))
			dir := float64(int(x*37) % 360)
			hum := float64(65 + int(x)%30)
			code := 2
			if rain > 0 {
				code = 61
			}
			hours = append(hours, domain.Reading{Time: t.UTC(), TemperatureC: &temp, HumidityPct: &hum, PrecipitationMM: &rain,
				PrecipitationProb: &prob, WindSpeedKmh: &wind, WindDirDeg: &dir, WeatherCode: &code})
		}
		day := summarize(domain.DayOf(start.AddDate(0, 0, d)), hours)
		f.Daily = append(f.Daily, day)
		if !p.Hourless {
			f.Hourly = append(f.Hourly, hours...)
		}
	}
	if !p.Hourless {
		f.Current = f.Now(now)
	}
	return f, nil
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }

func summarize(day string, hours []domain.Reading) domain.DaySummary {
	s := domain.DaySummary{Day: day}
	var rain, hum float64
	for i, h := range hours {
		if i == 0 || *h.TemperatureC < *s.TempMinC {
			v := *h.TemperatureC
			s.TempMinC = &v
		}
		if i == 0 || *h.TemperatureC > *s.TempMaxC {
			v := *h.TemperatureC
			s.TempMaxC = &v
		}
		if i == 0 || *h.PrecipitationProb > *s.PrecipitationProbMax {
			v := *h.PrecipitationProb
			s.PrecipitationProbMax = &v
		}
		if i == 0 || *h.WindSpeedKmh > *s.WindMaxKmh {
			v, d := *h.WindSpeedKmh, *h.WindDirDeg
			s.WindMaxKmh, s.WindDirDeg = &v, &d
		}
		rain += *h.PrecipitationMM
		hum += *h.HumidityPct
	}
	rain = round1(rain)
	hum /= float64(len(hours))
	code := 2
	if rain > 0 {
		code = 61
	}
	s.PrecipitationMM, s.HumidityAvg, s.WeatherCode = &rain, &hum, &code
	return s
}
