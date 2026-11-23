package app

// AH-269 (FN-01/AC-04, BR-03, EX-05; QTPQ-Q-05..Q-07): tài khoản realm HTX vừa được cấp mật khẩu tạm phải đổi mật
// khẩu ngay ở màn đăng nhập của ứng dụng, trước khi có token.
//   - Bước 1: đăng nhập bằng mật khẩu tạm → Keycloak chỉ báo "chưa hoàn tất thiết lập" SAU khi mật khẩu đúng →
//     trả status password_change_required, không có token.
//   - Bước 2: gửi lại mật khẩu tạm kèm mật khẩu mới và nhập lại. Máy chủ kiểm mật khẩu tạm lại bằng đúng một lần
//     gọi Keycloak (cùng bộ đếm sai, cùng khóa tạm), đặt mật khẩu cố định (Keycloak bỏ UPDATE_PASSWORD), rồi đăng
//     nhập bằng mật khẩu mới để vào thẳng trang chủ. Admin (OTP email) tiếp tục bước OTP như đăng nhập thường.
//   - Không lưu, không băm, không ghi log mật khẩu; chỉ so sánh trong bộ nhớ của request.

import (
	"context"
	"errors"
	"net/http"
	"time"
	"unicode/utf8"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
)

// LoginPasswordChangeRequired: mật khẩu tạm đúng, cần đổi mật khẩu lần đầu (không có token).
const LoginPasswordChangeRequired = "password_change_required"

// Câu thông báo tại trường của bước đổi mật khẩu lần đầu (FN-01/EX-05, AC-N1, AC-N2).
const (
	MinFirstPasswordLen     = 6
	MsgFirstLoginTooShort   = "Mật khẩu tối thiểu 06 ký tự"
	MsgFirstLoginMismatch   = "Mật khẩu nhập lại không khớp"
	MsgFirstLoginSameAsTemp = "Mật khẩu mới phải khác mật khẩu tạm"
	MsgFirstLoginPolicy     = "Mật khẩu mới không hợp lệ: không trùng tên đăng nhập hoặc mật khẩu đã dùng gần đây"
)

// firstPasswordOnly: chỉ realm HTX, và chỉ khi việc còn thiếu duy nhất là đổi mật khẩu tạm. Còn việc khác (vd đăng ký
// TOTP) thì giữ lỗi cần hoàn tất thiết lập như trước.
func (s *Service) firstPasswordOnly(u port.User) bool {
	return len(s.NativeHTXKey) > 0 && len(u.RequiredActions) == 1 && u.RequiredActions[0] == port.ActionUpdatePassword
}

func validateFirstPassword(in LoginInput) error {
	p := problem.Validation
	bad := false
	switch n := utf8.RuneCountInString(in.NewPassword); {
	case n < MinFirstPasswordLen:
		p, bad = p.WithField("new_password", MsgFirstLoginTooShort), true
	case len(in.NewPassword) > domain.MaxLoginPasswordLen:
		p, bad = p.WithField("new_password", "Mật khẩu quá dài"), true
	case in.NewPassword == in.Password:
		p, bad = p.WithField("new_password", MsgFirstLoginSameAsTemp), true
	}
	if in.ConfirmPassword != in.NewPassword {
		p, bad = p.WithField("confirm_password", MsgFirstLoginMismatch), true
	}
	if bad {
		return p
	}
	return nil
}

// Hạn của mật khẩu tạm (FN-TK-02/BR-03, AC-04 — AH-223): quá 72 giờ kể từ lúc cấp (tạo tài khoản hoặc đặt lại) mà
// chưa đổi thì không đăng nhập được bằng mật khẩu đó. Người dùng lấy lại bằng "Quên mật khẩu?" (OTP qua SMS, FN-10)
// hoặc nhờ người cấp tài khoản đặt lại (AH-281).
const TemporaryPasswordLifespan = 72 * time.Hour

// MsgTemporaryPasswordExpired là câu báo khi mật khẩu tạm đúng nhưng đã hết hạn.
const MsgTemporaryPasswordExpired = `Mật khẩu tạm đã quá 72 giờ nên không còn dùng được. Dùng "Quên mật khẩu?" hoặc liên hệ người đã cấp tài khoản để được cấp lại.` //nolint:gosec // câu thông báo, không phải bí mật

