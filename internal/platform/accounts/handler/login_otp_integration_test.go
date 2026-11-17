package handler_test

// ADR-0061 (17/11/2026): Admin (platform_admin) đăng nhập hai bước ở màn riêng — mật khẩu, rồi mã OTP 6 số gửi qua email
// (hiệu lực 5 phút, sai 5 lần thì hủy, gửi lại sau 60 giây, tối đa 10 mã/giờ). Máy chủ ghi phiên Keycloak đã qua mã để hai
// bề mặt API nhận token không có amr otp. DB thật, Keycloak trong bộ nhớ, đồng hồ cố định.

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
)

var otpInMail = regexp.MustCompile(`\b[0-9]{6}\b`)

// admin tạo tài khoản platform_admin có email <username>@example.vn (verified quyết định email đã xác minh).
func (f *loginFixture) admin(username string, verified bool) uuid.UUID {
	f.t.Helper()
	id := f.mem.Seed(port.NewUser{Username: username, Email: username + "@example.vn", TenantID: f.tenant, OrgRef: uuid.New()}, true, "platform_admin")
	if err := f.mem.SetPassword(context.Background(), id, loginPwd); err != nil {
		f.t.Fatal(err)
	}
	f.mem.SetEmailVerified(id, verified)
	return id
}

func (f *loginFixture) loginOTP(identifier, password, challenge, otp string) reply {
	f.t.Helper()
	return f.post(loginRoute, map[string]any{"identifier": identifier, "password": password, "challenge_id": challenge, "otp": otp})
}

// lastCode trả mã trong email OTP gửi gần nhất và số email đã gửi; email phải gửi tới đúng một địa chỉ của tài khoản.
func (f *loginFixture) lastCode() (string, int) {
	f.t.Helper()
	f.otpMail.mu.Lock()
	defer f.otpMail.mu.Unlock()
	if len(f.otpMail.sent) == 0 {
		return "", 0
	}
	m := f.otpMail.sent[len(f.otpMail.sent)-1]
	if len(m.to) != 1 || !strings.HasSuffix(m.to[0], "@example.vn") {
		f.t.Fatalf("email OTP phải gửi đúng địa chỉ của tài khoản: %v", m.to)
	}
	return otpInMail.FindString(m.body), len(f.otpMail.sent)
}

func (f *loginFixture) verified(id uuid.UUID, sid string, roles ...string) bool {
	f.t.Helper()
	if len(roles) == 0 {
		roles = []string{"platform_admin"}
	}
	ok, err := f.mod.SecondFactorAttestor().SecondFactorVerified(context.Background(),
		authz.Principal{ActorRef: id, TenantID: f.tenant, Roles: roles, SessionID: sid})
	if err != nil {
		f.t.Fatal(err)
	}
	return ok
}

func (f *loginFixture) setMailFail(err error) {
	f.otpMail.mu.Lock()
	defer f.otpMail.mu.Unlock()
	f.otpMail.fail = err
}

func wrongCode(code string) string {
	if code == "000000" {
		return "111111"
	}
	return "000000"
}

func TestLoginOTP_HaiBuoc_MatKhauRoiMaEmail(t *testing.T) {
	f := newLoginFixture(t, true)
	id := f.admin("quan.tri", true)

	r := f.login("quan.tri", loginPwd, "")
	ch, _ := r.body["challenge_id"].(string)
	if r.status != http.StatusOK || r.body["status"] != "otp_required" || ch == "" || r.body["access_token"] != nil ||
		r.body["email_hint"] != "q***@example.vn" || r.num("expires_in") != 300 || r.num("resend_after") != 60 {
		t.Fatalf("bước 1: %d %s", r.status, r.raw)
	}
	code, sent := f.lastCode()
	if sent != 1 || code == "" {
		t.Fatalf("phải gửi đúng một email có mã 6 số, gửi %d", sent)
	}
	if n := f.mem.ActiveLoginSessions(id); n != 0 {
		t.Fatalf("bước 1 không được để lại phiên Keycloak, còn %d", n)
	}

	f.step()
	r = f.loginOTP("quan.tri", loginPwd, ch, code)
	if r.status != http.StatusOK || r.body["status"] != "authenticated" {
		t.Fatalf("bước 2: %d %s", r.status, r.raw)
	}
	sid, _ := jwtClaims(t, r.body["access_token"].(string))["sid"].(string)
	if sid == "" {
		t.Fatal("token phải có sid")
	}
	if !f.verified(id, sid) {
		t.Fatal("phiên vừa qua mã email phải được ghi nhận")
	}
	if f.verified(id, uuid.NewString()) || f.verified(uuid.New(), sid) || f.verified(id, sid, "coop_manager") {
		t.Fatal("chỉ đúng phiên, đúng tài khoản, đúng vai OTP email mới được ghi nhận")
	}

	// Mã đã dùng không dùng lại được.
	f.step()
	if r := f.loginOTP("quan.tri", loginPwd, ch, code); r.status != http.StatusGone || r.code() != "accounts.login_otp_expired" {
		t.Fatalf("dùng lại mã: %d %s", r.status, r.raw)
	}
	// Phiên đã ghi hết hạn sau 24 giờ.
	f.clk.Advance(24*time.Hour + time.Minute)
	if f.verified(id, sid) {
		t.Fatal("phiên đã qua mã phải hết hạn sau 24 giờ")
	}
}

