package port

import (
	"context"
	"errors"
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
	// SessionID là phiên Keycloak của token (claim sid, dự phòng session_state): khóa của phiên đã qua mã OTP email
	// (ADR-0061). Rỗng khi token không mang claim nào.
	SessionID string
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
	// PasswordGrant kiểm mật khẩu kèm otp (rỗng nếu không gửi) trong đúng một lần gọi Keycloak và trả token. Không có
	// đường kiểm mật khẩu mà không kiểm yếu tố thứ hai: tài khoản đã cấu hình OTP mà thiếu/sai mã → ErrInvalidGrant.
	PasswordGrant(ctx context.Context, username, password, otp string) (TokenSet, error)
	// Refresh làm mới token bằng client đăng nhập; phiên đã hết → ErrSessionEnded.
	Refresh(ctx context.Context, refresh string) (TokenSet, error)
	// EndSession thu hồi phiên Keycloak của refresh token; phiên đã hết coi như thành công.
	EndSession(ctx context.Context, refresh string) error
}
