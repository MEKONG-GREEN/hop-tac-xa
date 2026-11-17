package handler_test

// FN-01 (BRD App HTX — Quản trị phân quyền; ADR-0060): đăng nhập tên đăng nhập/SĐT + mật khẩu ở màn riêng. Chạy với DB thật
// (bộ đếm mã định danh không tồn tại ở pf_accounts.login_probe), Keycloak trong bộ nhớ (hợp đồng đã kiểm ở keycloak/) và
// đồng hồ cố định: mỗi lượt thử cách nhau 2 giây để không dính khóa nhanh 60 giây.

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

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

const (
	loginPwd   = "Mat-khau-dai-12!"
	loginOTP   = "123456"
	loginRoute = "/v1/public/auth/login"
)

// testDir bọc Memory để đếm số lần đọc trạng thái khóa và (tùy chọn) giấu vai khỏi Directory, mô phỏng Directory cũ so với
// claim của token.
type testDir struct {
	*keycloak.Memory
	lockReads atomic.Int32
	hideRoles bool
}

func (d *testDir) LoginLockStatus(ctx context.Context, id uuid.UUID) (port.LoginLock, error) {
	d.lockReads.Add(1)
	return d.Memory.LoginLockStatus(ctx, id)
}

func (d *testDir) RealmRoles(ctx context.Context, id uuid.UUID) ([]string, error) {
	if d.hideRoles {
		return nil, nil
	}
	return d.Memory.RealmRoles(ctx, id)
}

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

type loginFixture struct {
	t      *testing.T
	env    *pgtest.Env
	clk    *clock.Fixed
	mem    *keycloak.Memory
	dir    *testDir
	srv    *httptest.Server
	logs   *syncBuf
	tenant uuid.UUID
	mod    *accounts.Module
	// otpMail nhận email mã OTP đăng nhập của Admin (ADR-0061); nối khi configured.
	otpMail *fakeMailer
}

// newLoginFixture dựng module với Keycloak trong bộ nhớ; configured = false → không nối đăng nhập (fail-closed 503).
func newLoginFixture(t *testing.T, configured bool) *loginFixture {
	env := pgtest.Get(t)
	clk := clock.NewFixed(time.Now())
	mem := keycloak.NewMemory()
	mem.SetNow(clk.Now)
	dir := &testDir{Memory: mem}
	logs := &syncBuf{}
	logger := telemetry.NewLogger("test", logs)
	unit := env.Pool(t, migrations.PF, dbadmin.AppPlatform)
	mod := accounts.New(unit, clk, accounts.Deps{Directory: dir})
	mod.UsePasswordReset(&fakeMailer{}, resetBase, logger) // đặt logger của module
	otpMail := &fakeMailer{}
	if configured {
		mod.UseLogin(mem, bytes.Repeat([]byte{7}, 32))
		mod.UseLoginOTPMailer(otpMail)
	}
	eng := httpx.NewEngine(logger, 1<<20)
	mod.RegisterPublic(eng.Group("/v1/public"))
	srv := httptest.NewServer(eng)
	t.Cleanup(srv.Close)
	return &loginFixture{t: t, env: env, clk: clk, mem: mem, dir: dir, srv: srv, logs: logs, tenant: ids.New(), mod: mod, otpMail: otpMail}
}

func (f *loginFixture) account(username, phone string, roles ...string) uuid.UUID {
	f.t.Helper()
	if len(roles) == 0 {
		roles = []string{"coop_manager"}
	}
	id := f.mem.Seed(port.NewUser{Username: username, Phone: phone, TenantID: f.tenant, OrgRef: ids.New()}, true, roles...)
	if err := f.mem.SetPassword(context.Background(), id, loginPwd); err != nil {
		f.t.Fatal(err)
	}
	return id
}

type reply struct {
	status int
	header http.Header
	raw    []byte
	body   map[string]any
}

