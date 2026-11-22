package handler_test

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
)

type fakeSMS struct {
	mu   sync.Mutex
	sent []struct{ phone, text string }
}

func (s *fakeSMS) SendSMS(_ context.Context, phone, text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sent = append(s.sent, struct{ phone, text string }{phone, text})
	return nil
}

func (s *fakeSMS) last(t *testing.T) (string, string) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.sent) == 0 {
		t.Fatal("no SMS sent")
	}
	m := s.sent[len(s.sent)-1]
	code := regexp.MustCompile(`\b[0-9]{6}\b`).FindString(m.text)
	if code == "" {
		t.Fatalf("SMS without a 6-digit code: %q", m.text)
	}
	return m.phone, code
}

func (s *fakeSMS) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sent)
}

const otpRoute = "/v1/public/auth/password-otp"

// AH-280 (FN-10/AC-01..AC-04, EX-01..EX-06, AC-N2, AC-N4, AC-N5): quên mật khẩu bằng mã OTP qua SMS.
func TestPasswordOTP_FN10(t *testing.T) {
	f := newNativeLoginFixture(t)
	sms := &fakeSMS{}
	f.mod.UseSMS(sms)
	id := f.mem.Seed(port.NewUser{Username: "tanphu", Phone: "0913456222", TenantID: f.tenant, OrgRef: ids.New()}, true, "htx_member")
	if err := f.mem.SetTemporaryPassword(context.Background(), id, "Tam12345"); err != nil {
		t.Fatal(err)
	}
	request := func() reply { f.t.Helper(); return f.post(otpRoute, map[string]any{"identifier": "tanphu"}) }
	reset := func(challenge, code, pw, confirm string) reply {
		f.t.Helper()
		return f.post(otpRoute+"/reset", map[string]any{"identifier": "tanphu", "challenge_id": challenge, "otp": code, "new_password": pw, "confirm_password": confirm})
	}

	// AC-01: gửi tới số đã lưu, che 4 đầu / 3 cuối; mã không nằm trong phản hồi (AC-N5).
	r := request()
	if r.status != http.StatusOK || r.body["phone_hint"] != "0913***222" || r.body["expires_in"] != float64(300) {
		t.Fatalf("gửi mã: %d %s", r.status, r.raw)
	}
	challenge, _ := r.body["challenge_id"].(string)
	phone, code := sms.last(t)
	if phone != "0913456222" || strings.Contains(string(r.raw), code) {
		t.Fatalf("SMS tới %s; phản hồi lộ mã: %v", phone, strings.Contains(string(r.raw), code))
	}
	// Trong 60 giây: trả lại thử thách cũ, không gửi tin mới.
	if again := request(); again.body["challenge_id"] != challenge || sms.count() != 1 {
		t.Fatalf("gửi lại trong 60 giây: %s", again.raw)
	}
	// AC-02, EX-03: sai 4 lần còn n lần; lần 5 hủy mã; mã đúng sau đó cũng hết hiệu lực.
	wrong := "000000"
	if wrong == code {
		wrong = "111111"
	}
	for left := 4; left >= 1; left-- {
		f.step()
		r := reset(challenge, wrong, "matkhau1", "matkhau1")
		if r.status != http.StatusUnauthorized || r.num("remaining_attempts") != left || !strings.Contains(r.detail(), "Còn") {
			t.Fatalf("sai lần %d: %d %s", 5-left, r.status, r.raw)
		}
	}
	f.step()
	if r := reset(challenge, wrong, "matkhau1", "matkhau1"); r.status != http.StatusGone || !strings.Contains(r.detail(), "05 lần") {
		t.Fatalf("sai lần 5: %d %s", r.status, r.raw)
	}
	f.step()
	if r := reset(challenge, code, "matkhau1", "matkhau1"); r.status != http.StatusGone {
		t.Fatalf("mã đã hủy: %d %s", r.status, r.raw)
	}

	// AC-03, EX-04: nhập mã sau 05 phút → hết hạn.
	f.clk.Advance(61 * time.Second)
	r = request()
	challenge, _ = r.body["challenge_id"].(string)
	_, code = sms.last(t)
	f.clk.Advance(5*time.Minute + time.Second)
	if r := reset(challenge, code, "matkhau1", "matkhau1"); r.status != http.StatusGone || !strings.Contains(r.detail(), "hết hạn") {
		t.Fatalf("mã quá 5 phút: %d %s", r.status, r.raw)
	}

	// EX-06 rồi AC-01: đặt mật khẩu mới thành công, đăng nhập được, không phải đổi mật khẩu lần đầu (QTPQ-Q-07).
	r = request()
	challenge, _ = r.body["challenge_id"].(string)
	_, code = sms.last(t)
	if r := reset(challenge, code, "12345", "12345"); r.status != http.StatusUnprocessableEntity || !strings.Contains(string(r.raw), "Mật khẩu tối thiểu 06 ký tự") {
		t.Fatalf("mật khẩu ngắn: %d %s", r.status, r.raw)
	}
	if r := reset(challenge, code, "matkhau1", "matkhau2"); r.status != http.StatusUnprocessableEntity || !strings.Contains(string(r.raw), "Mật khẩu nhập lại không khớp") {
		t.Fatalf("nhập lại không khớp: %d %s", r.status, r.raw)
	}
	if r := reset(challenge, code, "matkhau1", "matkhau1"); r.status != http.StatusOK || r.body["status"] != "password_reset" {
		t.Fatalf("đặt lại: %d %s", r.status, r.raw)
	}
	f.step()
	if r := f.login("tanphu", "matkhau1", ""); r.status != http.StatusOK || r.body["status"] != "authenticated" {
		t.Fatalf("đăng nhập bằng mật khẩu mới: %d %s", r.status, r.raw)
	}
	if r := reset(challenge, code, "matkhau3", "matkhau3"); r.status != http.StatusGone {
		t.Fatalf("mã dùng một lần: %d %s", r.status, r.raw)
	}

	// AC-N4, EX-05: lần gửi thứ 06 trong 24 giờ → không gửi.
	for sms.count() < 5 {
		f.clk.Advance(61 * time.Second)
		if r := request(); r.status != http.StatusOK {
			t.Fatalf("gửi mã thứ %d: %d %s", sms.count()+1, r.status, r.raw)
		}
	}
	f.clk.Advance(61 * time.Second)
	before := sms.count()
	if r := request(); r.status != http.StatusTooManyRequests || !strings.Contains(r.detail(), "05 lần trong ngày") || sms.count() != before {
		t.Fatalf("lần thứ 6: %d %s", r.status, r.raw)
	}

	// EX-01, EX-02 / AC-04.
	if r := f.post(otpRoute, map[string]any{"identifier": "khong-co-ai"}); r.status != http.StatusNotFound || !strings.Contains(r.detail(), "Không tìm thấy tài khoản") {
		t.Fatalf("không tìm thấy: %d %s", r.status, r.raw)
	}
	locked := f.mem.Seed(port.NewUser{Username: "da.khoa", Phone: "0913456223", TenantID: f.tenant, OrgRef: ids.New()}, false, "htx_member")
	_ = locked
	if r := f.post(otpRoute, map[string]any{"identifier": "da.khoa"}); r.status != http.StatusForbidden || sms.count() != before {
		t.Fatalf("tài khoản đã khóa: %d %s", r.status, r.raw)
	}
	// AC-N5: mã không vào log.
	if strings.Contains(f.logs.String(), code) {
		t.Fatal("OTP leaked into logs")
	}
}

// AC-N2: kênh SMS tắt → không tạo mã, báo liên hệ người cấp tài khoản.
func TestPasswordOTP_SMSDisabled(t *testing.T) {
	f := newNativeLoginFixture(t)
	f.mem.Seed(port.NewUser{Username: "tanphu", Phone: "0913456222", TenantID: f.tenant, OrgRef: ids.New()}, true, "htx_member")
	r := f.post(otpRoute, map[string]any{"identifier": "tanphu"})
	if r.status != http.StatusServiceUnavailable || !strings.Contains(r.detail(), "liên hệ người cấp tài khoản") {
		t.Fatalf("kênh tắt: %d %s", r.status, r.raw)
	}
	if n := f.count(`SELECT count(*) FROM pf_accounts.password_otp_challenge WHERE tenant_id = $1`, f.tenant); n != 0 {
		t.Fatalf("không được tạo mã khi kênh tắt: %d", n)
	}
}
