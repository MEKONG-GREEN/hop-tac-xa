package app

// ADR-0061 (quyết định 17/11/2026): vai trong port.EmailOTPRoles (Admin) đăng nhập HAI bước ở màn riêng của App HTX.
//
//   - Bước 1 (identifier + password): Keycloak kiểm mật khẩu như mọi tài khoản. Đúng → thu hồi ngay phiên vừa mở (không
//     trả token), sinh mã 6 số, ghi HMAC của mã (giao dịch ngắn), gửi email NGOÀI giao dịch rồi trả otp_required kèm
//     challenge_id. Mã còn trong 60 giây chờ thì trả lại thử thách cũ, không gửi thêm.
//   - Bước 2 (identifier + password + challenge_id + otp): Keycloak kiểm lại mật khẩu (web giữ mật khẩu trong bộ nhớ trang,
//     máy chủ không giữ token tạm), rồi kiểm mã; đúng → đánh dấu mã đã dùng và ghi phiên Keycloak (sid) vào
//     pf_accounts.login_mfa_session trong cùng giao dịch, trả token. Sai → thu hồi phiên, đếm một lần sai; sai 5 lần hủy mã.
//   - Thiếu kênh email hoặc tài khoản không có email đã xác minh → từ chối (fail-closed), không báo "đã gửi" giả.
//   - SecondFactorVerified: bước xác thực token của hai bề mặt API tra phiên đã ghi khi token không có amr otp.
//
// Mã, mật khẩu, token và email không vào log hay lỗi.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/store"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/clock"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
)

// LoginOTPRequired là trạng thái của bước 1 thành công: đã gửi mã, chờ người dùng nhập.
const LoginOTPRequired = "otp_required"

// Mã lỗi của đăng nhập bằng OTP email (docs/errors.md, mục pf/accounts).
var (
	ErrLoginOTPInvalid      = problem.New(http.StatusUnauthorized, "accounts.login_otp_invalid", "Mã OTP không đúng")
	ErrLoginOTPExpired      = problem.New(http.StatusGone, "accounts.login_otp_expired", "Mã OTP đã hết hạn hoặc đã bị hủy")
	ErrLoginOTPUnavailable  = problem.New(http.StatusServiceUnavailable, "accounts.login_otp_unavailable", "Chưa gửi được mã OTP qua email")
	ErrLoginOTPEmailMissing = problem.New(http.StatusForbidden, "accounts.login_otp_email_missing", "Tài khoản chưa có email đã xác minh để nhận mã OTP")
	ErrLoginOTPRateLimited  = problem.New(http.StatusTooManyRequests, "accounts.login_otp_rate_limited", "Đã gửi quá nhiều mã OTP")
)

// LoginOTPPrompt là phần trả về của bước 1: thử thách cần gửi lại ở bước 2 và thông tin hiện trên màn nhập mã.
type LoginOTPPrompt struct {
	ChallengeID uuid.UUID
	// EmailHint là địa chỉ đã che (domain.MaskEmail), không phải địa chỉ đầy đủ.
	EmailHint string
	// ExpiresIn, ResendAfter: số giây tới khi mã hết hạn và tới khi được xin mã mới.
	ExpiresIn   int
	ResendAfter int
}

const loginOTPSubject = "Mã OTP đăng nhập App Hợp tác xã"

func loginOTPBody(code string, expires time.Time) string {
	return "Xin chào,\n\nMã OTP đăng nhập App Hợp tác xã của bạn là:\n\n    " + code + "\n\nMã dùng một lần, hết hạn lúc " +
		expires.In(clock.Zone()).Format("15:04 02/01/2006") + ". Không chia sẻ mã này cho bất kỳ ai, kể cả người tự xưng là " +
		"quản trị viên.\n\nNếu bạn không đăng nhập, hãy đổi mật khẩu ngay: ai đó đã biết mật khẩu của bạn.\n"
}

func seconds(d time.Duration) int {
	if d <= 0 {
		return 0
	}
	return int((d + time.Second - 1) / time.Second)
}