func (f *loginFixture) post(path string, body any) reply {
	f.t.Helper()
	b, _ := json.Marshal(body)
	res, err := http.Post(f.srv.URL+path, "application/json", bytes.NewReader(b))
	if err != nil {
		f.t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	raw, _ := io.ReadAll(res.Body)
	r := reply{status: res.StatusCode, header: res.Header, raw: raw}
	_ = json.Unmarshal(raw, &r.body)
	return r
}

func (f *loginFixture) login(identifier, password, otp string) reply {
	f.t.Helper()
	return f.post(loginRoute, map[string]any{"identifier": identifier, "password": password, "otp": otp})
}

// step trôi 2 giây giữa hai lượt thử (tránh khóa nhanh 60 giây, vẫn trong cửa sổ đếm).
func (f *loginFixture) step() { f.clk.Advance(2 * time.Second) }

func (r reply) code() string { s, _ := r.body["code"].(string); return s }

func (r reply) num(key string) int { v, _ := r.body[key].(float64); return int(v) }

func (r reply) detail() string { s, _ := r.body["detail"].(string); return s }

// comparable là phản hồi lỗi bỏ trace_id để so hai lượt.
func (r reply) comparable() map[string]any {
	m := maps.Clone(r.body)
	delete(m, "trace_id")
	return m
}

func jwtClaims(t *testing.T, token string) map[string]any {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("không phải JWT: %q", token)
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return m
}

func (f *loginFixture) count(sql string, args ...any) int {
	f.t.Helper()
	var n int
	if err := f.env.AdminPool(f.t, migrations.PF).QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n
}

func TestLogin_FN01_AC01_DungMatKhauVaoDuoc(t *testing.T) {
	f := newLoginFixture(t, true)
	f.account("nguoi.dung.a", "")
	before := f.count(`SELECT count(*) FROM shared.idempotency_key`)
	r := f.login("nguoi.dung.a", loginPwd, "")
	if r.status != http.StatusOK || r.body["status"] != "authenticated" || r.body["token_type"] != "Bearer" {
		t.Fatalf("đăng nhập đúng: %d %s", r.status, r.raw)
	}
	at, _ := r.body["access_token"].(string)
	rt, _ := r.body["refresh_token"].(string)
	if at == "" || rt == "" || r.num("expires_in") <= 0 {
		t.Fatalf("thiếu token: %s", r.raw)
	}
	if roles, _ := jwtClaims(t, at)["realm_access"].(map[string]any); roles["roles"] == nil {
		t.Fatalf("token phải mang vai: %v", jwtClaims(t, at))
	}
	if r.header.Get("Cache-Control") != "no-store" || r.header.Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("phản hồi mang token phải no-store: %v", r.header)
	}
	if after := f.count(`SELECT count(*) FROM shared.idempotency_key`); after != before {
		t.Fatalf("đăng nhập không được vào kho idempotency (%d → %d)", before, after)
	}
}

func TestLogin_FN01_AC09_TenDangNhapHoacSDT(t *testing.T) {
	f := newLoginFixture(t, true)
	f.account("nguyen.van.a", "0912345678")
	for _, id := range []string{"nguyen.van.a", "  Nguyen.Van.A ", "0912345678", "+84 912 345 678", "091-234-5678"} {
		f.step()
		if r := f.login(id, loginPwd, ""); r.status != http.StatusOK || r.body["status"] != "authenticated" {
			t.Fatalf("đăng nhập bằng %q: %d %s", id, r.status, r.raw)
		}
	}
	// SĐT khớp hai tài khoản: không đoán — coi như sai tài khoản hoặc mật khẩu, ghi cảnh báo không kèm SĐT.
	f.account("trung.sdt.1", "0987654321")
	f.account("trung.sdt.2", "0987654321")
	f.step()
	r := f.login("0987654321", loginPwd, "")
	if r.status != http.StatusUnauthorized || r.code() != "accounts.login_failed" || r.num("remaining_attempts") != 4 {
		t.Fatalf("SĐT trùng: %d %s", r.status, r.raw)
	}
	logs := f.logs.String()
	if !strings.Contains(logs, `"outcome":"ambiguous_phone"`) || !strings.Contains(logs, `"matches":2`) {
		t.Fatalf("thiếu cảnh báo vận hành về SĐT trùng:\n%s", logs)
	}
	for _, digits := range []string{"0987654321", "987654321", "0912345678", "912345678"} {
		if strings.Contains(logs, digits) {
			t.Fatalf("log chứa số điện thoại %q", digits)
		}
	}
	// Tên đăng nhập khớp đúng thắng SĐT: tài khoản có tên là chuỗi SĐT được chọn trước.
	f.account("0911222333", "0900000001")
	f.step()
	if r := f.login("0911222333", loginPwd, ""); r.status != http.StatusOK {
		t.Fatalf("tên đăng nhập khớp đúng phải được ưu tiên: %d %s", r.status, r.raw)
	}
}

