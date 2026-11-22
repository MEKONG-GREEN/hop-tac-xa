package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/clock"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/httpx"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/hop-tac-xa/migrations"
)

// appCaller gọi Application API của module accounts bằng token của subject (tài khoản có thật trong Keycloak giả).
type appCaller struct {
	t     *testing.T
	url   string
	token string
}

func (c appCaller) do(method, path string, body any) resp {
	c.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.url+path, rd)
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	if method == http.MethodPost {
		req.Header.Set("Idempotency-Key", key())
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	raw, _ := io.ReadAll(res.Body)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return resp{status: res.StatusCode, hdr: res.Header, body: m}
}

func appServer(t *testing.T, f *fixture) string {
	t.Helper()
	eng := httpx.NewEngine(telemetry.NewLogger("test", io.Discard), 1<<20)
	app := eng.Group("/v1", httpx.Authenticate(f.iss.Verifier()))
	app.Use(f.mod.Middleware())
	app.Use(idempotency.Middleware(f.env.Pool(t, migrations.PF, "platform_api")))
	f.mod.RegisterApp(app)
	srv := httptest.NewServer(eng)
	t.Cleanup(srv.Close)
	return srv.URL
}

// AH-281 (FN-10/AC-05, BR-03, AC-N1..AC-N5): đặt lại mật khẩu của Admin/Đại diện HTX và tự đổi mật khẩu.
func TestFN10ResetByManagersAndSelfChange(t *testing.T) {
	f := newFixtureAt(t, clock.System{}, true)
	f.dir.PasswordMinLen = 6
	ctx := context.Background()
	url := appServer(t, f)
	admin := f.as(f.tenant, uuid.Nil, htxroles.Admin)
	create := func(username, phone string, coop uuid.UUID, roles ...string) (uuid.UUID, string) {
		t.Helper()
		body := map[string]any{"username": username, "full_name": "Người " + username, "phone": phone, "roles": roles}
		if coop != uuid.Nil {
			body["cooperative_ref"] = coop.String()
		} else {
			body["email"] = username + "@example.vn"
		}
		r := admin.do(http.MethodPost, "/v1/accounts/htx-users", body)
		if r.status != http.StatusCreated {
			t.Fatalf("create %s: %d %v", username, r.status, r.body)
		}
		return uuid.MustParse(str(r.body, "user", "id")), str(r.body, "temporary_password")
	}
	rep, _ := create("fn10-rep", "0912200001", f.coop30292, htxroles.Representative)
	member, memberPw := create("fn10-member", "0912200002", f.coop30292, htxroles.Member)
	other, _ := create("fn10-other", "0912200003", f.coop30301, htxroles.Member)
	adminAcc, _ := create("fn10-admin", "0912200004", uuid.Nil, htxroles.Admin)

	// AC-N1: kênh SMS tắt → Admin cấp mật khẩu tạm 08 ký tự đúng một lần; mật khẩu cũ hết hiệu lực; phiên bị thu hồi.
	before := f.dir.Logouts(member)
	const op = "fn10-admin-reset-member"
	b, _ := json.Marshal(map[string]any{})
	r := admin.raw(http.MethodPost, "/v1/accounts/htx-users/"+member.String()+"/password-reset", op, "application/json", b)
	temp := str(r.body, "temporary_password")
	if r.status != http.StatusOK || r.body["mode"] != "temporary_password" || len(temp) != 8 {
		t.Fatalf("admin reset: %d %v", r.status, r.body)
	}
	u, _ := f.dir.GetUser(ctx, member)
	if !f.dir.PasswordMatches(member, temp) || f.dir.PasswordMatches(member, memberPw) || !slices.Contains(u.RequiredActions, port.ActionUpdatePassword) || f.dir.Logouts(member) <= before {
		t.Fatal("temporary password must replace the old one, require a change and revoke sessions")
	}
	if r := admin.raw(http.MethodPost, "/v1/accounts/htx-users/"+member.String()+"/password-reset", op, "application/json", b); r.status != http.StatusOK || r.body["temporary_password"] != nil {
		t.Fatalf("replay must not return the password: %d %v", r.status, r.body)
	}
	var cached []byte
	if err := f.env.AdminPool(t, migrations.PF).QueryRow(ctx, `SELECT response_body FROM shared.idempotency_key WHERE tenant_id=$1 AND idem_key=$2`, f.tenant, op).Scan(&cached); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(cached, []byte(temp)) {
		t.Fatal("temporary password persisted in replay cache")
	}
	// Không cho chính mình; tài khoản đã khóa không được cấp.
	self := f.as(f.tenant, uuid.Nil, htxroles.Admin)
	if r := self.do(http.MethodPost, "/v1/accounts/htx-users/"+self.actor.String()+"/password-reset", nil); r.status != http.StatusForbidden {
		t.Fatalf("self reset: %d %v", r.status, r.body)
	}
	if r := admin.do(http.MethodPost, "/v1/accounts/htx-users/"+other.String()+"/lock", nil); r.status != http.StatusOK {
		t.Fatalf("lock other: %d %v", r.status, r.body)
	}
	if r := admin.do(http.MethodPost, "/v1/accounts/htx-users/"+other.String()+"/password-reset", nil); r.status != http.StatusConflict {
		t.Fatalf("locked account reset: %d %v", r.status, r.body)
	}

	// AC-05, AC-N2: Đại diện HTX chỉ đặt lại cho Thành viên HTX của HTX mình.
	repCaller := appCaller{t: t, url: url, token: f.iss.Sign(t, authztest.Token{Subject: rep, TenantID: f.tenant, Roles: []string{htxroles.Representative}, OrgRef: f.coop30292, MFA: true})}
	if r := repCaller.do(http.MethodPost, "/v1/htx-accounts/"+member.String()+"/password-reset", nil); r.status != http.StatusOK || r.body["mode"] != "temporary_password" {
		t.Fatalf("rep reset member: %d %v", r.status, r.body)
	}
	for _, id := range []uuid.UUID{adminAcc, other} {
		if r := repCaller.do(http.MethodPost, "/v1/htx-accounts/"+id.String()+"/password-reset", nil); r.status != http.StatusNotFound {
			t.Fatalf("rep reset out of scope %s: %d %v", id, r.status, r.body)
		}
	}
	if r := repCaller.do(http.MethodPost, "/v1/htx-accounts/"+rep.String()+"/password-reset", nil); r.status != http.StatusForbidden {
		t.Fatalf("rep reset self: %d %v", r.status, r.body)
	}

	// AC-N3: kênh SMS bật → gửi mã OTP, không sinh mật khẩu tạm.
	sms := &fakeSMS{}
	f.mod.UseSMS(sms)
	f.mod.UseLogin(f.dir, bytes.Repeat([]byte{9}, 32))
	r = admin.do(http.MethodPost, "/v1/accounts/htx-users/"+member.String()+"/password-reset", nil)
	if r.status != http.StatusOK || r.body["mode"] != "otp_sent" || r.body["temporary_password"] != nil || r.body["phone_hint"] != "0912***002" || sms.count() != 1 {
		t.Fatalf("reset with SMS on: %d %v", r.status, r.body)
	}

	// AC-N4, AC-N5: tự đổi mật khẩu.
	f.mod.UseSMS(nil)
	r = repCaller.do(http.MethodPost, "/v1/htx-accounts/"+member.String()+"/password-reset", nil)
	current := str(r.body, "temporary_password")
	if current == "" {
		t.Fatalf("temporary password for self-change: %d %v", r.status, r.body)
	}
	me := appCaller{t: t, url: url, token: f.iss.Sign(t, authztest.Token{Subject: member, TenantID: f.tenant, Roles: []string{htxroles.Member}, OrgRef: f.coop30292, MFA: true})}
	change := func(cur, next, confirm string) resp {
		t.Helper()
		return me.do(http.MethodPut, "/v1/me/password", map[string]any{"current_password": cur, "new_password": next, "confirm_password": confirm})
	}
	for name, tc := range map[string]struct{ cur, next, confirm, field string }{
		"mật khẩu hiện tại sai":   {"sai-mat-khau", "matkhau1", "matkhau1", "Mật khẩu hiện tại không đúng"},
		"dưới 06 ký tự":           {current, "12345", "12345", "Mật khẩu tối thiểu 06 ký tự"},
		"nhập lại không khớp":     {current, "matkhau1", "matkhau2", "Mật khẩu nhập lại không khớp"},
		"trùng mật khẩu hiện tại": {current, current, current, "Mật khẩu mới phải khác mật khẩu hiện tại"},
	} {
		if r := change(tc.cur, tc.next, tc.confirm); r.status != http.StatusUnprocessableEntity || !bytes.Contains(mustJSON(r.body), []byte(tc.field)) {
			t.Fatalf("%s: %d %v", name, r.status, r.body)
		}
	}
	if f.dir.PasswordMatches(member, "matkhau1") {
		t.Fatal("rejected changes must not set the password")
	}
	if r := change(current, "matkhau1", "matkhau1"); r.status != http.StatusNoContent {
		t.Fatalf("self change: %d %v", r.status, r.body)
	}
	u, _ = f.dir.GetUser(ctx, member)
	if !f.dir.PasswordMatches(member, "matkhau1") || f.dir.PasswordMatches(member, current) || slices.Contains(u.RequiredActions, port.ActionUpdatePassword) {
		t.Fatal("new password must work, old must not, no first-change left")
	}
}
