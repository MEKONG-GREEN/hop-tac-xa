// Package openmeteo là bộ chuyển đổi nhà cung cấp Open-Meteo (https://open-meteo.com, ADR-0065) cho pf/weather.
//
// Chỉ gửi tọa độ đã làm thô theo lưới (domain.Snap) và các tham số số liệu; không token người dùng, không cookie, không
// mã HTX hay thiết bị; không theo chuyển hướng. Không khóa API → endpoint miễn phí (api.open-meteo.com, điều khoản phi
// thương mại); có khóa → endpoint thương mại (customer-api.open-meteo.com) kèm apikey. Số liệu quá khứ của Open-Meteo là
// mô hình, không phải thực đo, nên Forecast.Observed = false (FN-TT-03/BR-05 dùng "Dự báo cuối ngày").
package openmeteo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/weather/domain"
)

// Endpoint mặc định.
const (
	FreeURL     = "https://api.open-meteo.com"
	CustomerURL = "https://customer-api.open-meteo.com"
	maxBody     = 4 << 20
	timeZone    = "Asia/Ho_Chi_Minh"
)

const (
	hourlyVars  = "temperature_2m,relative_humidity_2m,precipitation,precipitation_probability,weather_code,wind_speed_10m,wind_direction_10m"
	dailyVars   = "weather_code,temperature_2m_max,temperature_2m_min,precipitation_sum,precipitation_probability_max,wind_speed_10m_max,wind_direction_10m_dominant"
	currentVars = "temperature_2m,relative_humidity_2m,precipitation,weather_code,wind_speed_10m,wind_direction_10m"
)

// Client gọi API dự báo của Open-Meteo.
type Client struct {
	// BaseURL: rỗng → FreeURL hoặc CustomerURL theo APIKey. Ghi đè (AG_WEATHER_OPEN_METEO_URL) cho môi trường thử.
	BaseURL string
	APIKey  string `pii:"true"`
	HTTP    *http.Client
}

