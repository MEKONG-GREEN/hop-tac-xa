package domain

// FN-01 (đăng nhập tên đăng nhập/SĐT + mật khẩu): chính sách đếm lần sai và câu thông báo. Hằng số khớp chính sách
// brute-force của realm (agrigreen-realm.json: MULTIPLE, failureFactor 5, maxDeltaTimeSeconds 86400, waitIncrement =
// maxFailureWait 900, quickLoginCheck 1000 ms, minimumQuickLoginWait 60 giây); keycloak/lockout_test.go giữ hai nơi khớp nhau.

import (
	"strings"
	"time"
)

// Chính sách khóa đăng nhập tạm (FN-01/BR-06, QTPQ-Q-03).
const (
	LoginMaxFailures   = 5
	LoginLockout       = 15 * time.Minute
	LoginQuickCheck    = time.Second
	LoginQuickWait     = time.Minute
	LoginFailureWindow = 24 * time.Hour
)

// Câu thông báo của FN-01 (nguyên văn BRD và trả lời của nhóm dự án 16/11/2026).
const (
	MsgLoginFailed        = "Sai tài khoản hoặc mật khẩu. Còn %d lần thử."
	MsgLoginTempLocked    = "Tài khoản tạm khóa đăng nhập do nhập sai nhiều lần. Thử lại sau %d phút hoặc dùng Quên mật khẩu?"
	MsgAccountLockedFN01  = "Tài khoản đã bị khóa. Liên hệ người đã cấp tài khoản để mở khóa."
	MsgIdentifierRequired = "Nhập tên đăng nhập hoặc số điện thoại"
	MsgPasswordRequired   = "Nhập mật khẩu" //nolint:gosec // câu thông báo trường bắt buộc, không phải bí mật
)

// Giới hạn độ dài đầu vào đăng nhập (byte).
const (
	MaxLoginIdentifierLen = 254
	MaxLoginPasswordLen   = 256
)

// NormalizeLoginIdentifier chuẩn hóa mã định danh người dùng nhập. phone khác rỗng khi chuỗi là SĐT hợp lệ (đã bỏ
// khoảng trắng/dấu chấm/gạch, +84 → 0); canonical là dạng dùng để băm bộ đếm: phone nếu có, ngược lại chữ thường đã cắt.
func NormalizeLoginIdentifier(s string) (canonical, phone string) {
	if p := NormalizePhone(s); ValidPhone(p) {
		return p, p
	}
	return strings.ToLower(strings.TrimSpace(s)), ""
}

// LoginProbe là bộ đếm lần sai của một mã định danh KHÔNG khớp tài khoản nào; mô phỏng brute-force MULTIPLE của
// Keycloak để phản hồi giống hệt tài khoản có thật.
type LoginProbe struct {
	Failures    int
	LastFailure time.Time
	LockedUntil time.Time
}

// Locked cho biết còn bị khóa tạm lúc now và thời gian còn lại.
func (p LoginProbe) Locked(now time.Time) (bool, time.Duration) {
	if p.LockedUntil.After(now) {
		return true, p.LockedUntil.Sub(now)
	}
	return false, 0
}

// Fail ghi một lần sai lúc now. Lần sai trước cách quá cửa sổ 24 giờ thì đếm lại từ 0; bội số của 5 khóa 15 phút;
// hai lần sai cách nhau dưới 1 giây khóa 60 giây.
func (p LoginProbe) Fail(now time.Time) LoginProbe {
	if !p.LastFailure.IsZero() && now.Sub(p.LastFailure) > LoginFailureWindow {
		p.Failures = 0
	}
	quick := !p.LastFailure.IsZero() && now.Sub(p.LastFailure) < LoginQuickCheck
	p.Failures++
	switch {
	case p.Failures%LoginMaxFailures == 0:
		p.LockedUntil = now.Add(LoginLockout)
	case quick:
		p.LockedUntil = now.Add(LoginQuickWait)
	}
	p.LastFailure = now
	return p
}

// AfterFailure tính kết quả của một lần sai trên tài khoản có thật từ số lần sai Keycloak ghi TRƯỚC lần kiểm này:
// remaining là số lần thử còn lại; lockFor > 0 khi lần sai này gây khóa tạm.
func AfterFailure(before int, last *time.Time, now time.Time) (remaining int, lockFor time.Duration) {
	if last != nil && now.Sub(*last) > LoginFailureWindow {
		before = 0
	}
	after := before + 1
	switch {
	case after%LoginMaxFailures == 0:
		return 0, LoginLockout
	case last != nil && now.Sub(*last) < LoginQuickCheck:
		return 0, LoginQuickWait
	}
	return LoginMaxFailures - after%LoginMaxFailures, 0
}

// CeilMinutes làm tròn d lên theo phút, tối thiểu 1.
func CeilMinutes(d time.Duration) int {
	m := int((d + time.Minute - 1) / time.Minute)
	return max(m, 1)
}
