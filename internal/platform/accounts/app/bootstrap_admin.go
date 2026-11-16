package app

// QTPQ-Q-01 (FN-01, quyết định 16/11/2026): tài khoản Admin đầu tiên tạo bằng lệnh khởi tạo khi triển khai, không có màn
// hình. Mật khẩu tạm sinh ngẫu nhiên và trả về MỘT lần cho người chạy lệnh; tài khoản phải đổi mật khẩu và đăng ký TOTP ở
// lần đăng nhập đầu. Không ghi mật khẩu vào mã nguồn, log hay tài liệu.

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"math/big"
	"strings"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
)

// BootstrapAdminInput là tham số lệnh khởi tạo. TenantID và OrgRef bắt buộc, không có giá trị mặc định.
type BootstrapAdminInput struct {
	TenantID  uuid.UUID
	OrgRef    uuid.UUID
	Username  string `pii:"true"`
	Email     string `pii:"true"`
	FirstName string `pii:"true"`
	LastName  string `pii:"true"`
}

// BootstrapPasswordLen là độ dài mật khẩu tạm (ký tự).
const BootstrapPasswordLen = 20

const (
	bootLower   = "abcdefghijkmnpqrstuvwxyz"
	bootUpper   = "ABCDEFGHJKLMNPQRSTUVWXYZ"
	bootDigit   = "23456789"
	bootSpecial = "!@#$%^&*-_=+"
)

// BootstrapAdmin tạo tài khoản platform_admin bắt đổi mật khẩu và đăng ký TOTP ở lần đăng nhập đầu, đặt mật khẩu tạm và
// trả (id, mật khẩu tạm). Tên đăng nhập đã có thì dừng mà không đụng tài khoản đó. Lỗi sau bước tạo thì xóa người dùng vừa
// tạo để bù (như Create). rnd là nguồn ngẫu nhiên (crypto/rand.Reader).
func BootstrapAdmin(ctx context.Context, dir port.Directory, in BootstrapAdminInput, rnd io.Reader) (uuid.UUID, string, error) {
	username := strings.ToLower(strings.TrimSpace(in.Username))
	switch {
	case dir == nil:
		return uuid.Nil, "", errNoDirectoryYet
	case in.TenantID == uuid.Nil:
		return uuid.Nil, "", errors.New("accounts: thiếu tenant (bắt buộc, không có giá trị mặc định)")
	case in.OrgRef == uuid.Nil:
		return uuid.Nil, "", errors.New("accounts: thiếu đơn vị (bắt buộc, không có giá trị mặc định)")
	case username == "":
		return uuid.Nil, "", errors.New("accounts: thiếu tên đăng nhập")
	}
	setter, ok := dir.(port.PasswordSetter)
	if !ok {
		return uuid.Nil, "", errors.New("accounts: Keycloak Admin chưa hỗ trợ đặt mật khẩu")
	}
	password, err := tempPassword(rnd)
	if err != nil {
		return uuid.Nil, "", err
	}
	id, err := dir.CreateUser(ctx, port.NewUser{
		Username: username, Email: strings.TrimSpace(in.Email), FirstName: in.FirstName, LastName: in.LastName,
		TenantID: in.TenantID, OrgRef: in.OrgRef,
		RequiredActions: []string{port.ActionUpdatePassword, port.ActionConfigureTOTP},
	})
	if errors.Is(err, port.ErrUserExists) {
		return uuid.Nil, "", fmt.Errorf("accounts: tên đăng nhập hoặc email đã có, không đụng tài khoản đó: %w", err)
	}
	if err != nil {
		return uuid.Nil, "", err
	}
	compensate := func(cause error) (uuid.UUID, string, error) {
		if derr := dir.DeleteUser(ctx, id); derr != nil {
			cause = errors.Join(cause, fmt.Errorf("accounts: không xóa được tài khoản vừa tạo: %w", derr))
		}
		return uuid.Nil, "", cause
	}
	if err := dir.AddRealmRoles(ctx, id, []string{authz.RolePlatformAdmin}); err != nil {
		return compensate(err)
	}
	if err := setter.SetPassword(ctx, id, password); err != nil {
		return compensate(err)
	}
	return id, password, nil
}

// tempPassword sinh mật khẩu BootstrapPasswordLen ký tự có đủ chữ hoa, chữ thường, số và ký tự đặc biệt (bỏ ký tự dễ nhầm).
func tempPassword(rnd io.Reader) (string, error) {
	if rnd == nil {
		rnd = rand.Reader
	}
	all := bootLower + bootUpper + bootDigit + bootSpecial
	pick := func(set string) (byte, error) {
		n, err := rand.Int(rnd, big.NewInt(int64(len(set))))
		if err != nil {
			return 0, fmt.Errorf("accounts: không sinh được mật khẩu tạm: %w", err)
		}
		return set[n.Int64()], nil
	}
	out := make([]byte, 0, BootstrapPasswordLen)
	for _, set := range []string{bootLower, bootUpper, bootDigit, bootSpecial} {
		c, err := pick(set)
		if err != nil {
			return "", err
		}
		out = append(out, c)
	}
	for len(out) < BootstrapPasswordLen {
		c, err := pick(all)
		if err != nil {
			return "", err
		}
		out = append(out, c)
	}
	// Xáo trộn để bốn ký tự đầu không cố định theo loại.
	for i := len(out) - 1; i > 0; i-- {
		n, err := rand.Int(rnd, big.NewInt(int64(i+1)))
		if err != nil {
			return "", fmt.Errorf("accounts: không sinh được mật khẩu tạm: %w", err)
		}
		j := int(n.Int64())
		out[i], out[j] = out[j], out[i]
	}
	return string(out), nil
}
