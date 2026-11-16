package app

// FN-01 (BRD App HTX — Quản trị phân quyền): đăng nhập bằng tên đăng nhập hoặc SĐT + mật khẩu ở màn riêng của App HTX
// (ADR-0060; quyết định 16/11/2026 và trả lời của nhóm dự án).
//
//   - Keycloak vẫn giữ mật khẩu và phát token; máy chủ chỉ chuyển mật khẩu (và OTP) qua direct grant, không lưu, không băm.
//   - Tài khoản có thật chỉ có MỘT bộ đếm lần sai: của Keycloak. Máy chủ đọc numFailures/lastFailure TRƯỚC khi kiểm mật
//     khẩu rồi cộng lần vừa sai (Keycloak có thể ghi nhận lần sai chậm một nhịp), không đọc lại sau lần sai.
//   - Mã định danh không khớp tài khoản nào dùng bộ đếm HMAC riêng (pf_accounts.login_probe) cùng ngưỡng để phản hồi giống hệt.
//   - Tài khoản "Đã khóa" (FN-07) báo EX-03 dù mật khẩu đúng hay sai (trả lời lượt 2 #1).
//   - Đăng nhập một bước: mỗi lượt gọi Keycloak đúng một lần, gửi mật khẩu kèm OTP nếu người dùng nhập. Không có đường
//     kiểm mật khẩu mà bỏ qua yếu tố thứ hai; tài khoản có OTP mà thiếu mã bị từ chối như sai mật khẩu (không lộ tài
//     khoản nào có OTP) và bị Keycloak tính là một lần sai. Chưa đăng ký OTP mà vai bắt TOTP thì không bao giờ có token.
//   - Mật khẩu, OTP, token và mã định danh không vào log, lỗi hay kho idempotency.

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/store"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
)

// Mã lỗi đăng nhập (docs/errors.md, mục pf/accounts).
var (
	ErrLoginFailed        = problem.New(http.StatusUnauthorized, "accounts.login_failed", "Sai tài khoản hoặc mật khẩu")
	ErrLoginTempLocked    = problem.New(http.StatusLocked, "accounts.login_temporarily_locked", "Tài khoản tạm khóa đăng nhập do nhập sai nhiều lần")
	ErrLoginSetupRequired = problem.New(http.StatusForbidden, "accounts.login_setup_required", "Tài khoản cần hoàn tất thiết lập lần đầu trên trang xác thực")
	ErrLoginUnavailable   = problem.New(http.StatusServiceUnavailable, "accounts.login_unavailable", "Đăng nhập chưa được cấu hình")
	ErrSessionExpired     = problem.New(http.StatusUnauthorized, "accounts.session_expired", "Phiên đăng nhập đã hết")
)

// LoginAuthenticated là trạng thái duy nhất của đăng nhập thành công.
const LoginAuthenticated = "authenticated"

const loginKeyContext = "pf/accounts login v1\x00"

var otpRe = regexp.MustCompile(`^[0-9]{6}$`)

// LoginInput là dữ liệu đăng nhập. Không bao giờ vào log.
type LoginInput struct {
	Identifier string `pii:"true"`
	Password   string `pii:"true"`
	OTP        string `pii:"true"`
}

// LoginResult: Status authenticated kèm Tokens.
type LoginResult struct {
	Status string
	Tokens port.TokenSet
}

func loginFailed(remaining int) error {
	return ErrLoginFailed.With("remaining_attempts", remaining).WithDetail(fmt.Sprintf(domain.MsgLoginFailed, remaining))
}

func loginTempLocked(left time.Duration) error {
	m := domain.CeilMinutes(left)
	return ErrLoginTempLocked.With("retry_after_minutes", m).WithDetail(fmt.Sprintf(domain.MsgLoginTempLocked, m))
}

func accountLockedEX03() error { return ErrAccountLocked.WithDetail(domain.MsgAccountLockedFN01) }

func (s *Service) loginConfigured() bool {
	return s.PwLogin != nil && len(s.LoginIDKey) > 0 && s.Dir != nil
}

func (s *Service) loginLog(outcome string, args ...any) {
	s.log().Info("đăng nhập", append([]any{"event", "accounts.login", "outcome", outcome}, args...)...)
}