func (c Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func (c Client) base() string {
	switch {
	case c.BaseURL != "":
		return strings.TrimRight(c.BaseURL, "/")
	case c.APIKey != "":
		return CustomerURL
	}
	return FreeURL
}

// Fetch lấy dự báo days ngày (1–14) cho điểm p (đã làm thô).
func (c Client) Fetch(ctx context.Context, p domain.Point, days int) (domain.Forecast, error) {
	q := url.Values{}
	q.Set("latitude", p.Lat.String())
	q.Set("longitude", p.Lon.String())
	q.Set("hourly", hourlyVars)
	q.Set("daily", dailyVars)
	q.Set("current", currentVars)
	q.Set("timezone", timeZone)
	q.Set("forecast_days", fmt.Sprint(days))
	q.Set("wind_speed_unit", "kmh")
	q.Set("timeformat", "unixtime")
	if c.APIKey != "" {
		q.Set("apikey", c.APIKey)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base()+"/v1/forecast?"+q.Encode(), nil)
	if err != nil {
		return domain.Forecast{}, domain.ProviderError{Code: domain.ErrCodeUnavailable}
	}
	req.Header.Set("User-Agent", "agrigreen-weather/1")
	req.Header.Set("Accept", "application/json")
	res, err := c.httpClient().Do(req)
	if err != nil {
		// Mạng, DNS, hết thời gian: không đưa thông điệp lỗi (có thể chứa URL kèm apikey) ra ngoài.
		return domain.Forecast{}, domain.ProviderError{Code: domain.ErrCodeUnavailable}
	}
	defer func() { _ = res.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(res.Body, maxBody))
	if err != nil {
		return domain.Forecast{}, domain.ProviderError{Code: domain.ErrCodeUnavailable}
	}
	if res.StatusCode != http.StatusOK {
		return domain.Forecast{}, classify(res.StatusCode, body)
	}
	return parse(body)
}

// classify phân loại lỗi HTTP. Open-Meteo trả {"error": true, "reason": "..."}; vượt hạn mức là 429 hoặc lý do có
// "limit exceeded".
func classify(status int, body []byte) error {
	var e struct {
		Reason string `json:"reason"`
	}
	_ = json.Unmarshal(body, &e)
	reason := strings.ToLower(e.Reason)
	switch {
	case status == http.StatusTooManyRequests || strings.Contains(reason, "limit exceeded"):
		return domain.ProviderError{Code: domain.ErrCodeQuota}
	case status == http.StatusUnauthorized || status == http.StatusForbidden || strings.Contains(reason, "api key"):
		return domain.ProviderError{Code: domain.ErrCodeAuth}
	case status >= 500:
		return domain.ProviderError{Code: domain.ErrCodeUnavailable}
	}
	return domain.ProviderError{Code: domain.ErrCodeBadResponse}
}

type series struct {
	Time          []int64    `json:"time"`
	Temp          []*float64 `json:"temperature_2m"`
	Humidity      []*float64 `json:"relative_humidity_2m"`
	Precip        []*float64 `json:"precipitation"`
	PrecipProb    []*float64 `json:"precipitation_probability"`
	Code          []*float64 `json:"weather_code"`
	Wind          []*float64 `json:"wind_speed_10m"`
	WindDir       []*float64 `json:"wind_direction_10m"`
	TempMax       []*float64 `json:"temperature_2m_max"`
	TempMin       []*float64 `json:"temperature_2m_min"`
	PrecipSum     []*float64 `json:"precipitation_sum"`
	PrecipProbMax []*float64 `json:"precipitation_probability_max"`
	WindMax       []*float64 `json:"wind_speed_10m_max"`
	WindDirDom    []*float64 `json:"wind_direction_10m_dominant"`
}

type current struct {
	Time     int64    `json:"time"`
	Temp     *float64 `json:"temperature_2m"`
	Humidity *float64 `json:"relative_humidity_2m"`
	Precip   *float64 `json:"precipitation"`
	Code     *float64 `json:"weather_code"`
	Wind     *float64 `json:"wind_speed_10m"`
	WindDir  *float64 `json:"wind_direction_10m"`
}

func at(s []*float64, i int) *float64 {
	if i < len(s) {
		return s[i]
	}
	return nil
}

func code(v *float64) *int {
	if v == nil {
		return nil
	}
	c := int(*v)
	return &c
}

func parse(body []byte) (domain.Forecast, error) {
	var r struct {
		Current *current `json:"current"`
		Hourly  *series  `json:"hourly"`
		Daily   *series  `json:"daily"`
	}
	if err := json.Unmarshal(body, &r); err != nil || r.Daily == nil || len(r.Daily.Time) == 0 {
		return domain.Forecast{}, domain.ProviderError{Code: domain.ErrCodeBadResponse}
	}
	f := domain.Forecast{Provider: domain.ProviderOpenMeteo}
	if c := r.Current; c != nil && c.Time > 0 {
		f.Current = &domain.Reading{Time: time.Unix(c.Time, 0).UTC(), TemperatureC: c.Temp, HumidityPct: c.Humidity,
			PrecipitationMM: c.Precip, WindSpeedKmh: c.Wind, WindDirDeg: c.WindDir, WeatherCode: code(c.Code)}
	}
	if h := r.Hourly; h != nil {
		for i, t := range h.Time {
			f.Hourly = append(f.Hourly, domain.Reading{Time: time.Unix(t, 0).UTC(), TemperatureC: at(h.Temp, i),
				HumidityPct: at(h.Humidity, i), PrecipitationMM: at(h.Precip, i), PrecipitationProb: at(h.PrecipProb, i),
				WindSpeedKmh: at(h.Wind, i), WindDirDeg: at(h.WindDir, i), WeatherCode: code(at(h.Code, i))})
		}
	}
	d := r.Daily
	for i, t := range d.Time {
		day := domain.DaySummary{Day: domain.DayOf(time.Unix(t, 0)), TempMinC: at(d.TempMin, i), TempMaxC: at(d.TempMax, i),
			PrecipitationMM: at(d.PrecipSum, i), PrecipitationProbMax: at(d.PrecipProbMax, i), WindMaxKmh: at(d.WindMax, i),
			WindDirDeg: at(d.WindDirDom, i), WeatherCode: code(at(d.Code, i))}
		day.HumidityAvg = avgHumidity(f.HourlyOn(day.Day))
		f.Daily = append(f.Daily, day)
	}
	return f, nil
}

func avgHumidity(hours []domain.Reading) *float64 {
	var sum float64
	var n int
	for _, h := range hours {
		if h.HumidityPct != nil {
			sum += *h.HumidityPct
			n++
		}
	}
	if n == 0 {
		return nil
	}
	v := sum / float64(n)
	return &v
}
