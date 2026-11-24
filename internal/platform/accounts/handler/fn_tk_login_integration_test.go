package handler_test

// BRD Thời tiết & Thu gom phụ phẩm, phần bù không trái BRD Quản trị phân quyền (AH-222 FN-TK-01, AH-223 FN-TK-02):
// tài khoản không còn vai App HTX (EX-02) và mật khẩu tạm quá 72 giờ (AC-04). Realm HTX, DB thật, Keycloak trong bộ nhớ.

import (
	"bytes"
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
)

// FN-TK-01/EX-02: mật khẩu đúng mà tài khoản không còn vai nào của App HTX → câu EX-02, không có token, phiên vừa phát
// ở Keycloak bị thu hồi. Mật khẩu sai vẫn là câu sai tài khoản/mật khẩu (không lộ tài khoản nào không còn vai).
func TestLogin_FNTK01_EX02_KhongConVai(t *testing.T) {
	f := newNativeLoginFixture(t)
	id := f.account("khong.vai", "0912345671", "offline_access")
	r := f.login("khong.vai", loginPwd, "")
	if r.status != http.StatusForbidden || r.code() != "accounts.no_active_role" || r.body["access_token"] != nil {
		t.Fatalf("EX-02: %d %s", r.status, r.raw)
	}
	if !bytes.Contains(r.raw, []byte("Tài khoản chưa được gán vai trò. Liên hệ quản trị viên.")) {
		t.Fatalf("câu EX-02: %s", r.raw)
	}
	if n := f.mem.ActiveLoginSessions(id); n != 0 {
		t.Fatalf("phiên phải bị thu hồi, còn %d", n)
	}
	f.step()
	if r := f.login("khong.vai", "SaiMatKhau9", ""); r.status != http.StatusUnauthorized || r.code() != "accounts.login_failed" {
		t.Fatalf("sai mật khẩu: %d %s", r.status, r.raw)
	}
	// Tài khoản có vai vẫn đăng nhập bình thường.
	f.account("co.vai", "0912345672", "htx_member")
	f.step()
	if r := f.login("co.vai", loginPwd, ""); r.status != http.StatusOK || r.body["status"] != "authenticated" {
		t.Fatalf("tài khoản có vai: %d %s", r.status, r.raw)
	}
}

// FN-TK-02/AC-04, BR-03: mật khẩu tạm quá 72 giờ chưa đổi thì không đăng nhập được bằng mật khẩu đó (cả bước hỏi đổi
// mật khẩu lẫn bước gửi mật khẩu mới); trong 72 giờ vẫn đổi được. Cấp lại mật khẩu tạm thì hạn tính lại.
func TestLogin_FNTK02_AC04_MatKhauTamHetHan(t *testing.T) {
	f := newNativeLoginFixture(t)
	ctx := context.Background()
	id := f.mem.Seed(port.NewUser{Username: "tam.het.han", Phone: "0912345673", TenantID: f.tenant, OrgRef: ids.New()}, true, "htx_member")
	if err := f.mem.SetTemporaryPassword(ctx, id, "Tam12345"); err != nil {
		t.Fatal(err)
	}
	f.clk.Advance(72*time.Hour - time.Minute)
	if r := f.login("tam.het.han", "Tam12345", ""); r.status != http.StatusOK || r.body["status"] != "password_change_required" {
		t.Fatalf("trong 72 giờ: %d %s", r.status, r.raw)
	}
	f.clk.Advance(2 * time.Minute)
	expired := func(r reply) {
		t.Helper()
		if r.status != http.StatusForbidden || r.code() != "accounts.temporary_password_expired" || r.body["access_token"] != nil ||
			!bytes.Contains(r.raw, []byte("Mật khẩu tạm đã quá 72 giờ")) {
			t.Fatalf("quá 72 giờ: %d %s", r.status, r.raw)
		}
	}
	expired(f.login("tam.het.han", "Tam12345", ""))
	f.step()
	expired(f.firstPassword("tam.het.han", "Tam12345", "matkhau1", "matkhau1"))
	if f.mem.PasswordMatches(id, "matkhau1") {
		t.Fatal("không được đổi mật khẩu bằng mật khẩu tạm đã hết hạn")
	}
	// Mật khẩu tạm sai vẫn là một lần sai thường — không lộ trạng thái hết hạn.
	f.step()
	if r := f.login("tam.het.han", "SaiTam99", ""); r.status != http.StatusUnauthorized || r.code() != "accounts.login_failed" {
		t.Fatalf("sai mật khẩu tạm: %d %s", r.status, r.raw)
	}
	// Cấp lại (AH-281) → hạn mới.
	if err := f.mem.SetTemporaryPassword(ctx, id, "Tam67890"); err != nil {
		t.Fatal(err)
	}
	f.step()
	if r := f.firstPassword("tam.het.han", "Tam67890", "matkhau1", "matkhau1"); r.status != http.StatusOK || r.body["status"] != "authenticated" {
		t.Fatalf("sau khi cấp lại: %d %s", r.status, r.raw)
	}
}