func TestLogin_FN01_AC02_SaiMatKhau(t *testing.T) {
	f := newLoginFixture(t, true)
	f.account("nguoi.dung.b", "")
	r := f.login("nguoi.dung.b", "sai-mat-khau", "")
	if r.status != http.StatusUnauthorized || r.code() != "accounts.login_failed" || r.num("remaining_attempts") != 4 ||
		r.detail() != "Sai tài khoản hoặc mật khẩu. Còn 4 lần thử." || r.body["access_token"] != nil {
		t.Fatalf("EX-02: %d %s", r.status, r.raw)
	}
}

func TestLogin_FN01_AC08_Lan4ConMotLan_Lan5Khoa15Phut(t *testing.T) {
	f := newLoginFixture(t, true)
	f.account("nguoi.dung.c", "")
	f.mem.LagFailureVisibility(500 * time.Millisecond) // Keycloak ghi nhận lần sai chậm một nhịp
	for i, want := range []int{4, 3, 2, 1} {
		f.step()
		reads := f.dir.lockReads.Load()
		r := f.login("nguoi.dung.c", "sai-mat-khau", "")
		if r.status != http.StatusUnauthorized || r.num("remaining_attempts") != want {
			t.Fatalf("lần sai %d: %d %s", i+1, r.status, r.raw)
		}
		if got := f.dir.lockReads.Load() - reads; got != 1 {
			t.Fatalf("lần sai %d: phải đọc trạng thái khóa đúng một lần, đọc %d", i+1, got)
		}
		if want == 1 && r.detail() != "Sai tài khoản hoặc mật khẩu. Còn 1 lần thử." {
			t.Fatalf("lần 4 phải hiện Còn 1 lần thử: %q", r.detail())
		}
	}
	f.step()
	r := f.login("nguoi.dung.c", "sai-mat-khau", "")
	if r.status != http.StatusLocked || r.code() != "accounts.login_temporarily_locked" || r.num("retry_after_minutes") != 15 ||
		r.detail() != "Tài khoản tạm khóa đăng nhập do nhập sai nhiều lần. Thử lại sau 15 phút hoặc dùng Quên mật khẩu?" ||
		r.header.Get("Retry-After") != "900" {
		t.Fatalf("lần 5 phải khóa 15 phút: %d %s %v", r.status, r.raw, r.header)
	}
}

func TestLogin_ACN3_KhoaTamRoiHet15Phut(t *testing.T) {
	f := newLoginFixture(t, true)
	id := f.account("nguoi.dung.d", "")
	for range 5 {
		f.step()
		f.login("nguoi.dung.d", "sai-mat-khau", "")
	}
	f.clk.Advance(14*time.Minute - 2*time.Second)
	grants := f.mem.PasswordGrants(id)
	r := f.login("nguoi.dung.d", loginPwd, "")
	if r.status != http.StatusLocked || r.code() != "accounts.login_temporarily_locked" {
		t.Fatalf("đúng mật khẩu ở phút 14 vẫn bị chặn: %d %s", r.status, r.raw)
	}
	if f.mem.PasswordGrants(id) != grants {
		t.Fatal("đang khóa tạm thì không được kiểm mật khẩu")
	}
	f.clk.Advance(time.Minute + 3*time.Second)
	if r := f.login("nguoi.dung.d", loginPwd, ""); r.status != http.StatusOK {
		t.Fatalf("sau 15 phút nhập đúng phải vào được: %d %s", r.status, r.raw)
	}
	f.step()
	if r := f.login("nguoi.dung.d", "sai-mat-khau", ""); r.num("remaining_attempts") != 4 {
		t.Fatalf("đăng nhập đúng xóa bộ đếm, lần sai kế tiếp còn 4: %s", r.raw)
	}
}