func TestLoginOTP_SaiMa5LanThiHuy(t *testing.T) {
	f := newLoginFixture(t, true)
	id := f.admin("quan.tri.sai", true)
	ch, _ := f.login("quan.tri.sai", loginPwd, "").body["challenge_id"].(string)
	code, _ := f.lastCode()
	for i := 1; i <= 5; i++ {
		f.step()
		r := f.loginOTP("quan.tri.sai", loginPwd, ch, wrongCode(code))
		if i < 5 {
			if r.status != http.StatusUnauthorized || r.code() != "accounts.login_otp_invalid" || r.num("remaining_attempts") != 5-i {
				t.Fatalf("lần sai %d: %d %s", i, r.status, r.raw)
			}
		} else if r.status != http.StatusGone || r.code() != "accounts.login_otp_expired" {
			t.Fatalf("lần sai thứ 5 phải hủy mã: %d %s", r.status, r.raw)
		}
		if n := f.mem.ActiveLoginSessions(id); n != 0 {
			t.Fatalf("lần sai %d không được để lại phiên, còn %d", i, n)
		}
	}
	f.step()
	if r := f.loginOTP("quan.tri.sai", loginPwd, ch, code); r.status != http.StatusGone {
		t.Fatalf("mã đã hủy thì mã đúng cũng không dùng được: %d %s", r.status, r.raw)
	}
}

func TestLoginOTP_GuiLaiSau60GiayVaHetHan5Phut(t *testing.T) {
	f := newLoginFixture(t, true)
	f.admin("quan.tri.gui", true)
	first, _ := f.login("quan.tri.gui", loginPwd, "").body["challenge_id"].(string)
	oldCode, _ := f.lastCode()

	// Trong 60 giây: trả lại thử thách cũ, không gửi thêm.
	f.clk.Advance(20 * time.Second)
	r := f.login("quan.tri.gui", loginPwd, "")
	if r.body["challenge_id"] != first || r.num("resend_after") != 40 {
		t.Fatalf("gửi lại sớm: %d %s", r.status, r.raw)
	}
	if _, sent := f.lastCode(); sent != 1 {
		t.Fatalf("không được gửi thêm email trong 60 giây, đã gửi %d", sent)
	}

	// Sau 60 giây: mã mới, mã cũ hết dùng.
	f.clk.Advance(41 * time.Second)
	second, _ := f.login("quan.tri.gui", loginPwd, "").body["challenge_id"].(string)
	newCode, sent := f.lastCode()
	if second == "" || second == first || sent != 2 {
		t.Fatalf("phải gửi mã mới sau 60 giây (thử thách %s, %d email)", second, sent)
	}
	f.step()
	if r := f.loginOTP("quan.tri.gui", loginPwd, first, oldCode); r.status != http.StatusGone {
		t.Fatalf("mã cũ phải hết dùng khi đã gửi mã mới: %d %s", r.status, r.raw)
	}

	// Quá 5 phút: mã hết hạn.
	f.clk.Advance(5*time.Minute + time.Second)
	if r := f.loginOTP("quan.tri.gui", loginPwd, second, newCode); r.status != http.StatusGone || r.code() != "accounts.login_otp_expired" {
		t.Fatalf("mã quá 5 phút: %d %s", r.status, r.raw)
	}
}

