package app

// AH-280 (FN-10, TECH-03; ADR-0063): quên mật khẩu — đặt lại bằng mã OTP gửi qua SMS tới số điện thoại đã lưu của tài
// khoản realm HTX.
//   - Mã 06 chữ số, dùng một lần, hiệu lực 05 phút; gửi lại sau 60 giây; tối đa 05 mã / tài khoản / 24 giờ; sai 05 lần
//     thì hủy mã (FN-10/BR-01). Mã lưu dạng HMAC (khóa AG_LOGIN_ID_KEY), không vào log, không trả trong phản hồi.
//   - Chỉ gửi tới số trong IAM, không nhận số do người dùng nhập (BR-02). Tài khoản đã khóa hoặc chưa có số → không gửi.
//   - Kênh SMS tắt → không tạo mã, báo liên hệ người cấp tài khoản (không báo "đã gửi" giả).
//   - Không gọi mạng trong giao dịch DB: kiểm và ghi mã trong một giao dịch ngắn, gửi SMS / đặt mật khẩu sau khi commit.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/store"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
)

// Giới hạn của FN-10/BR-01 (ADR-0063).
const (
	PasswordOTPPerDay = 5
	// PasswordOTPWindow là cửa sổ đếm PasswordOTPPerDay (24 giờ trượt).
	PasswordOTPWindow = 24 * time.Hour
)

// Câu thông báo của FN-10 (EX-01 → EX-06, AC-N2, AC-N4).
const (
	MsgResetOTPNotFound    = "Không tìm thấy tài khoản với tên đăng nhập hoặc số điện thoại này"
	MsgResetOTPBlocked     = "Tài khoản đã bị khóa hoặc chưa có số điện thoại. Vui lòng liên hệ người cấp tài khoản."
	MsgResetOTPUnavailable = "Chưa gửi được mã OTP qua SMS. Vui lòng liên hệ người cấp tài khoản để được cấp mật khẩu tạm."
	MsgResetOTPDailyLimit  = "Đã gửi quá 05 lần trong ngày. Vui lòng liên hệ người cấp tài khoản."
	MsgResetOTPInvalid     = "Mã OTP không đúng. Còn %d lần thử"
	MsgResetOTPExpired     = "Mã OTP đã hết hạn. Vui lòng gửi lại mã mới."
	MsgResetOTPExhausted   = "Mã OTP đã bị hủy do nhập sai 05 lần. Vui lòng gửi lại mã mới."
	MsgResetOTPCodeFormat  = "Nhập mã OTP gồm 6 chữ số"
	resetOTPSMSText        = "Ma OTP dat lai mat khau App HTX: %s. Hieu luc 5 phut. Khong chia se ma nay."
)

var (
	ErrResetOTPNotFound    = problem.New(http.StatusNotFound, "accounts.password_otp_account_not_found", "Không tìm thấy tài khoản")
	ErrResetOTPBlocked     = problem.New(http.StatusForbidden, "accounts.password_otp_blocked", "Không gửi được mã OTP cho tài khoản này")
	ErrResetOTPUnavailable = problem.New(http.StatusServiceUnavailable, "accounts.password_otp_unavailable", "Chưa gửi được mã OTP qua SMS")
	ErrResetOTPDailyLimit  = problem.New(http.StatusTooManyRequests, "accounts.password_otp_daily_limit", "Đã gửi quá số lần trong ngày")
	ErrResetOTPInvalid     = problem.New(http.StatusUnauthorized, "accounts.password_otp_invalid", "Mã OTP không đúng")
	ErrResetOTPExpired     = problem.New(http.StatusGone, "accounts.password_otp_expired", "Mã OTP đã hết hạn hoặc đã bị hủy")
)

// PasswordOTPPrompt là kết quả gửi mã: số điện thoại đã che, thời gian tính bằng giây.
type PasswordOTPPrompt struct {
	ChallengeID uuid.UUID
	PhoneHint   string
	ExpiresIn   int
	ResendAfter int
}

// PasswordOTPReset là bước đặt mật khẩu mới bằng mã. Không vào log.
type PasswordOTPReset struct {
	Identifier      string `pii:"true"`
	ChallengeID     uuid.UUID
	OTP             string `pii:"true"`
	NewPassword     string `pii:"true"`
	ConfirmPassword string `pii:"true"`
}

func (s *Service) passwordOTPConfigured() bool {
	return len(s.NativeHTXKey) > 0 && len(s.LoginIDKey) > 0 && s.Dir != nil
}

// maskPhone che SĐT 4 đầu / 3 cuối (0913***222, FN-10/AC-01).
func maskPhone(phone string) string {
	r := []rune(phone)
	if len(r) <= 7 {
		return strings.Repeat("*", len(r))
	}
	return string(r[:4]) + strings.Repeat("*", len(r)-7) + string(r[len(r)-3:])
}

