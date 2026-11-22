package handler_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/clock"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/httpx"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/hop-tac-xa/migrations"
)

// AH-277 (FN-08/AC-01..AC-06, AC-N1..AC-N3): Đại diện HTX quản lý tài khoản Thành viên HTX của HTX mình (Application API).
func TestFN08CoopManagerAccounts(t *testing.T) {
	f := newFixtureAt(t, clock.System{}, true)
	eng := httpx.NewEngine(telemetry.NewLogger("test", io.Discard), 1<<20)
	app := eng.Group("/v1", httpx.Authenticate(f.iss.Verifier()))
	app.Use(f.mod.Middleware())
	app.Use(idempotency.Middleware(f.env.Pool(t, migrations.PF, "platform_api")))
	f.mod.RegisterApp(app)
	srv := httptest.NewServer(eng)
	t.Cleanup(srv.Close)

	admin := f.as(f.tenant, uuid.Nil, htxroles.Admin)
	create := func(username, phone string, coop uuid.UUID, roles ...string) uuid.UUID {
		t.Helper()
		r := admin.do(http.MethodPost, "/v1/accounts/htx-users", map[string]any{"username": username, "full_name": "Người " + username, "phone": phone, "cooperative_ref": coop.String(), "roles": roles})
		if r.status != http.StatusCreated {
			t.Fatalf("create %s: %d %v", username, r.status, r.body)
		}
		return uuid.MustParse(str(r.body, "user", "id"))
	}
	repID := create("fn08-rep", "0912100001", f.coop30292, htxroles.Representative)
	member := create("fn08-member", "0912100002", f.coop30292, htxroles.Member)
	otherCoop := create("fn08-other", "0912100003", f.coop30301, htxroles.Member)
	otherRep := create("fn08-rep2", "0912100004", f.coop30292, htxroles.Representative)
	token := f.iss.Sign(t, authztest.Token{Subject: repID, TenantID: f.tenant, Roles: []string{htxroles.Representative}, OrgRef: f.coop30292, MFA: true})
	call := func(method, path string, body any) resp {
		t.Helper()
		var rd io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			rd = bytes.NewReader(b)
		}
		req, _ := http.NewRequest(method, srv.URL+path, rd)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		if method == http.MethodPost {
			req.Header.Set("Idempotency-Key", key())
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = res.Body.Close() }()
		raw, _ := io.ReadAll(res.Body)
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		return resp{status: res.StatusCode, hdr: res.Header, body: m}
	}

	// AC-01, BR-01: chỉ Thành viên HTX của HTX mình và dòng của chính mình.
	r := call(http.MethodGet, "/v1/htx-accounts", nil)
	if r.status != http.StatusOK {
		t.Fatalf("list: %d %v", r.status, r.body)
	}
	var ids []string
	for _, it := range r.body["items"].([]any) {
		ids = append(ids, it.(map[string]any)["id"].(string))
	}
	slices.Sort(ids)
	want := []string{repID.String(), member.String()}
	slices.Sort(want)
	if !slices.Equal(ids, want) {
		t.Fatalf("visible accounts: %v, want %v (not %s, %s)", ids, want, otherCoop, otherRep)
	}

	// AC-02, AC-03, AC-N1: chỉ tạo Thành viên HTX của HTX mình; nhiều Thành viên đều tạo được.
	for i, phone := range []string{"0912100005", "0912100006"} {
		username := []string{"fn08-newa", "fn08-newb"}[i]
		r := call(http.MethodPost, "/v1/htx-accounts", map[string]any{"username": username, "full_name": "Thành viên mới", "phone": phone, "cooperative_ref": f.coop30292.String(), "roles": []string{htxroles.Member}})
		if r.status != http.StatusCreated || str(r.body, "temporary_password") == "" {
			t.Fatalf("create member %d: %d %v", i, r.status, r.body)
		}
	}
	if r := call(http.MethodPost, "/v1/htx-accounts", map[string]any{"username": "fn08-x1", "full_name": "X", "phone": "0912100007", "cooperative_ref": f.coop30301.String(), "roles": []string{htxroles.Member}}); r.status != http.StatusForbidden {
		t.Fatalf("other cooperative: %d %v", r.status, r.body)
	}
	if r := call(http.MethodPost, "/v1/htx-accounts", map[string]any{"username": "fn08-x2", "full_name": "X", "phone": "0912100008", "cooperative_ref": f.coop30292.String(), "roles": []string{htxroles.Representative}}); r.status != http.StatusForbidden {
		t.Fatalf("other role: %d %v", r.status, r.body)
	}
	// AC-N2: trùng SĐT.
	if r := call(http.MethodPost, "/v1/htx-accounts", map[string]any{"username": "fn08-x3", "full_name": "X", "phone": "0912100002", "cooperative_ref": f.coop30292.String(), "roles": []string{htxroles.Member}}); r.status != http.StatusConflict {
		t.Fatalf("duplicate phone: %d %v", r.status, r.body)
	}

	// Sửa Thành viên HTX của HTX mình; không đổi HTX; tài khoản ngoài phạm vi → 404 (AC-04).
	if r := call(http.MethodPost, "/v1/htx-accounts/"+member.String()+"/revisions", map[string]any{"full_name": "Thành viên đổi tên", "phone": "0912100002"}); r.status != http.StatusOK {
		t.Fatalf("edit member: %d %v", r.status, r.body)
	}
	if r := call(http.MethodPost, "/v1/htx-accounts/"+member.String()+"/revisions", map[string]any{"full_name": "X", "phone": "0912100002", "cooperative_ref": f.coop30301.String()}); r.status != http.StatusForbidden {
		t.Fatalf("move member: %d %v", r.status, r.body)
	}
	for _, id := range []uuid.UUID{otherCoop, otherRep} {
		if r := call(http.MethodPost, "/v1/htx-accounts/"+id.String()+"/revisions", map[string]any{"full_name": "X", "phone": "0912100099"}); r.status != http.StatusNotFound {
			t.Fatalf("out of scope edit %s: %d %v", id, r.status, r.body)
		}
		if r := call(http.MethodPost, "/v1/htx-accounts/"+id.String()+"/lock", nil); r.status != http.StatusNotFound {
			t.Fatalf("out of scope lock %s: %d %v", id, r.status, r.body)
		}
	}

	// AC-05: khóa / mở khóa Thành viên HTX; AC-N3: không tự khóa.
	if r := call(http.MethodPost, "/v1/htx-accounts/"+member.String()+"/lock", nil); r.status != http.StatusOK {
		t.Fatalf("lock member: %d %v", r.status, r.body)
	}
	if u, _ := f.dir.GetUser(t.Context(), member); u.Enabled {
		t.Fatal("locked member must not log in")
	}
	if r := call(http.MethodPost, "/v1/htx-accounts/"+member.String()+"/unlock", nil); r.status != http.StatusOK {
		t.Fatalf("unlock member: %d %v", r.status, r.body)
	}
	if r := call(http.MethodPost, "/v1/htx-accounts/"+repID.String()+"/lock", nil); r.status != http.StatusForbidden {
		t.Fatalf("self lock: %d %v", r.status, r.body)
	}

	// AC-06, QTPQ-Q-28: chính mình chỉ bật/tắt kiêm Thành viên HTX; họ tên gửi kèm bị bỏ qua.
	if r := call(http.MethodPost, "/v1/htx-accounts/"+repID.String()+"/revisions", map[string]any{"full_name": "Đổi tên mình", "phone": "0912100001", "also_member": true}); r.status != http.StatusOK {
		t.Fatalf("self also member: %d %v", r.status, r.body)
	}
	roles, _ := f.dir.RealmRoles(t.Context(), repID)
	u, _ := f.dir.GetUser(t.Context(), repID)
	if !slices.Contains(roles, htxroles.Member) || u.FirstName == "mình" {
		t.Fatalf("self toggle: roles %v, name %q %q", roles, u.LastName, u.FirstName)
	}
	if r := call(http.MethodPost, "/v1/htx-accounts/"+repID.String()+"/revisions", map[string]any{"full_name": "x", "phone": "0912100001", "also_member": false}); r.status != http.StatusOK {
		t.Fatalf("self remove member: %d %v", r.status, r.body)
	}
	if roles, _ := f.dir.RealmRoles(t.Context(), repID); slices.Contains(roles, htxroles.Member) {
		t.Fatalf("member role must be removed: %v", roles)
	}
	// Thành viên HTX không dùng API FN-08.
	token = f.iss.Sign(t, authztest.Token{Subject: member, TenantID: f.tenant, Roles: []string{htxroles.Member}, OrgRef: f.coop30292, MFA: true})
	if r := call(http.MethodGet, "/v1/htx-accounts", nil); r.status != http.StatusForbidden {
		t.Fatalf("member list: %d %v", r.status, r.body)
	}
}