func TestLogin_FN01_AC03_TaiKhoanDaKhoa(t *testing.T) {
	f := newLoginFixture(t, true)
	id := f.account("nguoi.dung.e", "")
	if err := f.mem.SetEnabled(context.Background(), id, false); err != nil {
		t.Fatal(err)
	}
	for _, pw := range []string{loginPwd, "sai-mat-khau"} {
		f.step()
		r := f.login("nguoi.dung.e", pw, "")
		if r.status != http.StatusForbidden || r.code() != "accounts.account_locked" ||
			r.detail() != "Tài khoản đã bị khóa. Liên hệ người đã cấp tài khoản để mở khóa." || r.body["access_token"] != nil {
			t.Fatalf("EX-03 (mật khẩu %q): %d %s", pw, r.status, r.raw)
		}
	}
	_ = f.mem.SetEnabled(context.Background(), id, true)
	f.step()
	if r := f.login("nguoi.dung.e", loginPwd, ""); r.status != http.StatusOK {
		t.Fatalf("mở khóa rồi đăng nhập lại được: %d %s", r.status, r.raw)
	}
}

func TestLogin_AdminMoKhoaThiDangNhapDuoc(t *testing.T) {
	f := newLoginFixture(t, true)
	id := f.account("nguoi.dung.f", "")
	for range 5 {
		f.step()
		f.login("nguoi.dung.f", "sai-mat-khau", "")
	}
	if r := f.login("nguoi.dung.f", loginPwd, ""); r.status != http.StatusLocked {
		t.Fatalf("đang khóa tạm: %d", r.status)
	}
	if err := f.mem.ClearBruteForce(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if r := f.login("nguoi.dung.f", loginPwd, ""); r.status != http.StatusOK {
		t.Fatalf("Admin mở khóa thì đăng nhập được: %d %s", r.status, r.raw)
	}
}

// Ràng buộc 4: mã định danh không tồn tại phải cho kết quả giống hệt mã thật nhưng sai mật khẩu — cùng thông báo, cùng
// cách đếm, cùng cách khóa — và bảng bộ đếm không chứa mã ở dạng đọc được.
func TestLogin_MaDinhDanhKhongTonTaiGiongSaiMatKhau(t *testing.T) {
	f := newLoginFixture(t, true)
	f.account("tai.khoan.that", "0933444555")
	const ghost = "khong.ton.tai.xyz"
	for i := 1; i <= 7; i++ {
		f.clk.Advance(12 * time.Second) // hai lượt mỗi vòng: nhường giới hạn tần suất theo IP (10 lượt/phút) nạp lại đủ
		real := f.login("tai.khoan.that", "sai-mat-khau", "")
		fake := f.login(ghost, "sai-mat-khau", "")
		if real.status != fake.status || !reflect.DeepEqual(real.comparable(), fake.comparable()) || real.header.Get("Retry-After") != fake.header.Get("Retry-After") {
			t.Fatalf("lượt %d khác nhau:\n thật: %d %s\n giả:  %d %s", i, real.status, real.raw, fake.status, fake.raw)
		}
		if i == 6 && fake.status != http.StatusLocked {
			t.Fatalf("lượt 6 phải là khóa tạm: %d", fake.status)
		}
	}
	// SĐT không tồn tại cũng vậy, và khóa theo mã đã chuẩn hóa (+84 và 0 là một).
	f.clk.Advance(12 * time.Second)
	a := f.login("0900111222", "sai-mat-khau", "")
	f.clk.Advance(12 * time.Second)
	b := f.login("+84 900 111 222", "sai-mat-khau", "")
	if a.num("remaining_attempts") != 4 || b.num("remaining_attempts") != 3 {
		t.Fatalf("SĐT chuẩn hóa dùng chung bộ đếm: %s / %s", a.raw, b.raw)
	}
	// Bảng chỉ có key_hash 32 byte; không cột nào chứa chuỗi đã nhập.
	if n := f.count(`SELECT count(*) FROM information_schema.columns WHERE table_schema = 'pf_accounts' AND table_name = 'login_probe'
	  AND column_name NOT IN ('key_hash', 'failures', 'last_failure_at', 'locked_until', 'recorded_at')`); n != 0 {
		t.Fatalf("login_probe có cột lạ: %d", n)
	}
	if n := f.count(`SELECT count(*) FROM pf_accounts.login_probe WHERE octet_length(key_hash) <> 32`); n != 0 {
		t.Fatalf("key_hash phải 32 byte: %d dòng sai", n)
	}
	for _, s := range []string{ghost, "0900111222", "900111222"} {
		if n := f.count(`SELECT count(*) FROM pf_accounts.login_probe WHERE position(convert_to($1, 'UTF8') in key_hash) > 0
		  OR $1 = ANY (ARRAY[failures::text, last_failure_at::text, locked_until::text])`, s); n != 0 {
			t.Fatalf("bảng bộ đếm chứa %q ở dạng đọc được", s)
		}
	}
}

func TestLogin_ACN1_ThieuTruong(t *testing.T) {
	f := newLoginFixture(t, true)
	reads := f.dir.lockReads.Load()
	cases := []struct {
		identifier, password string
		fields               map[string]string
	}{
		{"", "x", map[string]string{"identifier": "Nhập tên đăng nhập hoặc số điện thoại"}},
		{"   ", "x", map[string]string{"identifier": "Nhập tên đăng nhập hoặc số điện thoại"}},
		{"a", "", map[string]string{"password": "Nhập mật khẩu"}},
		{"", "", map[string]string{"identifier": "Nhập tên đăng nhập hoặc số điện thoại", "password": "Nhập mật khẩu"}},
	}
	for _, tc := range cases {
		r := f.login(tc.identifier, tc.password, "")
		if r.status != http.StatusUnprocessableEntity || r.code() != "validation_failed" {
			t.Fatalf("%q/%q: %d %s", tc.identifier, tc.password, r.status, r.raw)
		}
		got := map[string]string{}
		errs, _ := r.body["errors"].([]any)
		for _, e := range errs {
			m, _ := e.(map[string]any)
			got[m["field"].(string)] = m["reason"].(string)
		}
		if !maps.Equal(got, tc.fields) {
			t.Fatalf("%q/%q: lỗi theo trường %v, muốn %v", tc.identifier, tc.password, got, tc.fields)
		}
	}
	if r := f.login("a", "x", "12ab"); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("OTP sai dạng: %d", r.status)
	}
	if f.dir.lockReads.Load() != reads {
		t.Fatal("thiếu trường thì không được chạm Keycloak")
	}
}