func (s *Service) passwordOTPUser(ctx context.Context, identifier string) (port.User, error) {
	if strings.TrimSpace(identifier) == "" {
		return port.User{}, problem.Validation.WithField("identifier", domain.MsgIdentifierRequired)
	}
	if len(identifier) > domain.MaxLoginIdentifierLen {
		return port.User{}, problem.Validation.WithField("identifier", "Tên đăng nhập hoặc số điện thoại quá dài")
	}
	_, phone := domain.NormalizeLoginIdentifier(identifier)
	u, found, _, err := s.findLoginUser(ctx, identifier, phone)
	if err != nil {
		return port.User{}, err
	}
	if !found || u.TenantID == uuid.Nil {
		return port.User{}, ErrResetOTPNotFound.WithDetail(MsgResetOTPNotFound)
	}
	return u, nil
}

// RequestPasswordOTP gửi mã OTP đặt lại mật khẩu (FN-10 B1 → B3). POST /v1/public/auth/password-otp.
func (s *Service) RequestPasswordOTP(ctx context.Context, identifier string) (PasswordOTPPrompt, error) {
	if !s.passwordOTPConfigured() {
		return PasswordOTPPrompt{}, ErrLoginUnavailable
	}
	u, err := s.passwordOTPUser(ctx, identifier)
	if err != nil {
		return PasswordOTPPrompt{}, err
	}
	if !u.Enabled || strings.TrimSpace(u.Phone) == "" {
		return PasswordOTPPrompt{}, ErrResetOTPBlocked.WithDetail(MsgResetOTPBlocked)
	}
	if s.SMS == nil {
		return PasswordOTPPrompt{}, ErrResetOTPUnavailable.WithDetail(MsgResetOTPUnavailable)
	}
	return s.sendPasswordOTP(ctx, u)
}

// sendPasswordOTP tạo (hoặc dùng lại khi còn trong 60 giây chờ) mã và gửi SMS. Dùng chung cho bước gửi hộ (AH-281).
func (s *Service) sendPasswordOTP(ctx context.Context, u port.User) (PasswordOTPPrompt, error) {
	code, err := domain.NewLoginOTPCode(nil)
	if err != nil {
		return PasswordOTPPrompt{}, ErrDependency.WithCause(err)
	}
	now := s.now()
	fresh := store.LoginOTPChallenge{ID: ids.New(), UserRef: u.ID, CreatedAt: now, ExpiresAt: now.Add(domain.LoginOTPTTL)}
	fresh.CodeHash = domain.LoginOTPHash(s.LoginIDKey, fresh.ID, code)
	var reuse *store.LoginOTPChallenge
	limited := false
	err = tenant.WithTenantTx(ctx, s.DB, u.TenantID, func(tx pgx.Tx) error {
		if err := store.LockPasswordOTPUser(ctx, tx, u.ID); err != nil {
			return err
		}
		n, last, err := store.PasswordOTPStats(ctx, tx, u.ID, now.Add(-PasswordOTPWindow))
		if err != nil {
			return err
		}
		if last != nil && last.Open(now) && now.Sub(last.CreatedAt) < domain.LoginOTPResendAfter {
			reuse = last
			return nil
		}
		if n >= PasswordOTPPerDay {
			limited = true
			return nil
		}
		return store.InsertPasswordOTP(ctx, tx, u.TenantID, fresh)
	})
	if err != nil {
		return PasswordOTPPrompt{}, ErrDependency.WithCause(err)
	}
	hint := maskPhone(u.Phone)
	switch {
	case limited:
		return PasswordOTPPrompt{}, ErrResetOTPDailyLimit.WithDetail(MsgResetOTPDailyLimit)
	case reuse != nil:
		return PasswordOTPPrompt{ChallengeID: reuse.ID, PhoneHint: hint, ExpiresIn: seconds(reuse.ExpiresAt.Sub(now)),
			ResendAfter: seconds(reuse.CreatedAt.Add(domain.LoginOTPResendAfter).Sub(now))}, nil
	}
	if err := s.SMS.SendSMS(ctx, u.Phone, fmt.Sprintf(resetOTPSMSText, code)); err != nil {
		if cerr := tenant.WithTenantTx(context.WithoutCancel(ctx), s.DB, u.TenantID, func(tx pgx.Tx) error {
			return store.ClosePasswordOTP(ctx, tx, fresh.ID, s.now())
		}); cerr != nil {
			s.log().Warn("không hủy được mã OTP gửi SMS lỗi", "event", "accounts.password_otp", "outcome", "close_failed", "challenge_id", fresh.ID)
		}
		s.log().Warn("gửi SMS mã OTP lỗi", "event", "accounts.password_otp", "outcome", "send_failed", "user_ref", u.ID)
		return PasswordOTPPrompt{}, ErrResetOTPUnavailable.WithDetail(MsgResetOTPUnavailable)
	}
	s.log().Info("đã gửi mã OTP đặt lại mật khẩu", "event", "accounts.password_otp", "outcome", "sent", "user_ref", u.ID)
	return PasswordOTPPrompt{ChallengeID: fresh.ID, PhoneHint: hint, ExpiresIn: seconds(domain.LoginOTPTTL),
		ResendAfter: seconds(domain.LoginOTPResendAfter)}, nil
}

