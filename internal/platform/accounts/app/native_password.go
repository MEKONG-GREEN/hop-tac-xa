package app

// AH-281 (FN-10/AC-05, BR-03, EX-02, EX-06; trả lời PO 15/11/2026):
//   - "Đặt lại mật khẩu" của Admin (mọi tài khoản realm HTX) và Đại diện HTX (Thành viên HTX của HTX mình): kênh SMS bật
//     → gửi mã OTP như FN-10 (người dùng nhập mã ở màn Quên mật khẩu); kênh tắt → cấp mật khẩu tạm 08 ký tự hiển thị một
//     lần, mật khẩu cũ hết hiệu lực, phiên bị thu hồi, phải đổi ở lần đăng nhập kế tiếp. Không cho chính mình, không cho
//     tài khoản đã khóa. Ghi sổ ai cấp, cho ai, lúc nào; không ghi mật khẩu.
//   - Người dùng đã đăng nhập tự đổi mật khẩu: kiểm mật khẩu hiện tại bằng một lần gọi Keycloak, mật khẩu mới tối thiểu
//     06 ký tự, khác mật khẩu hiện tại, nhập lại khớp.

import (
	"context"
	"errors"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
)

// Kết quả "Đặt lại mật khẩu" (AH-281).
const (
	ResetModeTemporary = "temporary_password"
	ResetModeOTP       = "otp_sent"
)

// Câu thông báo của tự đổi mật khẩu (FN-10/EX-06, AC-N5).
const (
	MsgSelfChangeCurrentWrong = "Mật khẩu hiện tại không đúng"
	MsgSelfChangeSame         = "Mật khẩu mới phải khác mật khẩu hiện tại"
	MsgSelfChangeCurrentEmpty = "Nhập mật khẩu hiện tại"
)

// NativeResetResult: Mode temporary_password kèm mật khẩu (chỉ phản hồi đầu, phát lại thì rỗng); otp_sent kèm SĐT đã che.
type NativeResetResult struct {
	User              UserView
	Mode              string
	TemporaryPassword string `pii:"true"`
	PhoneHint         string
	Replayed          bool
}

// NativeResetPassword là nút "Đặt lại mật khẩu" của Admin (FN-07 B7).
func (s *Service) NativeResetPassword(ctx context.Context, id uuid.UUID, key string) (NativeResetResult, error) {
	a, err := s.nativeAccountCaller(ctx)
	if err != nil {
		return NativeResetResult{}, err
	}
	return s.nativeResetPassword(ctx, a, id, key)
}

// CoopNativeResetPassword là nút "Đặt lại mật khẩu" của Đại diện HTX (FN-08 B6): chỉ Thành viên HTX của HTX mình.
func (s *Service) CoopNativeResetPassword(ctx context.Context, id uuid.UUID, key string) (NativeResetResult, error) {
	a, err := s.coopAccountCaller(ctx)
	if err != nil {
		return NativeResetResult{}, err
	}
	return s.nativeResetPassword(ctx, a, id, key)
}

func (s *Service) nativeResetPassword(ctx context.Context, a *actor, id uuid.UUID, key string) (NativeResetResult, error) {
	if err := requireKey(key); err != nil {
		return NativeResetResult{}, err
	}
	if id == a.p.ActorRef {
		return NativeResetResult{}, ErrSelfAction
	}
	// Phạm vi: Admin mọi tài khoản của tenant; Đại diện chỉ Thành viên HTX của HTX mình (ngoài phạm vi → 404, AC-N2).
	cur, err := s.nativeTarget(ctx, a, id)
	if err != nil {
		return NativeResetResult{}, err
	}
	if a.coop != uuid.Nil && cur.User.ID != a.p.ActorRef && !coopVisible(a, cur) {
		return NativeResetResult{}, problem.NotFound
	}
	idem := "native-reset:" + key
	if prev, err := s.replayFor(ctx, a, idem, domain.ActionActionsSent, id); err != nil || prev != nil {
		if err != nil {
			return NativeResetResult{}, err
		}
		mode, _ := prev.Details["method"].(string)
		return NativeResetResult{User: cur, Mode: mode, Replayed: true}, nil
	}
	if !cur.User.Enabled {
		return NativeResetResult{}, ErrState.WithDetail("Tài khoản đã khóa, không đặt lại mật khẩu.")
	}
	res := NativeResetResult{User: cur}
	if s.SMS != nil && len(s.LoginIDKey) > 0 {
		if cur.User.Phone == "" {
			return NativeResetResult{}, ErrResetOTPBlocked.WithDetail(MsgResetOTPBlocked)
		}
		prompt, err := s.sendPasswordOTP(ctx, cur.User)
		if err != nil {
			return NativeResetResult{}, err
		}
		res.Mode, res.PhoneHint = ResetModeOTP, prompt.PhoneHint
	} else {
		setter, ok := s.Dir.(port.TemporaryPasswordSetter)
		if !ok {
			return NativeResetResult{}, ErrDependency
		}
		password, err := nativeTemporaryPassword()
		if err != nil {
			return NativeResetResult{}, err
		}
		// Mật khẩu tạm: mật khẩu cũ hết hiệu lực và Keycloak bắt đổi ở lần đăng nhập kế tiếp; thu hồi phiên đang mở.
		if err := setter.SetTemporaryPassword(ctx, id, password); err != nil {
			return NativeResetResult{}, dirErr(err)
		}
		if err := s.Dir.Logout(ctx, id); err != nil {
			return NativeResetResult{}, dirErr(err)
		}
		res.Mode, res.TemporaryPassword = ResetModeTemporary, password
	}
	acc := domain.Account{UserRef: id, OrgRef: cur.User.OrgRef, Roles: cur.Roles, Enabled: true, LockCause: cur.LockCause}
	ev := newEvent(a.p.TenantID, a.p.ActorRef, acc, domain.ActionActionsSent, s.now(), idem, "")
	ev.Details = map[string]any{"flow": "FN-10", "method": res.Mode}
	err = tenant.WithTenantTx(ctx, s.DB, a.p.TenantID, func(tx pgx.Tx) error { return record(ctx, tx, &ev) })
	if err != nil {
		return NativeResetResult{}, err
	}
	s.log().Info("đặt lại mật khẩu tài khoản HTX", "event", "accounts.password_reset", "outcome", res.Mode, "user_ref", id, "actor_ref", a.p.ActorRef)
	return res, nil
}

