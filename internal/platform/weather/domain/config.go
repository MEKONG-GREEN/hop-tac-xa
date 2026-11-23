// Package domain là quy tắc nghiệp vụ thuần của pf/weather (FN-TT-01..03, TECH-TP-02/03, DATA-TP-06; ADR-0065):
// cấu hình kết nối, lưới điểm lấy số liệu, chuẩn hóa và tổng hợp số liệu, trạng thái lấy (lỗi liên tiếp, giãn tần suất).
// Không I/O.
package domain

import (
	"errors"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// Nhà cung cấp được hỗ trợ (FN-TT-03: "danh sách nhà cung cấp được hỗ trợ"). fake chỉ dùng ngoài production.
const (
	ProviderOpenMeteo = "open_meteo"
	ProviderFake      = "fake"
)

// ProviderLabel là nhãn nguồn hiển thị kèm số liệu (LEGAL-TP-02: ghi nguồn khi điều khoản yêu cầu; Open-Meteo yêu cầu
// ghi công theo CC BY 4.0).
func ProviderLabel(p string) string {
	switch p {
	case ProviderOpenMeteo:
		return "Open-Meteo.com"
	case ProviderFake:
		return "Nguồn thử nghiệm"
	}
	return p
}

// RefreshOptions là tần suất lấy số liệu cho phép (phút); mặc định 60 (FN-TT-01/BR-01).
var RefreshOptions = []int{30, 60, 120, 180, 360}

// Giá trị mặc định và giới hạn.
const (
	DefaultRefreshMinutes = 60
	DefaultForecastDays   = 7
	MaxForecastDays       = 14 // FN-TT-02/BR-02
	MaxAPIKeyLen          = 200
	// QuotaRefresh: vượt hạn mức gọi → giãn tần suất về 3 giờ/lần đến 00:00 hôm sau (FN-TT-03/EX-03).
	QuotaRefresh = 3 * time.Hour
)

// Config là cấu hình kết nối có hiệu lực của tenant. APIKey không bao giờ nằm ở đây (chỉ 4 ký tự cuối).
type Config struct {
	Provider       string
	RefreshMinutes int
	ForecastDays   int
	APIKeyLast4    string // rỗng: không có khóa
	Version        int64
	UpdatedAt      time.Time
}

// ConfigInput là dữ liệu Admin gửi khi lưu hoặc kiểm tra kết nối. APIKey: nil = giữ khóa đang có; "" = bỏ khóa.
type ConfigInput struct {
	Provider       string  `json:"provider"`
	RefreshMinutes int     `json:"refresh_minutes"`
	ForecastDays   int     `json:"forecast_days"`
	APIKey         *string `json:"api_key" pii:"true"` // bí mật — không log, không trả về
}

// FieldError là lỗi tại một trường.
type FieldError struct{ Field, Msg string }

func (e FieldError) Error() string { return e.Field + ": " + e.Msg }

// Câu thông báo tại trường.
const (
	MsgProvider     = "Chọn nhà cung cấp được hỗ trợ"
	MsgRefresh      = "Tần suất cập nhật không hợp lệ"
	MsgForecastDays = "Số ngày dự báo từ 1 đến 14"
	MsgAPIKey       = "Khóa API không hợp lệ"
)

// Normalize kiểm và chuẩn hóa dữ liệu cấu hình. allowFake: chỉ ngoài production.
func (in ConfigInput) Normalize(allowFake bool) (ConfigInput, error) {
	in.Provider = strings.TrimSpace(in.Provider)
	switch in.Provider {
	case ProviderOpenMeteo:
	case ProviderFake:
		if !allowFake {
			return in, FieldError{"provider", MsgProvider}
		}
	default:
		return in, FieldError{"provider", MsgProvider}
	}
	if !slices.Contains(RefreshOptions, in.RefreshMinutes) {
		return in, FieldError{"refresh_minutes", MsgRefresh}
	}
	if in.ForecastDays < 1 || in.ForecastDays > MaxForecastDays {
		return in, FieldError{"forecast_days", MsgForecastDays}
	}
	if in.APIKey != nil {
		k := strings.TrimSpace(*in.APIKey)
		if utf8.RuneCountInString(k) > MaxAPIKeyLen || strings.ContainsAny(k, " \t\r\n") {
			return in, FieldError{"api_key", MsgAPIKey}
		}
		in.APIKey = &k
	}
	return in, nil
}

// Last4 trả 4 ký tự cuối của khóa để hiển thị (BR-03); khóa ngắn hơn 8 ký tự chỉ lộ 1 ký tự cuối, để phần lộ không quá
// nửa khóa.
func Last4(key string) string {
	r := []rune(key)
	switch {
	case len(r) == 0:
		return ""
	case len(r) < 8:
		return string(r[len(r)-1:])
	}
	return string(r[len(r)-4:])
}

// MaskedKey là dạng hiển thị của khóa: "••••" + 4 ký tự cuối (AC-03).
func MaskedKey(last4 string) string {
	if last4 == "" {
		return ""
	}
	return "••••" + last4
}

// ErrNotConfigured: tenant chưa có cấu hình kết nối — không gọi nhà cung cấp (fail-closed).
var ErrNotConfigured = errors.New("weather: chưa cấu hình kết nối thời tiết")
