package domain

// ADR-0061: Admin đăng nhập hai bước ở màn riêng của App HTX — mật khẩu, rồi mã OTP 6 số gửi qua email. Chính sách mã
// (hạn, số lần sai, chờ gửi lại, trần mỗi giờ) và cách băm mã nằm ở đây; luồng ở app/login_otp.go.

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
	"math/big"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Chính sách mã OTP qua email (quyết định 17/11/2026).
const (
	// LoginOTPTTL: mã hết hạn sau 5 phút tính từ lúc gửi.
	LoginOTPTTL = 5 * time.Minute
	// LoginOTPMaxAttempts: sai 5 lần thì mã bị hủy, phải đăng nhập lại để nhận mã mới.
	LoginOTPMaxAttempts = 5
	// LoginOTPResendAfter: chỉ gửi mã mới sau 60 giây kể từ mã trước.
	LoginOTPResendAfter = time.Minute
	// LoginOTPPerHour: tối đa 10 mã mỗi giờ cho một tài khoản (chặn dội email khi lộ mật khẩu).
	LoginOTPPerHour = 10
	// LoginOTPWindow là cửa sổ đếm LoginOTPPerHour.
	LoginOTPWindow = time.Hour
	// LoginMFASessionTTL: phiên đã qua mã email được ghi nhận tối đa 24 giờ — bằng hạn tối đa của phiên Keycloak (D9).
	LoginMFASessionTTL = 24 * time.Hour
	// LoginOTPDigits là số chữ số của mã.
	LoginOTPDigits = 6
)

// Câu thông báo của đăng nhập bằng OTP email.
const (
	MsgLoginOTPInvalid      = "Mã OTP không đúng. Còn %d lần thử."
	MsgLoginOTPExpired      = "Mã OTP đã hết hạn hoặc đã bị hủy. Đăng nhập lại để nhận mã mới."
	MsgLoginOTPUnavailable  = "Chưa gửi được mã OTP qua email. Vui lòng thử lại sau hoặc liên hệ quản trị hệ thống."
	MsgLoginOTPEmailMissing = "Tài khoản chưa có email đã xác minh để nhận mã OTP. Liên hệ quản trị hệ thống."
)

const loginOTPContext = "pf/accounts login otp v1\x00"

// NewLoginOTPCode sinh mã LoginOTPDigits chữ số phân bố đều (crypto/rand; rnd nil → rand.Reader).
func NewLoginOTPCode(rnd io.Reader) (string, error) {
	if rnd == nil {
		rnd = rand.Reader
	}
	n, err := rand.Int(rnd, big.NewInt(1_000_000))
	if err != nil {
		return "", fmt.Errorf("accounts: không sinh được mã OTP: %w", err)
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

// LoginOTPHash băm mã bằng HMAC-SHA256 với khóa máy chủ, gắn với mã thử thách: DB lộ ra cũng không dò ngược được mã
// 6 số (không có khóa), và mã của thử thách này không dùng được cho thử thách khác.
func LoginOTPHash(key []byte, challenge uuid.UUID, code string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(loginOTPContext))
	mac.Write(challenge[:])
	mac.Write([]byte(code))
	return mac.Sum(nil)
}

// LoginOTPMatches so mã người dùng nhập với băm đã lưu trong thời gian hằng.
func LoginOTPMatches(key []byte, challenge uuid.UUID, code string, stored []byte) bool {
	return len(stored) == sha256.Size && hmac.Equal(LoginOTPHash(key, challenge, code), stored)
}

// MaskEmail che địa chỉ để hiện trên màn nhập mã: giữ ký tự đầu của phần tên và tên miền, vd "a***@mekonggreen.vn".
// Chuỗi không có '@' → "***".
func MaskEmail(addr string) string {
	name, host, ok := strings.Cut(strings.TrimSpace(addr), "@")
	if !ok || name == "" || host == "" {
		return "***"
	}
	r := []rune(name)
	return string(r[0]) + "***@" + host
}