// loginEmailOTP xử lý tài khoản có vai đăng nhập bằng OTP email sau khi Keycloak đã nhận mật khẩu (ts là phiên vừa mở).
func (s *Service) loginEmailOTP(ctx context.Context, u port.User, in LoginInput, ts port.TokenSet) (LoginResult, string, error) {
	if in.ChallengeID == uuid.Nil {
		s.endSession(ctx, ts.RefreshToken)
		prompt, outcome, err := s.sendLoginOTP(ctx, u)
		if err != nil {
			return LoginResult{}, outcome, err
		}
		return LoginResult{Status: LoginOTPRequired, OTP: &prompt}, outcome, nil
	}
	outcome, err := s.verifyLoginOTP(ctx, u, in, ts)
	if err != nil {
		s.endSession(ctx, ts.RefreshToken)
		return LoginResult{}, outcome, err
	}
	return LoginResult{Status: LoginAuthenticated, Tokens: ts}, outcome, nil
}

// sendLoginOTP tạo (hoặc dùng lại khi còn trong thời gian chờ) thử thách và gửi mã qua email.
func (s *Service) sendLoginOTP(ctx context.Context, u port.User) (LoginOTPPrompt, string, error) {
	email := strings.TrimSpace(u.Email)
	switch {
	case s.LoginOTPMailer == nil:
		return LoginOTPPrompt{}, "otp_unavailable", ErrLoginOTPUnavailable.WithDetail(domain.MsgLoginOTPUnavailable)
	case email == "" || !u.EmailVerified || u.TenantID == uuid.Nil:
		return LoginOTPPrompt{}, "otp_email_missing", ErrLoginOTPEmailMissing.WithDetail(domain.MsgLoginOTPEmailMissing)
	}
	code, err := domain.NewLoginOTPCode(nil)
	if err != nil {
		return LoginOTPPrompt{}, "dependency_error", ErrDependency.WithCause(err)
	}
	now := s.now()
	fresh := store.LoginOTPChallenge{ID: ids.New(), UserRef: u.ID, CreatedAt: now, ExpiresAt: now.Add(domain.LoginOTPTTL)}
	fresh.CodeHash = domain.LoginOTPHash(s.LoginIDKey, fresh.ID, code)
	var (
		reuse   *store.LoginOTPChallenge
		limited bool
		retryAt time.Time
	)
	err = tenant.WithTenantTx(ctx, s.DB, u.TenantID, func(tx pgx.Tx) error {
		if err := store.LockLoginOTPUser(ctx, tx, u.ID); err != nil {
			return err
		}
		n, last, err := store.LoginOTPStats(ctx, tx, u.ID, now.Add(-domain.LoginOTPWindow))
		if err != nil {
			return err
		}
		if last != nil && last.Open(now) && now.Sub(last.CreatedAt) < domain.LoginOTPResendAfter {
			reuse = last
			return nil
		}
		if n >= domain.LoginOTPPerHour {
			if last != nil && last.Open(now) {
				reuse = last
				return nil
			}
			limited, retryAt = true, now.Add(domain.LoginOTPWindow)
			return nil
		}
		return store.InsertLoginOTP(ctx, tx, u.TenantID, fresh)
	})
	if err != nil {
		return LoginOTPPrompt{}, "dependency_error", ErrDependency.WithCause(err)
	}
	hint := domain.MaskEmail(email)
	switch {
	case limited:
		m := domain.CeilMinutes(retryAt.Sub(now))
		return LoginOTPPrompt{}, "otp_rate_limited", ErrLoginOTPRateLimited.With("retry_after_minutes", m).
			WithDetail(fmt.Sprintf("Đã gửi quá nhiều mã OTP. Thử lại sau %d phút.", m))
	case reuse != nil:
		return LoginOTPPrompt{ChallengeID: reuse.ID, EmailHint: hint, ExpiresIn: seconds(reuse.ExpiresAt.Sub(now)),
			ResendAfter: seconds(reuse.CreatedAt.Add(domain.LoginOTPResendAfter).Sub(now))}, "otp_resend_wait", nil
	}
	if err := s.LoginOTPMailer.Send(ctx, []string{email}, loginOTPSubject, loginOTPBody(code, fresh.ExpiresAt)); err != nil {
		if cerr := tenant.WithTenantTx(ctx, s.DB, u.TenantID, func(tx pgx.Tx) error {
			return store.CloseLoginOTP(ctx, tx, fresh.ID, s.now())
		}); cerr != nil {
			s.log().Warn("không hủy được mã OTP gửi lỗi", "event", "accounts.login", "outcome", "otp_close_failed", "challenge_id", fresh.ID)
		}
		return LoginOTPPrompt{}, "otp_send_failed", ErrLoginOTPUnavailable.WithDetail(domain.MsgLoginOTPUnavailable)
	}
	return LoginOTPPrompt{ChallengeID: fresh.ID, EmailHint: hint, ExpiresIn: seconds(domain.LoginOTPTTL),
		ResendAfter: seconds(domain.LoginOTPResendAfter)}, "otp_sent", nil
}