func (f *loginFixture) failures(id uuid.UUID) int {
	f.t.Helper()
	st, err := f.mem.LoginLockStatus(context.Background(), id)
	if err != nil {
		f.t.Fatal(err)
	}
	return st.Failures
}

// Ràng buộc 2: không có đường nào vào được bằng mật khẩu đơn thuần khi tài khoản có OTP hoặc vai bắt TOTP.
func TestLogin_MFA_KhongVaoBangMatKhauDonThuan(t *testing.T) {
	f := newLoginFixture(t, true)
	id := f.account("co.otp", "", "coop_manager")
	f.mem.SetOTP(id, true)
	f.mem.SetOTPCode(id, loginOTP)

	// Có OTP, chỉ gửi mật khẩu đúng → sai tài khoản hoặc mật khẩu, không token, không phiên, Keycloak tăng đếm đúng một.
	f.step()
	r := f.login("co.otp", loginPwd, "")
	if r.status != http.StatusUnauthorized || r.code() != "accounts.login_failed" || r.num("remaining_attempts") != 4 ||
		r.body["access_token"] != nil || r.body["refresh_token"] != nil {
		t.Fatalf("thiếu OTP: %d %s", r.status, r.raw)
	}
	if n := f.mem.ActiveLoginSessions(id); n != 0 {
		t.Fatalf("không được để lại phiên, còn %d", n)
	}
	if n := f.failures(id); n != 1 {
		t.Fatalf("Keycloak phải ghi một lần sai, ghi %d", n)
	}
	// Sai OTP → 401, không token.
	f.step()
	if r := f.login("co.otp", loginPwd, "000000"); r.status != http.StatusUnauthorized || r.code() != "accounts.login_failed" || r.body["access_token"] != nil {
		t.Fatalf("sai OTP: %d %s", r.status, r.raw)
	}
	// Đúng OTP → token có amr otp.
	f.step()
	r = f.login("co.otp", loginPwd, loginOTP)
	if r.status != http.StatusOK || r.body["status"] != "authenticated" {
		t.Fatalf("đúng OTP: %d %s", r.status, r.raw)
	}
	if amr, _ := jwtClaims(t, r.body["access_token"].(string))["amr"].([]any); len(amr) != 2 || amr[1] != "otp" {
		t.Fatalf("token phải có amr otp: %v", amr)
	}

	// Vai bắt TOTP (registry_admin) chưa có OTP và không còn required action → không bao giờ phát token.
	admin := f.account("quan.tri", "", "registry_admin")
	f.step()
	r = f.login("quan.tri", loginPwd, "")
	if r.status != http.StatusForbidden || r.code() != "accounts.login_setup_required" || r.body["access_token"] != nil {
		t.Fatalf("vai bắt TOTP chưa có OTP: %d %s", r.status, r.raw)
	}
	if n := f.mem.ActiveLoginSessions(admin); n != 0 {
		t.Fatalf("phiên bị từ chối phải được thu hồi, còn %d", n)
	}
	f.step()
	if r := f.login("quan.tri", "sai-mat-khau", ""); r.status != http.StatusUnauthorized {
		t.Fatalf("sai mật khẩu của vai bắt TOTP: %d", r.status)
	}
}

