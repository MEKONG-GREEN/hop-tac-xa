package handler

import (
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/weather/app"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/weather/domain"
)

// Đơn vị cố định (FN-TT-01/BR-04): °C (số nguyên), mm (1 chữ số thập phân), km/h (số nguyên), % (số nguyên); hướng gió
// viết tắt tiếng Việt.

type lunarDTO struct {
	Day      int    `json:"day"`
	Month    int    `json:"month"`
	Year     int    `json:"year"`
	Leap     bool   `json:"leap"`
	Label    string `json:"label"`     // dd/mm[ nhuận]
	FirstDay bool   `json:"first_day"` // mùng 1
	FullMoon bool   `json:"full_moon"` // rằm
}

type sourceDTO struct {
	Provider  string     `json:"provider"`
	Label     string     `json:"label"`
	UpdatedAt *time.Time `json:"updated_at"`
	Stale     bool       `json:"stale"`
}

type readingDTO struct {
	Time              time.Time `json:"time"`
	TemperatureC      *int      `json:"temperature_c"`
	Condition         string    `json:"condition"`
	WeatherCode       *int      `json:"weather_code"`
	PrecipitationMM   *float64  `json:"precipitation_mm"`
	PrecipitationProb *int      `json:"precipitation_probability"`
	WindSpeedKmh      *int      `json:"wind_speed_kmh"`
	WindDirection     string    `json:"wind_direction"`
	HumidityPct       *int      `json:"humidity_pct"`
}

type summaryDTO struct {
	Condition            string   `json:"condition"`
	WeatherCode          *int     `json:"weather_code"`
	TemperatureMinC      *int     `json:"temperature_min_c"`
	TemperatureMaxC      *int     `json:"temperature_max_c"`
	PrecipitationMM      *float64 `json:"precipitation_mm"`
	PrecipitationProbMax *int     `json:"precipitation_probability_max"`
	WindSpeedMaxKmh      *int     `json:"wind_speed_max_kmh"`
	WindDirection        string   `json:"wind_direction"`
	HumidityPct          *int     `json:"humidity_pct"`
}

type periodDTO struct {
	Name                 string   `json:"name"`
	Condition            string   `json:"condition"`
	WeatherCode          *int     `json:"weather_code"`
	TemperatureC         *int     `json:"temperature_c"`
	PrecipitationMM      *float64 `json:"precipitation_mm"`
	PrecipitationProbMax *int     `json:"precipitation_probability_max"`
	WindSpeedMaxKmh      *int     `json:"wind_speed_max_kmh"`
}

type todayDTO struct {
	CooperativeRef uuid.UUID   `json:"cooperative_ref"`
	Date           string      `json:"date"`
	Lunar          lunarDTO    `json:"lunar"`
	Status         string      `json:"status"`
	Message        string      `json:"message,omitempty"`
	Current        *readingDTO `json:"current"`
	Today          *summaryDTO `json:"today"`
	Source         sourceDTO   `json:"source"`
}

type weatherDayDTO struct {
	Kind            string `json:"kind"`  // forecast | observed | final_forecast
	Label           string `json:"label"` // Thực đo | Dự báo cuối ngày | ""
	HourlyAvailable bool   `json:"hourly_available"`
	summaryDTO
	Periods []periodDTO `json:"periods"`
}

type calendarDayDTO struct {
	Date    string         `json:"date"`
	Lunar   lunarDTO       `json:"lunar"`
	Weather *weatherDayDTO `json:"weather"`
}

type calendarDTO struct {
	CooperativeRef uuid.UUID        `json:"cooperative_ref"`
	Month          string           `json:"month"`
	Status         string           `json:"status"`
	Message        string           `json:"message,omitempty"`
	ForecastDays   int              `json:"forecast_days"`
	Days           []calendarDayDTO `json:"days"`
	Source         sourceDTO        `json:"source"`
}

