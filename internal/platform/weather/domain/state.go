package domain

import "time"

// Mã lỗi khi gọi nhà cung cấp (lưu ở fetch_state.last_error và trong sự kiện; không chứa phản hồi thô).
const (
	ErrCodeUnavailable = "unavailable"  // mạng, hết thời gian, 5xx
	ErrCodeQuota       = "quota"        // vượt hạn mức gọi (FN-TT-03/EX-03)
	ErrCodeAuth        = "auth"         // khóa API sai hoặc hết hạn
	ErrCodeBadResponse = "bad_response" // phản hồi sai định dạng
)

// ProviderError là lỗi của nhà cung cấp, đã phân loại.
type ProviderError struct{ Code string }

func (e ProviderError) Error() string { return "weather: nhà cung cấp lỗi " + e.Code }

// FailureThreshold là số lần lỗi liên tiếp gửi thông báo cho Admin (FN-TT-03/EX-02).
const FailureThreshold = 3

// FinalForecastCutoff: số liệu ngày đã qua dùng bản dự báo lấy lần cuối TRƯỚC 23:30 giờ Việt Nam (FN-TT-03/BR-05).
const FinalForecastCutoff = 23*time.Hour + 30*time.Minute

// RecordsDay cho biết lần lấy lúc t còn được ghi làm số liệu của ngày đó (trước 23:30 giờ Việt Nam).
func RecordsDay(t time.Time) bool {
	vn := t.In(VN)
	midnight := time.Date(vn.Year(), vn.Month(), vn.Day(), 0, 0, 0, 0, VN)
	return vn.Sub(midnight) < FinalForecastCutoff
}

// FetchState là trạng thái lấy số liệu của một tenant.
type FetchState struct {
	LastAttempt  *time.Time
	LastSuccess  *time.Time
	Failures     int
	LastError    string
	BackoffUntil *time.Time
	Notified     bool // đã gửi thông báo cho chuỗi lỗi hiện tại
}

// Interval là khoảng cách giữa hai lần lấy lúc now: tần suất cấu hình, hoặc 3 giờ khi đang giãn vì vượt hạn mức.
func (s FetchState) Interval(now time.Time, refresh time.Duration) time.Duration {
	if s.BackoffUntil != nil && now.Before(*s.BackoffUntil) && refresh < QuotaRefresh {
		return QuotaRefresh
	}
	return refresh
}

// Due cho biết đã đến lượt lấy. Tần suất đọc từ cấu hình hiện hành mỗi lần kiểm nên đổi tần suất có hiệu lực ở lần
// lấy kế tiếp (FN-TT-03/AC-01).
func (s FetchState) Due(now time.Time, refresh time.Duration) bool {
	return s.LastAttempt == nil || !now.Before(s.LastAttempt.Add(s.Interval(now, refresh)))
}

// Succeeded trả trạng thái sau một lần lấy thành công: xóa chuỗi lỗi (lần lỗi sau lại đếm từ 1 và có thể báo lại).
// Giãn tần suất do vượt hạn mức vẫn giữ tới 00:00 hôm sau.
func (s FetchState) Succeeded(now time.Time) FetchState {
	s.LastAttempt, s.LastSuccess = &now, &now
	s.Failures, s.LastError, s.Notified = 0, "", false
	return s
}

// Failed trả trạng thái sau một lần lỗi và cho biết phải phát thông báo (đúng một lần khi chạm ngưỡng — AC-05).
// Vượt hạn mức → giãn tần suất tới 00:00 hôm sau giờ Việt Nam (EX-03).
func (s FetchState) Failed(now time.Time, code string) (FetchState, bool) {
	s.LastAttempt = &now
	s.Failures++
	s.LastError = code
	if code == ErrCodeQuota {
		vn := now.In(VN)
		until := time.Date(vn.Year(), vn.Month(), vn.Day()+1, 0, 0, 0, 0, VN)
		s.BackoffUntil = &until
	}
	notify := s.Failures >= FailureThreshold && !s.Notified
	if notify {
		s.Notified = true
	}
	return s, notify
}