// Mật khẩu đúng kèm OTP sai 5 lần → lần 5 khóa 15 phút. Xen các lượt chỉ gửi mật khẩu đúng không làm bộ đếm về 0 (lượt
// đó cũng là một lần sai); chỉ đăng nhập trọn vẹn mới xóa bộ đếm.
func TestLogin_MFA_SaiOTP5LanThiKhoa(t *testing.T) {
	f := newLoginFixture(t, true)
	id := f.account("co.otp.khoa", "", "coop_manager")
	f.mem.SetOTP(id, true)
	f.mem.SetOTPCode(id, loginOTP)
	otps := []string{"000000", "", "000000", "", "000000"} // lượt 2 và 4 chỉ gửi mật khẩu đúng
	for i, otp := range otps {
		f.clk.Advance(12 * time.Second)
		r := f.login("co.otp.khoa", loginPwd, otp)
		if i < 4 {
			if r.status != http.StatusUnauthorized || r.num("remaining_attempts") != 4-i {
				t.Fatalf("lượt %d: %d %s", i+1, r.status, r.raw)
			}
			if n := f.failures(id); n != i+1 {
				t.Fatalf("lượt %d: bộ đếm Keycloak phải là %d, là %d (không được về 0)", i+1, i+1, n)
			}
			continue
		}
		if r.status != http.StatusLocked || r.num("retry_after_minutes") != 15 || r.header.Get("Retry-After") != "900" {
			t.Fatalf("lần sai thứ 5 phải khóa 15 phút: %d %s", r.status, r.raw)
		}
	}
	// Đang khóa: dù mật khẩu và OTP đúng vẫn bị chặn và không kiểm mật khẩu.
	grants := f.mem.PasswordGrants(id)
	f.clk.Advance(12 * time.Second)
	if r := f.login("co.otp.khoa", loginPwd, loginOTP); r.status != http.StatusLocked || r.body["access_token"] != nil || f.mem.PasswordGrants(id) != grants {
		t.Fatalf("đang khóa tạm: %d %s", r.status, r.raw)
	}
	// Hết 15 phút, đăng nhập trọn vẹn thành công mới xóa bộ đếm.
	f.clk.Advance(15 * time.Minute)
	if r := f.login("co.otp.khoa", loginPwd, loginOTP); r.status != http.StatusOK {
		t.Fatalf("sau 15 phút: %d %s", r.status, r.raw)
	}
	if n := f.failures(id); n != 0 {
		t.Fatalf("đăng nhập trọn vẹn phải xóa bộ đếm, còn %d", n)
	}
}

