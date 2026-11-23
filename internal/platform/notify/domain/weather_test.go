package domain

import (
	"strings"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/notify/port"
	weatherport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/weather/port"
)

// FN-TT-03/EX-02, NTF-TP-07: API thời tiết lỗi 3 lần liên tiếp → thông báo khẩn cho Admin, dẫn tới màn Kết nối thời tiết.
func TestWeatherFetchFailingTemplate(t *testing.T) {
	at := time.Date(2026, 11, 23, 3, 0, 0, 0, time.UTC)
	tp := WeatherFetchFailingTemplate(weatherport.FetchFailingV1{Provider: "open_meteo", Failures: 3, LastError: "quota",
		LastAttempt: at.Format(time.RFC3339), EffectiveDay: "2026-11-23"}, time.Time{})
	if tp.Kind != port.KindWeatherFetchFailing || tp.Severity != port.SeverityUrgent || tp.DeepLink != "/quan-tri/ket-noi-thoi-tiet" ||
		!strings.Contains(tp.Body, "lỗi 3 lần liên tiếp") || !strings.Contains(tp.Body, "vượt hạn mức") || !tp.CreatedAt.Equal(at) {
		t.Fatalf("%+v", tp)
	}
}
