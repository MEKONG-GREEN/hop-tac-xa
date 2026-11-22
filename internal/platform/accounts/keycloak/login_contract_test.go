package keycloak

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/port"
)

const loginPassword = "Mat-khau-dai-12!"

// near so thời điểm với sai số 2 ms: bản HTTP làm tròn lần sai cuối tới mili giây.
func near(a, b time.Time) bool { return a.Sub(b).Abs() < 2*time.Millisecond }

// runLoginContract là hợp đồng chung của port.PasswordLogin: bản trong bộ nhớ và bản HTTP (qua emulator) phải cư xử
// giống nhau. mem là kho quan sát được; d đọc trạng thái khóa như máy chủ làm.
func runLoginContract(t *testing.T, l port.PasswordLogin, d port.Directory, mem *Memory) {
	ctx := context.Background()
	tid, org := uuid.New(), uuid.New()
	seed := func(name string, roles ...string) uuid.UUID {
		id := mem.Seed(port.NewUser{Username: name, Email: name + "@vi-du.vn", TenantID: tid, OrgRef: org}, true, roles...)
		if err := mem.SetPassword(ctx, id, loginPassword); err != nil {
			t.Fatal(err)
		}
		return id
	}
	clock := time.Now()
	mem.SetNow(func() time.Time { return clock })
	defer mem.SetNow(time.Now)

	// Đúng mật khẩu → token có roles và amr.
	idA := seed("tai-khoan-a", "coop_manager")
	ts, err := l.PasswordGrant(ctx, "tai-khoan-a", loginPassword, "")
	if err != nil || ts.AccessToken == "" || ts.RefreshToken == "" || ts.ExpiresIn <= 0 {
		t.Fatalf("đăng nhập đúng: %v", err)
	}
	if !slices.Equal(ts.Roles, []string{"coop_manager"}) || !slices.Equal(ts.AMR, []string{"pwd"}) {
		t.Fatalf("claim roles=%v amr=%v", ts.Roles, ts.AMR)
	}

	// Sai mật khẩu → ErrInvalidGrant và Keycloak tăng bộ đếm.
	if _, err := l.PasswordGrant(ctx, "tai-khoan-a", "sai-mat-khau", ""); !errors.Is(err, port.ErrInvalidGrant) {
		t.Fatalf("sai mật khẩu: %v", err)
	}
	if st, err := d.LoginLockStatus(ctx, idA); err != nil || st.Failures != 1 || st.LastFailure == nil || st.Locked {
		t.Fatalf("sau lần sai đầu: %+v %v", st, err)
	}
	// Đăng nhập đúng xóa bộ đếm.
	if _, err := l.PasswordGrant(ctx, "tai-khoan-a", loginPassword, ""); err != nil {
		t.Fatal(err)
	}
	if st, _ := d.LoginLockStatus(ctx, idA); st.Failures != 0 {
		t.Fatalf("đăng nhập đúng phải xóa bộ đếm: %+v", st)
	}
	if _, err := l.PasswordGrant(ctx, "khong-co-ai", loginPassword, ""); !errors.Is(err, port.ErrInvalidGrant) {
		t.Fatalf("tên không có: %v", err)
	}

	// OTP: thiếu hoặc sai mã → sai; đúng mã → amr có otp.
	idB := seed("tai-khoan-b", "kn_officer")
	mem.SetOTP(idB, true)
	mem.SetOTPCode(idB, "123456")
	for _, code := range []string{"", "000000"} {
		clock = clock.Add(2 * time.Second)
		if _, err := l.PasswordGrant(ctx, "tai-khoan-b", loginPassword, code); !errors.Is(err, port.ErrInvalidGrant) {
			t.Fatalf("OTP %q: %v", code, err)
		}
	}
	ts, err = l.PasswordGrant(ctx, "tai-khoan-b", loginPassword, "123456")
	if err != nil || !slices.Contains(ts.AMR, "otp") || !slices.Contains(ts.Roles, port.RoleLoginTOTP) {
		t.Fatalf("đúng OTP: %v amr=%v roles=%v", err, ts.AMR, ts.Roles)
	}
	// Thiếu OTP ở tài khoản có OTP: không có đường kiểm mật khẩu riêng, nên Keycloak tính một lần sai (không về 0).
	if st, _ := d.LoginLockStatus(ctx, idB); st.Failures != 0 {
		t.Fatalf("đăng nhập đúng (có OTP) phải xóa bộ đếm: %+v", st)
	}
	clock = clock.Add(2 * time.Second)
	if _, err := l.PasswordGrant(ctx, "tai-khoan-b", loginPassword, ""); !errors.Is(err, port.ErrInvalidGrant) {
		t.Fatalf("thiếu OTP: %v", err)
	}
	if st, _ := d.LoginLockStatus(ctx, idB); st.Failures != 1 {
		t.Fatalf("thiếu OTP phải tăng bộ đếm một lần: %+v", st)
	}

	// Required action: chỉ báo sau khi mật khẩu đúng.
	idC := mem.Seed(port.NewUser{Username: "tai-khoan-c", Email: "c@vi-du.vn", TenantID: tid, OrgRef: org,
		RequiredActions: []string{port.ActionUpdatePassword}}, true)
	if err := mem.SetPassword(ctx, idC, loginPassword); err != nil {
		t.Fatal(err)
	}
	if _, err := l.PasswordGrant(ctx, "tai-khoan-c", loginPassword, ""); !errors.Is(err, port.ErrAccountNotSetUp) {
		t.Fatalf("required action: %v", err)
	}
	if _, err := l.PasswordGrant(ctx, "tai-khoan-c", "sai-mat-khau", ""); !errors.Is(err, port.ErrInvalidGrant) {
		t.Fatalf("required action + sai mật khẩu: %v", err)
	}

	// Tài khoản bị khóa trong Keycloak.
	_ = mem.SetEnabled(ctx, idA, false)
	if _, err := l.PasswordGrant(ctx, "tai-khoan-a", loginPassword, ""); !errors.Is(err, port.ErrAccountDisabled) {
		t.Fatalf("tài khoản khóa: %v", err)
	}
	_ = mem.SetEnabled(ctx, idA, true)

	// Làm mới và thu hồi phiên.
	ts, _ = l.PasswordGrant(ctx, "tai-khoan-a", loginPassword, "")
	if again, err := l.Refresh(ctx, ts.RefreshToken); err != nil || again.AccessToken == "" {
		t.Fatalf("refresh: %v", err)
	}
	if err := l.EndSession(ctx, ts.RefreshToken); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Refresh(ctx, ts.RefreshToken); !errors.Is(err, port.ErrSessionEnded) {
		t.Fatalf("refresh sau đăng xuất: %v", err)
	}
	if err := l.EndSession(ctx, ts.RefreshToken); err != nil {
		t.Fatalf("đăng xuất lại phiên đã hết phải coi như xong: %v", err)
	}

	// Khóa tạm sau 5 lần sai (cách nhau 2 giây): trong lúc khóa không kiểm mật khẩu; Admin mở khóa thì vào được.
	idD := seed("tai-khoan-d", "coop_manager")
	for range 5 {
		clock = clock.Add(2 * time.Second)
		if _, err := l.PasswordGrant(ctx, "tai-khoan-d", "sai-mat-khau", ""); !errors.Is(err, port.ErrInvalidGrant) {
			t.Fatal(err)
		}
	}
	st, err := d.LoginLockStatus(ctx, idD)
	if err != nil || !st.Locked || st.Failures != domain.LoginMaxFailures || st.Until == nil || !near(*st.Until, clock.Add(domain.LoginLockout)) {
		t.Fatalf("khóa 15 phút: %+v %v", st, err)
	}
	grants := mem.PasswordGrants(idD)
	if _, err := l.PasswordGrant(ctx, "tai-khoan-d", loginPassword, ""); !errors.Is(err, port.ErrInvalidGrant) || mem.PasswordGrants(idD) != grants {
		t.Fatalf("đang khóa phải từ chối mà không kiểm mật khẩu: %v", err)
	}
	if err := d.ClearBruteForce(ctx, idD); err != nil {
		t.Fatal(err)
	}
	if _, err := l.PasswordGrant(ctx, "tai-khoan-d", loginPassword, ""); err != nil {
		t.Fatalf("sau khi mở khóa: %v", err)
	}

	// Khóa nhanh: hai lần sai cách nhau dưới 1 giây khóa 60 giây (không phải 15 phút).
	idE := seed("tai-khoan-e", "coop_manager")
	for range 2 {
		clock = clock.Add(100 * time.Millisecond)
		_, _ = l.PasswordGrant(ctx, "tai-khoan-e", "sai-mat-khau", "")
	}
	st, _ = d.LoginLockStatus(ctx, idE)
	if !st.Locked || st.Failures != 2 || st.Until == nil || !near(*st.Until, clock.Add(domain.LoginQuickWait)) {
		t.Fatalf("khóa nhanh 60 giây: %+v", st)
	}

	// Keycloak ghi nhận lần sai chậm một nhịp: đọc ngay thấy số cũ, sau độ trễ thấy số mới.
	idF := seed("tai-khoan-f", "coop_manager")
	mem.LagFailureVisibility(500 * time.Millisecond)
	defer mem.LagFailureVisibility(0)
	clock = clock.Add(2 * time.Second)
	_, _ = l.PasswordGrant(ctx, "tai-khoan-f", "sai-mat-khau", "")
	if st, _ := d.LoginLockStatus(ctx, idF); st.Failures != 0 {
		t.Fatalf("đọc ngay sau lần sai phải thấy số cũ, thấy %d", st.Failures)
	}
	clock = clock.Add(time.Second)
	if st, _ := d.LoginLockStatus(ctx, idF); st.Failures != 1 {
		t.Fatalf("sau độ trễ phải thấy số mới, thấy %d", st.Failures)
	}
}