// Phản hồi của tài khoản có OTP thiếu mã, tài khoản không OTP sai mật khẩu và mã định danh không tồn tại giống hệt nhau
// ở mọi lượt, kể cả lúc khóa tạm: không lộ tài khoản nào có OTP.
func TestLogin_MFA_PhanHoiGiongHetKhiThieuOTP(t *testing.T) {
	f := newLoginFixture(t, true)
	withOTP := f.account("co.otp.a", "", "coop_manager")
	f.mem.SetOTP(withOTP, true)
	f.mem.SetOTPCode(withOTP, loginOTP)
	f.account("khong.otp.b", "", "coop_manager")
	for i := 1; i <= 7; i++ {
		f.clk.Advance(20 * time.Second)
		a := f.login("co.otp.a", loginPwd, "")
		b := f.login("khong.otp.b", "sai-mat-khau", "")
		c := f.login("khong.ton.tai.c", "sai-mat-khau", "")
		for name, r := range map[string]reply{"không OTP sai mật khẩu": b, "không tồn tại": c} {
			if a.status != r.status || !reflect.DeepEqual(a.comparable(), r.comparable()) || a.header.Get("Retry-After") != r.header.Get("Retry-After") {
				t.Fatalf("lượt %d: OTP thiếu mã khác %s: %d %s | %d %s", i, name, a.status, a.raw, r.status, r.raw)
			}
		}
		if i == 6 && a.status != http.StatusLocked {
			t.Fatalf("lượt 6 phải là khóa tạm: %d", a.status)
		}
	}
}

// Kiểm lại claim: Directory không thấy vai bắt TOTP nhưng token mang vai đó mà thiếu amr otp → thu hồi, không trả token.
func TestLogin_MFA_KiemLaiClaimCuaToken(t *testing.T) {
	f := newLoginFixture(t, true)
	id := f.account("quan.tri.2", "", "registry_admin")
	f.dir.hideRoles = true
	r := f.login("quan.tri.2", loginPwd, "")
	if r.status != http.StatusForbidden || r.code() != "accounts.login_setup_required" || r.body["access_token"] != nil {
		t.Fatalf("kiểm lại claim: %d %s", r.status, r.raw)
	}
	if n := f.mem.ActiveLoginSessions(id); n != 0 {
		t.Fatalf("phải thu hồi phiên vừa mở, còn %d", n)
	}
}

func TestLogin_ViecLanDau_TraMaRieng(t *testing.T) {
	f := newLoginFixture(t, true)
	id := f.account("moi.tao", "")
	if err := f.mem.AddRequiredActions(context.Background(), id, []string{port.ActionUpdatePassword}); err != nil {
		t.Fatal(err)
	}
	r := f.login("moi.tao", loginPwd, "")
	if r.status != http.StatusForbidden || r.code() != "accounts.login_setup_required" || r.body["access_token"] != nil {
		t.Fatalf("còn việc lần đầu: %d %s", r.status, r.raw)
	}
	f.step()
	if r := f.login("moi.tao", "sai-mat-khau", ""); r.status != http.StatusUnauthorized || r.code() != "accounts.login_failed" {
		t.Fatalf("sai mật khẩu: %d %s", r.status, r.raw)
	}
}

func TestLogin_GioiHanTheoIP(t *testing.T) {
	f := newLoginFixture(t, true)
	for i := 1; i <= 10; i++ {
		if r := f.login("khong.ton.tai", "x", ""); r.status == http.StatusTooManyRequests {
			t.Fatalf("lần %d chưa được giới hạn", i)
		}
	}
	r := f.login("khong.ton.tai", "x", "")
	if r.status != http.StatusTooManyRequests || r.header.Get("Retry-After") == "" {
		t.Fatalf("lần thứ 11 liên tiếp phải 429: %d %s", r.status, r.raw)
	}
}

func TestLogin_ChuaCauHinh_503(t *testing.T) {
	f := newLoginFixture(t, false)
	f.account("nguoi.dung.g", "")
	for _, path := range []string{loginRoute, "/v1/public/auth/refresh", "/v1/public/auth/logout"} {
		body := map[string]any{"identifier": "nguoi.dung.g", "password": loginPwd, "refresh_token": "x"}
		if r := f.post(path, body); r.status != http.StatusServiceUnavailable || r.code() != "accounts.login_unavailable" {
			t.Fatalf("%s thiếu cấu hình phải 503 login_unavailable: %d %s", path, r.status, r.raw)
		}
	}
}

