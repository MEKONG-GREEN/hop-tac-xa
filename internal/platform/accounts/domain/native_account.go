package domain

import (
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
)

// NativeAccountInput is the FN-07 identity sent to IAM, never persisted in PF.
// FN-07 has one cooperative and immutable roles; it is distinct from KN assignments.
type NativeAccountInput struct {
	Username       string    `json:"username" pii:"true"`
	FullName       string    `json:"full_name" pii:"true"`
	Phone          string    `json:"phone" pii:"true"`
	Email          string    `json:"email" pii:"true"`
	CooperativeRef uuid.UUID `json:"cooperative_ref"`
	Roles          []string  `json:"roles"`
}

func (in NativeAccountInput) Normalize() (NativeAccountInput, error) {
	in.Username = strings.ToLower(strings.TrimSpace(in.Username))
	in.FullName = strings.Join(strings.Fields(in.FullName), " ")
	in.Phone = strings.TrimSpace(in.Phone)
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	in.Roles = slices.Clone(in.Roles)
	if !usernameRe.MatchString(in.Username) {
		return in, FieldError{Field: "username", Msg: MsgUsernameBad}
	}
	if in.FullName == "" {
		return in, FieldError{Field: "full_name", Msg: "Bắt buộc nhập"}
	}
	if utf8.RuneCountInString(in.FullName) > 100 {
		return in, FieldError{Field: "full_name", Msg: "Họ tên tối đa 100 ký tự"}
	}
	if !phoneRe.MatchString(in.Phone) {
		return in, FieldError{Field: "phone", Msg: "SĐT gồm 10 chữ số, bắt đầu bằng 0"}
	}
	if !NativeAccountRoles(in.Roles) {
		return in, FieldError{Field: "roles", Msg: "Chọn một vai hoặc hai vai Đại diện HTX và Thành viên HTX"}
	}
	admin := slices.Contains(in.Roles, htxroles.Admin)
	if admin && in.CooperativeRef != uuid.Nil {
		return in, FieldError{Field: "cooperative_ref", Msg: "Admin không gắn HTX"}
	}
	if !admin && in.CooperativeRef == uuid.Nil {
		return in, FieldError{Field: "cooperative_ref", Msg: "Chọn HTX"}
	}
	// Admin uses the existing email OTP login flow (ADR-0061).
	if admin && in.Email == "" {
		return in, FieldError{Field: "email", Msg: MsgEmailMissing}
	}
	if in.Email != "" && !validEmail(in.Email) {
		return in, FieldError{Field: "email", Msg: MsgEmailFormat}
	}
	slices.Sort(in.Roles)
	return in, nil
}

// NativeAccountRoles accepts only the three FN-07 roles, with the single dual-role exception.
func NativeAccountRoles(roles []string) bool {
	return htxroles.ValidAccountRoles(roles)
}