func validateLogin(in LoginInput) error {
	p := problem.Validation
	bad := false
	switch {
	case strings.TrimSpace(in.Identifier) == "":
		p, bad = p.WithField("identifier", domain.MsgIdentifierRequired), true
	case len(in.Identifier) > domain.MaxLoginIdentifierLen:
		p, bad = p.WithField("identifier", "Tên đăng nhập hoặc số điện thoại quá dài"), true
	}
	switch {
	case in.Password == "":
		p, bad = p.WithField("password", domain.MsgPasswordRequired), true
	case len(in.Password) > domain.MaxLoginPasswordLen:
		p, bad = p.WithField("password", "Mật khẩu quá dài"), true
	}
	if in.OTP != "" && !otpRe.MatchString(in.OTP) {
		p, bad = p.WithField("otp", "Mã OTP gồm 6 chữ số"), true
	}
	if bad {
		return p
	}
	return nil
}

// Login xử lý POST /v1/public/auth/login.
func (s *Service) Login(ctx context.Context, in LoginInput) (LoginResult, error) {
	if err := validateLogin(in); err != nil {
		return LoginResult{}, err
	}
	if !s.loginConfigured() {
		return LoginResult{}, ErrLoginUnavailable
	}
	canonical, phone := domain.NormalizeLoginIdentifier(in.Identifier)
	u, found, matches, err := s.findLoginUser(ctx, in.Identifier, phone)
	if err != nil {
		return LoginResult{}, err
	}
	key := s.loginKey(canonical)
	if !found {
		if matches > 1 {
			s.log().Warn("số điện thoại khớp nhiều tài khoản, coi như đăng nhập sai", "event", "accounts.login",
				"outcome", "ambiguous_phone", "matches", matches, "id_hash", hex.EncodeToString(key[:8]))
		}
		err := s.failUnknownIdentifier(ctx, key)
		s.loginLog("unknown_identifier")
		return LoginResult{}, err
	}
	res, outcome, err := s.loginKnown(ctx, u, in)
	s.loginLog(outcome, "user_ref", u.ID)
	return res, err
}

func (s *Service) loginKey(canonical string) []byte {
	mac := hmac.New(sha256.New, s.LoginIDKey)
	mac.Write([]byte(loginKeyContext + canonical))
	return mac.Sum(nil)
}

// findLoginUser tra tài khoản: khớp đúng tên đăng nhập trước (duy nhất), không có mới tra theo SĐT; SĐT khớp từ hai tài
// khoản trở lên coi như không tìm thấy (matches ≥ 2).
func (s *Service) findLoginUser(ctx context.Context, identifier, phone string) (u port.User, found bool, matches int, err error) {
	name := strings.ToLower(strings.TrimSpace(identifier))
	users, err := s.Dir.FindUsers(ctx, port.Query{Username: name, Max: 2})
	if err != nil {
		return port.User{}, false, 0, dirErr(err)
	}
	users = slices.DeleteFunc(users, func(c port.User) bool { return !strings.EqualFold(c.Username, name) })
	if len(users) == 1 {
		return users[0], true, 1, nil
	}
	if phone == "" {
		return port.User{}, false, 0, nil
	}
	users, err = s.Dir.FindUsers(ctx, port.Query{Attributes: map[string]string{port.AttrPhone: phone}, Max: 2})
	if err != nil {
		return port.User{}, false, 0, dirErr(err)
	}
	users = slices.DeleteFunc(users, func(c port.User) bool { return c.Phone != phone })
	if len(users) == 1 {
		return users[0], true, 1, nil
	}
	return port.User{}, false, len(users), nil
}