// ErrTemporaryPasswordExpired: mật khẩu tạm đúng nhưng quá hạn — không phát token, không đổi mật khẩu.
var ErrTemporaryPasswordExpired = problem.New(http.StatusForbidden, "accounts.temporary_password_expired", "Mật khẩu tạm đã hết hạn")

// temporaryPasswordExpired đọc lúc cấp mật khẩu tạm ở IAM. Không đọc được mốc (IAM không hỗ trợ hoặc không trả) thì coi
// như đã hết hạn — đóng khi lỗi, không mở đường đăng nhập bằng mật khẩu tạm không rõ tuổi.
func (s *Service) temporaryPasswordExpired(ctx context.Context, u port.User) (bool, error) {
	reader, ok := s.Dir.(port.PasswordAgeReader)
	if !ok {
		return false, ErrDependency.WithDetail("Kho tài khoản không đọc được thời điểm cấp mật khẩu tạm")
	}
	at, err := reader.PasswordSetAt(ctx, u.ID)
	if err != nil {
		return false, dirErr(err)
	}
	return at.IsZero() || s.now().Sub(at) > TemporaryPasswordLifespan, nil
}

// firstPassword chạy khi mật khẩu tạm vừa được Keycloak xác nhận đúng (ErrAccountNotSetUp).
func (s *Service) firstPassword(ctx context.Context, u port.User, in LoginInput) (LoginResult, string, error) {
	// Kiểm hạn SAU khi mật khẩu tạm đã đúng: người không biết mật khẩu không dò được tài khoản nào có mật khẩu tạm.
	if expired, err := s.temporaryPasswordExpired(ctx, u); err != nil {
		return LoginResult{}, "dependency_error", err
	} else if expired {
		return LoginResult{}, "temporary_password_expired", ErrTemporaryPasswordExpired.WithDetail(MsgTemporaryPasswordExpired)
	}
	if in.NewPassword == "" && in.ConfirmPassword == "" {
		return LoginResult{Status: LoginPasswordChangeRequired}, LoginPasswordChangeRequired, nil
	}
	if err := validateFirstPassword(in); err != nil {
		return LoginResult{}, "first_password_invalid", err
	}
	setter, ok := s.Dir.(port.PasswordSetter)
	remover, ok2 := s.Dir.(port.RequiredActionRemover)
	if !ok || !ok2 {
		return LoginResult{}, "dependency_error", ErrDependency.WithDetail("Kho tài khoản không hỗ trợ đổi mật khẩu")
	}
	// Mật khẩu cố định; chính sách realm (độ dài, lịch sử) kiểm ở IAM. Sau đó bỏ yêu cầu UPDATE_PASSWORD.
	if err := setter.SetPassword(ctx, u.ID, in.NewPassword); err != nil {
		if errors.Is(err, port.ErrPasswordPolicy) {
			return LoginResult{}, "first_password_policy", problem.Validation.WithField("new_password", MsgFirstLoginPolicy)
		}
		return LoginResult{}, "dependency_error", dirErr(err)
	}
	if err := remover.RemoveRequiredActions(ctx, u.ID, []string{port.ActionUpdatePassword}); err != nil {
		return LoginResult{}, "dependency_error", dirErr(err)
	}
	ts, err := s.PwLogin.PasswordGrant(ctx, u.Username, in.NewPassword, "")
	if err != nil {
		return LoginResult{}, "dependency_error", dirErr(err)
	}
	if port.NeedsLoginEmailOTP(ts.Roles) {
		// Admin: bước OTP email như đăng nhập thường; bước 2 dùng mật khẩu mới.
		return s.loginEmailOTP(ctx, u, LoginInput{Identifier: in.Identifier, Password: in.NewPassword}, ts)
	}
	if s.missingSecondFactor(ts) {
		s.endSession(ctx, ts.RefreshToken)
		return LoginResult{}, "setup_required", ErrLoginSetupRequired
	}
	return LoginResult{Status: LoginAuthenticated, Tokens: ts}, "first_password_changed", nil
}