func validatePasswordOTPReset(in PasswordOTPReset) error {
	p := problem.Validation
	bad := false
	if in.ChallengeID == uuid.Nil {
		p, bad = p.WithField("challenge_id", "Gửi mã OTP trước khi đặt lại mật khẩu"), true
	}
	if !otpRe.MatchString(in.OTP) {
		p, bad = p.WithField("otp", MsgResetOTPCodeFormat), true
	}
	switch n := utf8.RuneCountInString(in.NewPassword); {
	case n < MinFirstPasswordLen:
		p, bad = p.WithField("new_password", MsgFirstLoginTooShort), true
	case len(in.NewPassword) > domain.MaxLoginPasswordLen:
		p, bad = p.WithField("new_password", "Mật khẩu quá dài"), true
	}
	if in.ConfirmPassword != in.NewPassword {
		p, bad = p.WithField("confirm_password", MsgFirstLoginMismatch), true
	}
	if bad {
		return p
	}
	return nil
}

// ResetPasswordWithOTP đặt mật khẩu mới bằng mã OTP (FN-10 B4 → B5). POST /v1/public/auth/password-otp/reset.
// Lần nhập sai được commit trước khi trả lỗi; mã chỉ đánh dấu đã dùng sau khi IAM nhận mật khẩu mới.
func (s *Service) ResetPasswordWithOTP(ctx context.Context, in PasswordOTPReset) error {
	if !s.passwordOTPConfigured() {
		return ErrLoginUnavailable
	}
	if err := validatePasswordOTPReset(in); err != nil {
		return err
	}
	u, err := s.passwordOTPUser(ctx, in.Identifier)
	if err != nil {
		return err
	}
	if !u.Enabled {
		return ErrResetOTPBlocked.WithDetail(MsgResetOTPBlocked)
	}
	setter, ok := s.Dir.(port.PasswordSetter)
	remover, ok2 := s.Dir.(port.RequiredActionRemover)
	if !ok || !ok2 {
		return ErrDependency.WithDetail("Kho tài khoản không hỗ trợ đặt lại mật khẩu")
	}
	now := s.now()
	var verr error
	err = tenant.WithTenantTx(ctx, s.DB, u.TenantID, func(tx pgx.Tx) error {
		c, err := store.PasswordOTPByID(ctx, tx, in.ChallengeID)
		if err != nil {
			return err
		}
		if c == nil || c.UserRef != u.ID || !c.Open(now) {
			verr = ErrResetOTPExpired.WithDetail(MsgResetOTPExpired)
			return nil
		}
		if !domain.LoginOTPMatches(s.LoginIDKey, c.ID, in.OTP, c.CodeHash) {
			n, err := store.FailPasswordOTP(ctx, tx, c.ID, domain.LoginOTPMaxAttempts, now)
			if err != nil {
				return err
			}
			if left := domain.LoginOTPMaxAttempts - n; left > 0 {
				verr = ErrResetOTPInvalid.With("remaining_attempts", left).WithDetail(fmt.Sprintf(MsgResetOTPInvalid, left))
			} else {
				verr = ErrResetOTPExpired.WithDetail(MsgResetOTPExhausted)
			}
		}
		return nil
	})
	if err != nil {
		return ErrDependency.WithCause(err)
	}
	if verr != nil {
		return verr
	}
	if err := setter.SetPassword(ctx, u.ID, in.NewPassword); err != nil {
		if errors.Is(err, port.ErrPasswordPolicy) {
			return problem.Validation.WithField("new_password", MsgFirstLoginPolicy)
		}
		return dirErr(err)
	}
	// QTPQ-Q-07: người dùng đã tự đặt mật khẩu nên không phải đổi mật khẩu lần đầu nữa; phiên cũ bị thu hồi.
	if err := remover.RemoveRequiredActions(ctx, u.ID, []string{port.ActionUpdatePassword}); err != nil {
		return dirErr(err)
	}
	if err := s.Dir.Logout(ctx, u.ID); err != nil {
		s.log().Warn("không thu hồi được phiên sau khi đặt lại mật khẩu", "event", "accounts.password_otp", "outcome", "logout_failed", "user_ref", u.ID)
	}
	err = tenant.WithTenantTx(ctx, s.DB, u.TenantID, func(tx pgx.Tx) error {
		_, err := store.UsePasswordOTP(ctx, tx, in.ChallengeID, s.now())
		return err
	})
	if err != nil {
		return ErrDependency.WithCause(err)
	}
	s.log().Info("đã đặt lại mật khẩu bằng mã OTP", "event", "accounts.password_otp", "outcome", "reset", "user_ref", u.ID)
	return nil
}