func TestLoginContractMemory(t *testing.T) {
	m := NewMemory()
	runLoginContract(t, m, m, m)
}

func TestLoginContractHTTPAgainstEmulator(t *testing.T) {
	e := newEmulator(t)
	runLoginContract(t, e.login(t), e.client(t), e.mem)
}

// Lỗi không mang mật khẩu, OTP, bí mật client hay thân phản hồi (R-14).
func TestLoginHTTPErrorsCarryNoSecrets(t *testing.T) {
	const leak = "password=Mat-khau-dai-12! totp=123456 user=tai-khoan-a"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		oidcError(w, http.StatusBadRequest, "invalid_request", leak)
	}))
	t.Cleanup(srv.Close)
	cfg := Config{BaseURL: srv.URL, Realm: emuRealm, ClientID: emuClient, ClientSecret: emuSecret, HTTP: srv.Client()}
	l, err := NewLogin(cfg, LoginConfig{ClientID: emuLoginClient, ClientSecret: emuLoginSecret})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	_, err = l.PasswordGrant(ctx, "tai-khoan-a", loginPassword, "123456")
	if !errors.Is(err, port.ErrUnavailable) {
		t.Fatalf("phản hồi lạ phải là ErrUnavailable: %v", err)
	}
	for _, bad := range []string{loginPassword, "123456", "tai-khoan-a", emuLoginSecret, "invalid_request", "password="} {
		if strings.Contains(err.Error(), bad) {
			t.Fatalf("lỗi chứa %q: %v", bad, err)
		}
	}
	// Sai bí mật client → ErrUnavailable (cấu hình sai không phải sai mật khẩu của người dùng).
	e := newEmulator(t)
	bad, _ := NewLogin(Config{BaseURL: e.srv.URL, Realm: emuRealm, ClientID: emuClient, ClientSecret: emuSecret, HTTP: e.srv.Client()},
		LoginConfig{ClientID: emuLoginClient, ClientSecret: "sai-bi-mat"})
	if _, err := bad.PasswordGrant(ctx, "a", loginPassword, ""); !errors.Is(err, port.ErrUnavailable) || strings.Contains(err.Error(), "sai-bi-mat") {
		t.Fatalf("sai bí mật client: %v", err)
	}
}

func TestLoginConfigFromEnv(t *testing.T) {
	names := []string{"AG_KC_LOGIN_CLIENT_ID", "AG_KC_LOGIN_CLIENT_SECRET"}
	for _, n := range names {
		t.Setenv(n, "")
	}
	if _, ok, err := LoginConfigFromEnv(); ok || err != nil {
		t.Fatalf("không đặt biến nào → tắt: %v %v", ok, err)
	}
	t.Setenv(names[0], "agrigreen-htx-login")
	_, ok, err := LoginConfigFromEnv()
	if ok || err == nil || !strings.Contains(err.Error(), names[1]) || strings.Contains(err.Error(), names[0]) {
		t.Fatalf("đặt thiếu → lỗi nêu tên biến thiếu: %v %v", ok, err)
	}
	t.Setenv(names[1], "y")
	lc, ok, err := LoginConfigFromEnv()
	if !ok || err != nil || lc.ClientID != "agrigreen-htx-login" || lc.ClientSecret != "y" {
		t.Fatalf("đủ biến: %v %v", ok, err)
	}
}