func TestLogin_RefreshVaDangXuatThuHoiPhien(t *testing.T) {
	f := newLoginFixture(t, true)
	id := f.account("nguoi.dung.h", "")
	rt := f.login("nguoi.dung.h", loginPwd, "").body["refresh_token"].(string)
	if r := f.post("/v1/public/auth/refresh", map[string]any{"refresh_token": rt}); r.status != http.StatusOK || r.body["access_token"] == nil {
		t.Fatalf("refresh: %d %s", r.status, r.raw)
	}
	if r := f.post("/v1/public/auth/logout", map[string]any{"refresh_token": rt}); r.status != http.StatusNoContent {
		t.Fatalf("logout: %d %s", r.status, r.raw)
	}
	if n := f.mem.ActiveLoginSessions(id); n != 0 {
		t.Fatalf("đăng xuất phải thu hồi phiên ở Keycloak, còn %d", n)
	}
	if r := f.post("/v1/public/auth/refresh", map[string]any{"refresh_token": rt}); r.status != http.StatusUnauthorized || r.code() != "accounts.session_expired" {
		t.Fatalf("refresh sau đăng xuất: %d %s", r.status, r.raw)
	}
	if r := f.post("/v1/public/auth/logout", map[string]any{"refresh_token": rt}); r.status != http.StatusNoContent {
		t.Fatalf("đăng xuất lại phiên đã hết: %d", r.status)
	}
	if r := f.post("/v1/public/auth/refresh", map[string]any{"refresh_token": ""}); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("thiếu refresh_token: %d", r.status)
	}
}

// Ràng buộc 3: mật khẩu, OTP, mã định danh, SĐT và token không xuất hiện trong log hay thân lỗi.
func TestLogin_KhongLoMatKhauTrongLogVaLoi(t *testing.T) {
	f := newLoginFixture(t, true)
	const secretPwd, secretOTP, secretName, secretPhone = "Bi-mat-cuc-ky-7!", "654321", "ten.bi.mat.zz", "0966777888"
	id := f.account(secretName, secretPhone, "coop_manager")
	f.mem.SetOTP(id, true)
	f.mem.SetOTPCode(id, secretOTP)
	if err := f.mem.SetPassword(context.Background(), id, secretPwd); err != nil {
		t.Fatal(err)
	}
	var bodies []string
	try := func(identifier, password, otp string) reply {
		f.step()
		r := f.login(identifier, password, otp)
		if r.status != http.StatusOK {
			bodies = append(bodies, string(r.raw))
		}
		return r
	}
	try(secretName, secretPwd, "")               // thiếu OTP
	try(secretName, "sai-"+secretPwd, "")        // sai mật khẩu
	try(secretName, secretPwd, "000000")         // sai OTP
	try("ten.khac.tt"+secretName, "x", "")       // mã không tồn tại
	try(secretPhone, "sai-"+secretPwd, "")       // SĐT
	try("", secretPwd, secretOTP)                // thiếu trường
	ok := try(secretPhone, secretPwd, secretOTP) // thành công
	if ok.status != http.StatusOK {
		t.Fatalf("đăng nhập đúng: %d %s", ok.status, ok.raw)
	}
	logs := f.logs.String()
	forbidden := []string{secretPwd, secretOTP, secretName, secretPhone, strings.TrimPrefix(secretPhone, "0"),
		ok.body["access_token"].(string), ok.body["refresh_token"].(string)}
	for _, s := range forbidden {
		if strings.Contains(logs, s) {
			t.Fatalf("log chứa %q", s)
		}
		for _, b := range bodies {
			if strings.Contains(b, s) {
				t.Fatalf("thân lỗi chứa %q: %s", s, b)
			}
		}
	}
	if !strings.Contains(logs, `"event":"accounts.login"`) {
		t.Fatalf("thiếu dòng nhật ký đăng nhập:\n%s", logs)
	}
}