// failUnknownIdentifier đếm một lần sai cho mã không tồn tại, cùng ngưỡng và cùng thông báo với tài khoản có thật.
// Giao dịch ngắn, không I/O mạng bên trong.
func (s *Service) failUnknownIdentifier(ctx context.Context, key []byte) error {
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ErrDependency.WithCause(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	now := s.now()
	p, err := store.LockLoginProbe(ctx, tx, key)
	if err != nil {
		return ErrDependency.WithCause(err)
	}
	if locked, left := p.Locked(now); locked {
		return loginTempLocked(left)
	}
	p = p.Fail(now)
	if err := store.SaveLoginProbe(ctx, tx, key, p); err != nil {
		return ErrDependency.WithCause(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ErrDependency.WithCause(err)
	}
	if locked, left := p.Locked(now); locked {
		return loginTempLocked(left)
	}
	return loginFailed(domain.LoginMaxFailures - p.Failures%domain.LoginMaxFailures)
}

// loginKnown xử lý tài khoản có thật; trả kèm outcome cho nhật ký.
func (s *Service) loginKnown(ctx context.Context, u port.User, in LoginInput) (LoginResult, string, error) {
	// EX-03: chỉ báo khóa khi mã khớp tài khoản đã khóa (FN-07), dù mật khẩu đúng hay sai.
	if !u.Enabled {
		return LoginResult{}, "account_locked", accountLockedEX03()
	}
	// Đọc MỘT lần, trước khi kiểm mật khẩu.
	st, err := s.Dir.LoginLockStatus(ctx, u.ID)
	if err != nil {
		return LoginResult{}, "dependency_error", dirErr(err)
	}
	now := s.now()
	if st.Locked {
		left := domain.LoginLockout
		if st.Until != nil {
			left = st.Until.Sub(now)
		}
		return LoginResult{}, "temp_locked", loginTempLocked(left)
	}
	ts, err := s.PwLogin.PasswordGrant(ctx, u.Username, in.Password, in.OTP)
	if err == nil && s.missingSecondFactor(ts) {
		// Kiểm lại claim: vai bắt TOTP mà token không có amr otp thì thu hồi, không trả token.
		s.endSession(ctx, ts.RefreshToken)
		return LoginResult{}, "setup_required", ErrLoginSetupRequired
	}
	switch {
	case err == nil:
		return LoginResult{Status: LoginAuthenticated, Tokens: ts}, LoginAuthenticated, nil
	case errors.Is(err, port.ErrInvalidGrant):
		remaining, lockFor := domain.AfterFailure(st.Failures, st.LastFailure, now)
		if lockFor > 0 {
			return LoginResult{}, "temp_locked", loginTempLocked(lockFor)
		}
		return LoginResult{}, "failed", loginFailed(remaining)
	case errors.Is(err, port.ErrAccountNotSetUp):
		return LoginResult{}, "setup_required", ErrLoginSetupRequired
	case errors.Is(err, port.ErrAccountDisabled):
		return LoginResult{}, "account_locked", accountLockedEX03()
	}
	return LoginResult{}, "dependency_error", dirErr(err)
}

// missingSecondFactor: token mang vai bắt TOTP lúc đăng nhập nhưng không có amr otp.
func (s *Service) missingSecondFactor(ts port.TokenSet) bool {
	if slices.Contains(ts.AMR, "otp") {
		return false
	}
	return slices.Contains(ts.Roles, port.RoleLoginTOTP) || s.RoleCatalog().NeedsLoginTOTP(ts.Roles)
}

// endSession thu hồi phiên Keycloak vừa phát mà không được trả cho người dùng; lỗi chỉ ghi cảnh báo.
func (s *Service) endSession(ctx context.Context, refresh string) {
	if refresh == "" {
		return
	}
	if err := s.PwLogin.EndSession(ctx, refresh); err != nil {
		s.log().Warn("không thu hồi được phiên đăng nhập bị từ chối", "event", "accounts.login", "outcome", "end_session_failed")
	}
}

// Refresh xử lý POST /v1/public/auth/refresh.
func (s *Service) Refresh(ctx context.Context, refresh string) (port.TokenSet, error) {
	if refresh == "" {
		return port.TokenSet{}, problem.Validation.WithField("refresh_token", "bắt buộc")
	}
	if s.PwLogin == nil {
		return port.TokenSet{}, ErrLoginUnavailable
	}
	ts, err := s.PwLogin.Refresh(ctx, refresh)
	switch {
	case err == nil:
		return ts, nil
	case errors.Is(err, port.ErrSessionEnded):
		return port.TokenSet{}, ErrSessionExpired
	}
	return port.TokenSet{}, dirErr(err)
}

// Logout xử lý POST /v1/public/auth/logout: thu hồi phiên ở Keycloak; phiên đã hết coi như xong.
func (s *Service) Logout(ctx context.Context, refresh string) error {
	if refresh == "" {
		return problem.Validation.WithField("refresh_token", "bắt buộc")
	}
	if s.PwLogin == nil {
		return ErrLoginUnavailable
	}
	err := s.PwLogin.EndSession(ctx, refresh)
	if err == nil || errors.Is(err, port.ErrSessionEnded) {
		return nil
	}
	return dirErr(err)
}