func TestLoginOTP_Toi10MaMoiGio(t *testing.T) {
	f := newLoginFixture(t, true)
	f.admin("quan.tri.tran", true)
	for i := 1; i <= 10; i++ {
		if r := f.login("quan.tri.tran", loginPwd, ""); r.status != http.StatusOK {
			t.Fatalf("mã thứ %d: %d %s", i, r.status, r.raw)
		}
		f.clk.Advance(61 * time.Second)
	}
	f.clk.Advance(5 * time.Minute) // mã thứ 10 đã hết hạn
	r := f.login("quan.tri.tran", loginPwd, "")
	if r.status != http.StatusTooManyRequests || r.code() != "accounts.login_otp_rate_limited" || r.header.Get("Retry-After") == "" {
		t.Fatalf("mã thứ 11 trong một giờ: %d %s", r.status, r.raw)
	}
	if _, sent := f.lastCode(); sent != 10 {
		t.Fatalf("chỉ gửi 10 email, đã gửi %d", sent)
	}
}

func TestLoginOTP_SaiMatKhauOBuoc2KhongTonLuotMa(t *testing.T) {
	f := newLoginFixture(t, true)
	f.admin("quan.tri.mk", true)
	ch, _ := f.login("quan.tri.mk", loginPwd, "").body["challenge_id"].(string)
	code, _ := f.lastCode()
	f.step()
	if r := f.loginOTP("quan.tri.mk", "sai-mat-khau", ch, code); r.status != http.StatusUnauthorized || r.code() != "accounts.login_failed" {
		t.Fatalf("sai mật khẩu ở bước 2: %d %s", r.status, r.raw)
	}
	f.step()
	if r := f.loginOTP("quan.tri.mk", loginPwd, ch, code); r.status != http.StatusOK {
		t.Fatalf("mã vẫn dùng được sau lượt sai mật khẩu: %d %s", r.status, r.raw)
	}
}

// Fail-closed: không có email đã xác minh hoặc không có kênh email → không gửi, không báo "đã gửi", không có token.
func TestLoginOTP_FailClosed(t *testing.T) {
	f := newLoginFixture(t, true)
	f.admin("quan.tri.chua.xac.minh", false)
	if r := f.login("quan.tri.chua.xac.minh", loginPwd, ""); r.status != http.StatusForbidden ||
		r.code() != "accounts.login_otp_email_missing" || r.body["challenge_id"] != nil {
		t.Fatalf("email chưa xác minh: %d %s", r.status, r.raw)
	}

	id := f.admin("quan.tri.loi.gui", true)
	f.setMailFail(context.DeadlineExceeded)
	f.step()
	if r := f.login("quan.tri.loi.gui", loginPwd, ""); r.status != http.StatusServiceUnavailable || r.code() != "accounts.login_otp_unavailable" {
		t.Fatalf("gửi email lỗi: %d %s", r.status, r.raw)
	}
	f.setMailFail(nil)
	f.step()
	// Mã gửi lỗi đã bị hủy nên không bị chặn bởi thời gian chờ 60 giây.
	if r := f.login("quan.tri.loi.gui", loginPwd, ""); r.status != http.StatusOK || r.num("resend_after") != 60 {
		t.Fatalf("gửi lại sau lỗi: %d %s", r.status, r.raw)
	}
	if n := f.mem.ActiveLoginSessions(id); n != 0 {
		t.Fatalf("không được để lại phiên, còn %d", n)
	}

	f.mod.UseLoginOTPMailer(nil)
	f.step()
	if r := f.login("quan.tri.loi.gui", loginPwd, ""); r.status != http.StatusServiceUnavailable || r.code() != "accounts.login_otp_unavailable" {
		t.Fatalf("chưa cấu hình SMTP: %d %s", r.status, r.raw)
	}
	// Bước 2 thiếu mã → 422.
	if r := f.loginOTP("quan.tri.loi.gui", loginPwd, uuid.NewString(), ""); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("bước 2 thiếu mã: %d %s", r.status, r.raw)
	}
}
