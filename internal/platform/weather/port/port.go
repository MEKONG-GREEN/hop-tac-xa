// Package port là hợp đồng công khai của pf/weather (BRD Thời tiết & Thu gom phụ phẩm — FN-TT-01..03; ADR-0065).
// Module khác chỉ import gói này.
package port

// JobFetch là job định kỳ của worker lấy số liệu thời tiết theo tần suất cấu hình (FN-TT-03).
const JobFetch = "weather_fetch"

// Sự kiện module phát ra (schema trong contracts/events/).
const (
	// EventFetchFailing: gọi nhà cung cấp lỗi 3 lần liên tiếp (FN-TT-03/EX-02, NTF-TP-07) — pf/notify báo Admin trên
	// Web. Phát đúng một lần cho mỗi chuỗi lỗi; lần lấy thành công kế tiếp mở lại.
	EventFetchFailing = "pf.weather.fetch.failing"
	EventProducerName = "pf/weather"
)

// FailureThreshold là số lần lỗi liên tiếp phát EventFetchFailing (FN-TT-03/EX-02).
const FailureThreshold = 3

// FetchFailingV1 là payload của pf.weather.fetch.failing. Không có dữ liệu cá nhân, không khóa API, không tọa độ.
//
//ag:event pf.weather.fetch.failing v1
type FetchFailingV1 struct {
	Provider     string `json:"provider"`      // open_meteo | fake
	Failures     int    `json:"failures"`      // số lần lỗi liên tiếp lúc phát (= FailureThreshold)
	LastError    string `json:"last_error"`    // mã lỗi: unavailable | quota | auth | bad_response
	LastAttempt  string `json:"last_attempt"`  // RFC3339
	LastSuccess  string `json:"last_success"`  // RFC3339; rỗng nếu chưa từng thành công
	EffectiveDay string `json:"effective_day"` // ngày (giờ Việt Nam) của lần lỗi
}