// verifyLoginOTP kiểm mã của bước 2. Lần sai được ghi (commit) trước khi trả lỗi.
func (s *Service) verifyLoginOTP(ctx context.Context, u port.User, in LoginInput, ts port.TokenSet) (string, error) {
	if ts.SessionID == "" || u.TenantID == uuid.Nil {
		return "dependency_error", ErrDependency.WithCause(errors.New("accounts: token Keycloak thiếu sid, không ghi được phiên đã qua OTP"))
	}
	now := s.now()
	var verr error
	outcome := "authenticated_email_otp"
	err := tenant.WithTenantTx(ctx, s.DB, u.TenantID, func(tx pgx.Tx) error {
		c, err := store.LoginOTPByID(ctx, tx, in.ChallengeID)
		if err != nil {
			return err
		}
		if c == nil || c.UserRef != u.ID || !c.Open(now) {
			outcome, verr = "otp_expired", ErrLoginOTPExpired.WithDetail(domain.MsgLoginOTPExpired)
			return nil
		}
		if !domain.LoginOTPMatches(s.LoginIDKey, c.ID, in.OTP, c.CodeHash) {
			n, err := store.FailLoginOTP(ctx, tx, c.ID, domain.LoginOTPMaxAttempts, now)
			if err != nil {
				return err
			}
			if left := domain.LoginOTPMaxAttempts - n; left > 0 {
				outcome, verr = "otp_invalid", ErrLoginOTPInvalid.With("remaining_attempts", left).
					WithDetail(fmt.Sprintf(domain.MsgLoginOTPInvalid, left))
			} else {
				outcome, verr = "otp_exhausted", ErrLoginOTPExpired.WithDetail(domain.MsgLoginOTPExpired)
			}
			return nil
		}
		return store.UseLoginOTP(ctx, tx, u.TenantID, *c, ts.SessionID, now, now.Add(domain.LoginMFASessionTTL))
	})
	if err != nil {
		return "dependency_error", ErrDependency.WithCause(err)
	}
	return outcome, verr
}

// SecondFactorVerified cho biết phiên của p đã qua mã OTP email (ADR-0061). Chỉ tra DB cho vai đăng nhập bằng OTP email;
// vai khác hoặc token không có sid → false. Giao dịch chỉ đọc theo khóa chính, không gọi Keycloak.
func (s *Service) SecondFactorVerified(ctx context.Context, p authz.Principal) (bool, error) {
	if !port.NeedsLoginEmailOTP(p.Roles) || p.SessionID == "" {
		return false, nil
	}
	if s.DB == nil {
		return false, ErrDependency
	}
	var ok bool
	if err := tenant.WithTenantTxOptions(ctx, s.DB, p.TenantID, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		var e error
		ok, e = store.LoginMFASessionValid(ctx, tx, p.SessionID, p.ActorRef, s.now())
		return e
	}); err != nil {
		return false, ErrDependency.WithCause(err)
	}
	return ok, nil
}
