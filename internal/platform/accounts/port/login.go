package port

import (
	"context"
	"errors"
)

// LoginClient chọn client Keycloak cho lần kiểm mật khẩu (FN-01, ADR-0060).
type LoginClient int

const (
	// LoginMain: client phát token thật; luồng direct grant bắt OTP khi tài khoản đã cấu hình.
	LoginMain LoginClient = iota
	// LoginCheck: client chỉ kiểm mật khẩu (không OTP). Token của nó không có aud/vai nên API không nhận; máy chủ
	// thu hồi phiên ngay bằng EndSession.
	LoginCheck
)

// TokenSet là token Keycloak trả cho lần đăng nhập/làm mới. Token không bao giờ vào log (tag pii).
type TokenSet struct {
	AccessToken      string `pii:"true"`
	RefreshToken     string `pii:"true"`
	ExpiresIn        int
	RefreshExpiresIn int
	// Roles, AMR đọc từ claim realm_access.roles và amr của access token (token đến thẳng từ Keycloak).
	Roles []string
	AMR   []string
}

// Lỗi của PasswordLogin. Lỗi hạ tầng dùng lại ErrUnavailable.
var (
	// ErrInvalidGrant: sai mật khẩu, sai/thiếu OTP hoặc đang bị khóa tạm — Keycloak không phân biệt.
	ErrInvalidGrant = errors.New("accounts: Keycloak từ chối thông tin đăng nhập")
	// ErrAccountNotSetUp: còn required action (đổi mật khẩu tạm, đăng ký TOTP...), chỉ trả sau khi mật khẩu đúng.
	ErrAccountNotSetUp = errors.New("accounts: tài khoản chưa hoàn tất thiết lập lần đầu")
	ErrAccountDisabled = errors.New("accounts: tài khoản bị khóa trong Keycloak")
	ErrSessionEnded    = errors.New("accounts: phiên Keycloak đã hết")
)

// PasswordLogin là cổng direct grant của Keycloak cho màn đăng nhập riêng của App HTX (FN-01). Máy chủ chỉ chuyển
// tiếp mật khẩu cho Keycloak, không lưu, không băm. Mật khẩu và OTP chỉ nằm trong thân request, không vào lỗi hay log.
type PasswordLogin interface {
	// PasswordGrant kiểm mật khẩu (kèm otp nếu có, chỉ client LoginMain) và trả token.
	PasswordGrant(ctx context.Context, c LoginClient, username, password, otp string) (TokenSet, error)
	// Refresh làm mới token bằng client LoginMain; phiên đã hết → ErrSessionEnded.
	Refresh(ctx context.Context, refresh string) (TokenSet, error)
	// EndSession thu hồi phiên Keycloak của refresh token; phiên đã hết coi như thành công.
	EndSession(ctx context.Context, c LoginClient, refresh string) error
}