type providerDTO struct {
	Code  string `json:"code"`
	Label string `json:"label"`
}

type configDTO struct {
	Configured      bool          `json:"configured"`
	Provider        string        `json:"provider"`
	Providers       []providerDTO `json:"providers"`
	RefreshMinutes  int           `json:"refresh_minutes"`
	RefreshOptions  []int         `json:"refresh_options"`
	ForecastDays    int           `json:"forecast_days"`
	MaxForecastDays int           `json:"max_forecast_days"`
	APIKeySet       bool          `json:"api_key_set"`
	APIKeyMasked    string        `json:"api_key_masked"` // ••••1234 — không bao giờ trả khóa đầy đủ (AC-03)
	Version         int64         `json:"version"`
	UpdatedAt       *time.Time    `json:"updated_at"`
}

type testDTO struct {
	OK           bool   `json:"ok"`
	Provider     string `json:"provider"`
	TemperatureC *int   `json:"temperature_c"`
	Condition    string `json:"condition"`
	Days         int    `json:"days"`
}

func roundInt(v *float64) *int {
	if v == nil {
		return nil
	}
	x := domain.RoundInt(*v)
	return &x
}

func round1(v *float64) *float64 {
	if v == nil {
		return nil
	}
	x := domain.Round1(*v)
	return &x
}

func condition(code *int) string {
	if code == nil {
		return ""
	}
	return domain.Condition(*code)
}

func windDir(deg *float64) string {
	if deg == nil {
		return ""
	}
	return domain.WindDirection(*deg)
}

func toReading(r domain.Reading) *readingDTO {
	return &readingDTO{Time: r.Time, TemperatureC: roundInt(r.TemperatureC), Condition: condition(r.WeatherCode), WeatherCode: r.WeatherCode,
		PrecipitationMM: round1(r.PrecipitationMM), PrecipitationProb: roundInt(r.PrecipitationProb), WindSpeedKmh: roundInt(r.WindSpeedKmh),
		WindDirection: windDir(r.WindDirDeg), HumidityPct: roundInt(r.HumidityPct)}
}

func toSummary(d domain.DaySummary) *summaryDTO {
	return &summaryDTO{Condition: condition(d.WeatherCode), WeatherCode: d.WeatherCode, TemperatureMinC: roundInt(d.TempMinC),
		TemperatureMaxC: roundInt(d.TempMaxC), PrecipitationMM: round1(d.PrecipitationMM), PrecipitationProbMax: roundInt(d.PrecipitationProbMax),
		WindSpeedMaxKmh: roundInt(d.WindMaxKmh), WindDirection: windDir(d.WindDirDeg), HumidityPct: roundInt(d.HumidityAvg)}
}

func toPeriod(p domain.Period) periodDTO {
	return periodDTO{Name: p.Name, Condition: condition(p.WeatherCode), WeatherCode: p.WeatherCode, TemperatureC: p.TemperatureC,
		PrecipitationMM: p.PrecipitationMM, PrecipitationProbMax: p.PrecipitationProbMax, WindSpeedMaxKmh: p.WindMaxKmh}
}

func toConfig(v app.ConfigView) configDTO {
	out := configDTO{Configured: v.Configured, Provider: v.Config.Provider, RefreshMinutes: v.Config.RefreshMinutes,
		RefreshOptions: domain.RefreshOptions, ForecastDays: v.Config.ForecastDays, MaxForecastDays: domain.MaxForecastDays,
		APIKeySet: v.Config.APIKeyLast4 != "", APIKeyMasked: domain.MaskedKey(v.Config.APIKeyLast4), Version: v.Config.Version}
	for _, p := range v.Providers {
		out.Providers = append(out.Providers, providerDTO{Code: p, Label: domain.ProviderLabel(p)})
	}
	if v.Configured {
		t := v.Config.UpdatedAt
		out.UpdatedAt = &t
	}
	return out
}
