package handler_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/keycloak"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/clock"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/httpx"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/hop-tac-xa/migrations"
)

// newNativeLoginFixture như newLoginFixture nhưng ở realm HTX (FN-07): chính sách mật khẩu tối thiểu 6 ký tự.
func newNativeLoginFixture(t *testing.T) *loginFixture {
	env := pgtest.Get(t)
	clk := clock.NewFixed(time.Now())
	mem := keycloak.NewMemory()
	mem.SetNow(clk.Now)
	mem.PasswordMinLen = 6
	dir := &testDir{Memory: mem}
	logs := &syncBuf{}
	logger := telemetry.NewLogger("test", logs)
	unit := env.Pool(t, migrations.PF, dbadmin.AppPlatform)
	mod := accounts.New(unit, clk, accounts.Deps{Directory: dir, NativeHTXKey: bytes.Repeat([]byte{27}, 32), Roles: port.HTXRoleCatalog()})
	mod.UsePasswordReset(&fakeMailer{}, resetBase, logger)
	otpMail := &fakeMailer{}
	mod.UseLogin(mem, bytes.Repeat([]byte{7}, 32))
	mod.UseLoginOTPMailer(otpMail)
	eng := httpx.NewEngine(logger, 1<<20)
	mod.RegisterPublic(eng.Group("/v1/public"))
	srv := httptest.NewServer(eng)
	t.Cleanup(srv.Close)
	return &loginFixture{t: t, env: env, clk: clk, mem: mem, dir: dir, srv: srv, logs: logs, tenant: ids.New(), mod: mod, otpMail: otpMail}
}

func (f *loginFixture) firstPassword(identifier, temp, next, confirm string) reply {
	f.t.Helper()
	return f.post(loginRoute, map[string]any{"identifier": identifier, "password": temp, "new_password": next, "confirm_password": confirm})
}

// AH-269 (FN-01/AC-04, BR-03, EX-05, AC-N1, AC-N2): đổi mật khẩu lần đầu ngay ở màn đăng nhập của ứng dụng.
func TestLogin_FN01_AC04_DoiMatKhauLanDau(t *testing.T) {
	f := newNativeLoginFixture(t)
	const temp = "Tam12345"
	id := f.mem.Seed(port.NewUser{Username: "moi.tao", Phone: "0912345611", TenantID: f.tenant, OrgRef: ids.New()}, true, "htx_member")
	if err := f.mem.SetTemporaryPassword(context.Background(), id, temp); err != nil {
		t.Fatal(err)
	}
	// Bước 1: mật khẩu tạm đúng → cần đổi mật khẩu, chưa có token.
	r := f.login("moi.tao", temp, "")
	if r.status != http.StatusOK || r.body["status"] != "password_change_required" || r.body["access_token"] != nil {
		t.Fatalf("bước 1: %d %s", r.status, r.raw)
	}
	field := func(r reply, name, msg string) {
		t.Helper()
		if r.status != http.StatusUnprocessableEntity || !bytes.Contains(r.raw, []byte(name)) || !bytes.Contains(r.raw, []byte(msg)) || r.body["access_token"] != nil {
			t.Fatalf("%s: %d %s", name, r.status, r.raw)
		}
	}
	f.step()
	field(f.firstPassword("moi.tao", temp, "12345", "12345"), "new_password", "Mật khẩu tối thiểu 06 ký tự")
	f.step()
	field(f.firstPassword("moi.tao", temp, "matkhau1", "matkhau2"), "confirm_password", "Mật khẩu nhập lại không khớp")
	f.step()
	field(f.firstPassword("moi.tao", temp, temp, temp), "new_password", "Mật khẩu mới phải khác mật khẩu tạm")
	// Sai mật khẩu tạm: không đổi gì, tính một lần sai như đăng nhập thường.
	f.step()
	if r := f.firstPassword("moi.tao", "SaiTam99", "matkhau1", "matkhau1"); r.status != http.StatusUnauthorized || r.code() != "accounts.login_failed" {
		t.Fatalf("sai mật khẩu tạm: %d %s", r.status, r.raw)
	}
	if f.mem.PasswordMatches(id, "matkhau1") {
		t.Fatal("password changed without the temporary password")
	}
	// Đổi thành công → vào thẳng (có token); lần sau dùng mật khẩu mới.
	f.step()
	r = f.firstPassword("moi.tao", temp, "matkhau1", "matkhau1")
	if r.status != http.StatusOK || r.body["status"] != "authenticated" || r.body["access_token"] == nil {
		t.Fatalf("đổi mật khẩu: %d %s", r.status, r.raw)
	}
	if u, _ := f.mem.GetUser(context.Background(), id); len(u.RequiredActions) != 0 || !f.mem.PasswordMatches(id, "matkhau1") {
		t.Fatal("first-login requirement must be cleared with the new password")
	}
	f.step()
	if r := f.login("moi.tao", "matkhau1", ""); r.status != http.StatusOK || r.body["status"] != "authenticated" {
		t.Fatalf("đăng nhập bằng mật khẩu mới: %d %s", r.status, r.raw)
	}
	f.step()
	if r := f.login("moi.tao", temp, ""); r.status != http.StatusUnauthorized {
		t.Fatalf("mật khẩu tạm hết hiệu lực: %d %s", r.status, r.raw)
	}
	// Mật khẩu không vào log.
	if bytes.Contains([]byte(f.logs.String()), []byte("matkhau1")) || bytes.Contains([]byte(f.logs.String()), []byte(temp)) {
		t.Fatal("password leaked into logs")
	}
}

// QTPQ-Q-07: còn việc khác ngoài đổi mật khẩu tạm (vd đăng ký TOTP) → giữ lỗi cần hoàn tất thiết lập, không đổi mật khẩu.
func TestLogin_AH269_ViecKhacNgoaiDoiMatKhau(t *testing.T) {
	f := newNativeLoginFixture(t)
	id := f.mem.Seed(port.NewUser{Username: "can.otp", Phone: "0912345612", TenantID: f.tenant, OrgRef: ids.New()}, true, "htx_member")
	if err := f.mem.SetTemporaryPassword(context.Background(), id, "Tam12345"); err != nil {
		t.Fatal(err)
	}
	if err := f.mem.AddRequiredActions(context.Background(), id, []string{"CONFIGURE_TOTP"}); err != nil {
		t.Fatal(err)
	}
	if r := f.firstPassword("can.otp", "Tam12345", "matkhau1", "matkhau1"); r.status != http.StatusForbidden || r.code() != "accounts.login_setup_required" {
		t.Fatalf("việc khác: %d %s", r.status, r.raw)
	}
	if f.mem.PasswordMatches(id, "matkhau1") {
		t.Fatal("password must not change")
	}
}