// SelfChangePassword: người dùng đã đăng nhập tự đổi mật khẩu (FN-10, AC-N4, AC-N5). Không vào log.
type SelfChangePassword struct {
	Current string `pii:"true"`
	New     string `pii:"true"`
	Confirm string `pii:"true"`
}

func validateSelfChange(in SelfChangePassword) error {
	p := problem.Validation
	bad := false
	if in.Current == "" {
		p, bad = p.WithField("current_password", MsgSelfChangeCurrentEmpty), true
	}
	switch n := utf8.RuneCountInString(in.New); {
	case n < MinFirstPasswordLen:
		p, bad = p.WithField("new_password", MsgFirstLoginTooShort), true
	case len(in.New) > domain.MaxLoginPasswordLen:
		p, bad = p.WithField("new_password", "Mật khẩu quá dài"), true
	case in.Current != "" && in.New == in.Current:
		p, bad = p.WithField("new_password", MsgSelfChangeSame), true
	}
	if in.Confirm != in.New {
		p, bad = p.WithField("confirm_password", MsgFirstLoginMismatch), true
	}
	if bad {
		return p
	}
	return nil
}

// ChangeOwnPassword đổi mật khẩu của chính người gọi (realm HTX). PUT /v1/me/password.
func (s *Service) ChangeOwnPassword(ctx context.Context, in SelfChangePassword) error {
	p, ok := authz.FromContext(ctx)
	if !ok || p.ActorRef == uuid.Nil {
		return problem.Unauthenticated
	}
	if len(s.NativeHTXKey) == 0 || !s.loginConfigured() {
		return ErrLoginUnavailable
	}
	if err := validateSelfChange(in); err != nil {
		return err
	}
	setter, ok := s.Dir.(port.PasswordSetter)
	if !ok {
		return ErrDependency.WithDetail("Kho tài khoản không hỗ trợ đổi mật khẩu")
	}
	u, err := s.Dir.GetUser(ctx, p.ActorRef)
	if err != nil {
		return dirErr(err)
	}
	if u.TenantID != p.TenantID {
		return problem.Unauthenticated
	}
	// Kiểm mật khẩu hiện tại bằng đúng một lần gọi Keycloak (cùng bộ đếm sai); phiên phát ra để kiểm thì thu hồi ngay.
	ts, err := s.PwLogin.PasswordGrant(ctx, u.Username, in.Current, "")
	switch {
	case err == nil:
		s.endSession(ctx, ts.RefreshToken)
	case errors.Is(err, port.ErrAccountNotSetUp):
		// Mật khẩu đúng, tài khoản còn việc lần đầu: vẫn cho đổi.
	case errors.Is(err, port.ErrInvalidGrant):
		return problem.Validation.WithField("current_password", MsgSelfChangeCurrentWrong)
	case errors.Is(err, port.ErrAccountDisabled):
		return accountLockedEX03()
	default:
		return dirErr(err)
	}
	if err := setter.SetPassword(ctx, u.ID, in.New); err != nil {
		if errors.Is(err, port.ErrPasswordPolicy) {
			return problem.Validation.WithField("new_password", MsgFirstLoginPolicy)
		}
		return dirErr(err)
	}
	if remover, ok := s.Dir.(port.RequiredActionRemover); ok {
		if err := remover.RemoveRequiredActions(ctx, u.ID, []string{port.ActionUpdatePassword}); err != nil {
			return dirErr(err)
		}
	}
	s.log().Info("người dùng tự đổi mật khẩu", "event", "accounts.password_change", "outcome", "changed", "user_ref", u.ID)
	return nil
}
