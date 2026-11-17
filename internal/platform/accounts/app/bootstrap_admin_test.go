package app

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"unicode"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/keycloak"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
)

func bootInput() BootstrapAdminInput {
	return BootstrapAdminInput{TenantID: uuid.New(), OrgRef: uuid.New(), Username: "Admin.Dau.Tien", Email: " Admin@Example.VN "}
}

func TestBootstrapAdmin_ThieuTenantHoacDonViThiDung(t *testing.T) {
	dir := keycloak.NewMemory()
	for name, mutate := range map[string]func(*BootstrapAdminInput){
		"thiếu tenant":   func(in *BootstrapAdminInput) { in.TenantID = uuid.Nil },
		"thiếu đơn vị":   func(in *BootstrapAdminInput) { in.OrgRef = uuid.Nil },
		"thiếu tên":      func(in *BootstrapAdminInput) { in.Username = "  " },
		"thiếu email":    func(in *BootstrapAdminInput) { in.Email = "" },
		"email sai":      func(in *BootstrapAdminInput) { in.Email = "khong-phai-email" },
		"không Keycloak": nil,
	} {
		in := bootInput()
		var d port.Directory = dir
		if mutate == nil {
			d = nil
		} else {
			mutate(&in)
		}
		if id, pw, err := BootstrapAdmin(context.Background(), d, in, nil); err == nil || id != uuid.Nil || pw != "" {
			t.Errorf("%s: phải dừng, được id=%v err=%v", name, id, err)
		}
	}
	if users, _ := dir.FindUsers(context.Background(), port.Query{}); len(users) != 0 {
		t.Fatalf("thiếu tham số thì không tạo gì, có %d tài khoản", len(users))
	}
}

// ADR-0061: Admin phải đổi mật khẩu ở lần đầu, không đăng ký TOTP; email đã xác minh để nhận mã OTP đăng nhập.
func TestBootstrapAdmin_TaoPlatformAdminPhaiDoiMatKhauVaCoEmail(t *testing.T) {
	ctx := context.Background()
	dir := keycloak.NewMemory()
	in := bootInput()
	id, pw, err := BootstrapAdmin(ctx, dir, in, nil)
	if err != nil {
		t.Fatal(err)
	}
	u, err := dir.GetUser(ctx, id)
	if err != nil || u.Username != "admin.dau.tien" || u.TenantID != in.TenantID || u.OrgRef != in.OrgRef || !u.Enabled {
		t.Fatalf("tài khoản: %+v %v", u, err)
	}
	if !slices.Equal(u.RequiredActions, []string{port.ActionUpdatePassword}) {
		t.Fatalf("required actions: %v", u.RequiredActions)
	}
	if u.Email != "admin@example.vn" || !u.EmailVerified {
		t.Fatalf("email phải chuẩn hóa và đã xác minh: %q %v", u.Email, u.EmailVerified)
	}
	if roles, _ := dir.RealmRoles(ctx, id); !slices.Equal(roles, []string{authz.RolePlatformAdmin}) {
		t.Fatalf("vai: %v", roles)
	}
	if !dir.PasswordMatches(id, pw) {
		t.Fatal("mật khẩu tạm phải được đặt cho tài khoản")
	}
	if len(pw) != BootstrapPasswordLen || !strings.ContainsFunc(pw, unicode.IsUpper) || !strings.ContainsFunc(pw, unicode.IsLower) ||
		!strings.ContainsFunc(pw, unicode.IsDigit) || !strings.ContainsFunc(pw, func(r rune) bool { return strings.ContainsRune(bootSpecial, r) }) {
		t.Fatalf("mật khẩu tạm phải đủ độ dài và đủ loại ký tự (độ dài %d)", len(pw))
	}
	// Hai lần sinh khác nhau (ngẫu nhiên).
	_, pw2, err := BootstrapAdmin(ctx, dir, BootstrapAdminInput{TenantID: in.TenantID, OrgRef: in.OrgRef, Username: "admin2", Email: "admin2@example.vn"}, nil)
	if err != nil || pw2 == pw {
		t.Fatalf("mật khẩu tạm phải ngẫu nhiên: %v", err)
	}
}

func TestBootstrapAdmin_TrungTenKhongDungTaiKhoanCu(t *testing.T) {
	ctx := context.Background()
	dir := keycloak.NewMemory()
	in := bootInput()
	old := dir.Seed(port.NewUser{Username: "admin.dau.tien", TenantID: in.TenantID, OrgRef: in.OrgRef}, true, "coop_manager")
	if err := dir.SetPassword(ctx, old, "Mat-khau-cu-dai-12!"); err != nil {
		t.Fatal(err)
	}
	_, _, err := BootstrapAdmin(ctx, dir, in, nil)
	if !errors.Is(err, port.ErrUserExists) {
		t.Fatalf("tên trùng phải dừng: %v", err)
	}
	if roles, _ := dir.RealmRoles(ctx, old); !slices.Equal(roles, []string{"coop_manager"}) || !dir.PasswordMatches(old, "Mat-khau-cu-dai-12!") {
		t.Fatal("không được đụng tài khoản cũ")
	}
}

func TestBootstrapAdmin_LoiGiuaChungThiXoaBu(t *testing.T) {
	ctx := context.Background()
	for _, op := range []string{"AddRealmRoles", "SetPassword"} {
		dir := keycloak.NewMemory()
		dir.Fail(op, errors.New("keycloak lỗi"))
		if _, pw, err := BootstrapAdmin(ctx, dir, bootInput(), nil); err == nil || pw != "" {
			t.Fatalf("%s: phải lỗi", op)
		}
		if users, _ := dir.FindUsers(ctx, port.Query{}); len(users) != 0 {
			t.Fatalf("%s: lỗi giữa chừng phải xóa tài khoản vừa tạo, còn %d", op, len(users))
		}
	}
}

func TestBootstrapAdmin_NguonNgauNhienLoiThiKhongTao(t *testing.T) {
	dir := keycloak.NewMemory()
	if _, _, err := BootstrapAdmin(context.Background(), dir, bootInput(), bytes.NewReader(nil)); err == nil {
		t.Fatal("không sinh được mật khẩu thì phải lỗi")
	}
	if users, _ := dir.FindUsers(context.Background(), port.Query{}); len(users) != 0 {
		t.Fatal("không sinh được mật khẩu thì không tạo tài khoản")
	}
}
