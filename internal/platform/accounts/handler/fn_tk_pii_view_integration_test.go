package handler_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/clock"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/httpx"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/hop-tac-xa/migrations"
)

// FN-TK-02/BR-04 (AH-223): danh sách che SĐT (09•• ••• 678), tìm theo SĐT ở máy chủ; chi tiết trả SĐT đầy đủ và ghi
// pf_accounts.pii_view (ai xem, tài khoản nào — không lưu SĐT). Đại diện HTX chỉ xem chi tiết trong phạm vi FN-08.
func TestNativeFNTK02_BR04_CheSDT(t *testing.T) {
	f := newFixtureAt(t, clock.System{}, true)
	ctx := context.Background()
	admin := f.as(f.tenant, uuid.Nil, htxroles.Admin)
	create := func(username, phone string, coop uuid.UUID, roles ...string) uuid.UUID {
		t.Helper()
		r := admin.do(http.MethodPost, "/v1/accounts/htx-users", map[string]any{"username": username, "full_name": "Người " + username, "phone": phone, "cooperative_ref": coop.String(), "roles": roles})
		if r.status != http.StatusCreated {
			t.Fatalf("create %s: %d %v", username, r.status, r.body)
		}
		return uuid.MustParse(str(r.body, "user", "id"))
	}
	rep := create("br04-rep", "0912200001", f.coop30292, htxroles.Representative)
	member := create("br04-member", "0912200678", f.coop30292, htxroles.Member)
	other := create("br04-other", "0912200003", f.coop30301, htxroles.Member)

	views := func(user uuid.UUID) (n int, actors []string) {
		t.Helper()
		rows, err := f.env.AdminPool(t, migrations.PF).Query(ctx, `SELECT actor_ref::text, field FROM pf_accounts.pii_view WHERE tenant_id = $1 AND user_ref = $2 ORDER BY seq`, f.tenant, user)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		for rows.Next() {
			var a, field string
			if err := rows.Scan(&a, &field); err != nil || field != "phone" {
				t.Fatalf("pii_view row: %v %q", err, field)
			}
			n++
			actors = append(actors, a)
		}
		return n, actors
	}
	phoneOf := func(item any) string { s, _ := item.(map[string]any)["phone"].(string); return s }

	// Danh sách: mọi SĐT đều đã che; không ghi sổ truy cập.
	r := admin.do(http.MethodGet, "/v1/accounts/htx-users", nil)
	if r.status != http.StatusOK {
		t.Fatalf("list: %d %v", r.status, r.body)
	}
	raw, _ := json.Marshal(r.body)
	if strings.Contains(string(raw), "0912200678") || strings.Contains(string(raw), "0912200001") {
		t.Fatalf("list leaks full phone: %s", raw)
	}
	for _, it := range r.body["items"].([]any) {
		if it.(map[string]any)["id"] == member.String() && phoneOf(it) != "09•• ••• 678" {
			t.Fatalf("masked phone: %q", phoneOf(it))
		}
	}
	if n, _ := views(member); n != 0 {
		t.Fatal("listing must not log a full-phone view")
	}
	// FN-07/AC-02: tìm theo SĐT (so trên SĐT đầy đủ ở máy chủ) → chỉ còn tài khoản đó, vẫn che.
	r = admin.do(http.MethodGet, "/v1/accounts/htx-users?phone=0912200678", nil)
	if items := r.body["items"].([]any); r.status != http.StatusOK || len(items) != 1 || items[0].(map[string]any)["id"] != member.String() || phoneOf(items[0]) != "09•• ••• 678" {
		t.Fatalf("phone search: %d %v", r.status, r.body)
	}
	if r := admin.do(http.MethodGet, "/v1/accounts/htx-users?phone=091220", nil); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("bad phone search: %d %v", r.status, r.body)
	}
	// Chi tiết: SĐT đầy đủ, một dòng sổ cho mỗi lượt xem, không chứa SĐT.
	r = admin.do(http.MethodGet, "/v1/accounts/htx-users/"+member.String(), nil)
	if r.status != http.StatusOK || r.body["phone"] != "0912200678" {
		t.Fatalf("detail: %d %v", r.status, r.body)
	}
	if n, actors := views(member); n != 1 || actors[0] != admin.actor.String() {
		t.Fatalf("pii_view after admin detail: %d %v", n, actors)
	}

	// Đại diện HTX (Application API): danh sách che SĐT; chi tiết Thành viên HTX của HTX mình được, HTX khác thì không.
	eng := httpx.NewEngine(telemetry.NewLogger("test", io.Discard), 1<<20)
	app := eng.Group("/v1", httpx.Authenticate(f.iss.Verifier()))
	app.Use(f.mod.Middleware())
	f.mod.RegisterApp(app)
	srv := httptest.NewServer(eng)
	t.Cleanup(srv.Close)
	token := f.iss.Sign(t, authztest.Token{Subject: rep, TenantID: f.tenant, Roles: []string{htxroles.Representative}, OrgRef: f.coop30292, MFA: true})
	get := func(path string) resp {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, srv.URL+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = res.Body.Close() }()
		b, _ := io.ReadAll(res.Body)
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		return resp{status: res.StatusCode, hdr: res.Header, body: m}
	}
	r = get("/v1/htx-accounts")
	raw, _ = json.Marshal(r.body)
	if r.status != http.StatusOK || strings.Contains(string(raw), "0912200678") {
		t.Fatalf("coop list: %d %s", r.status, raw)
	}
	if r := get("/v1/htx-accounts/" + member.String()); r.status != http.StatusOK || r.body["phone"] != "0912200678" {
		t.Fatalf("coop detail: %d %v", r.status, r.body)
	}
	if n, actors := views(member); n != 2 || actors[1] != rep.String() {
		t.Fatalf("pii_view after coop detail: %d %v", n, actors)
	}
	if r := get("/v1/htx-accounts/" + other.String()); r.status != http.StatusNotFound {
		t.Fatalf("coop detail out of scope: %d %v", r.status, r.body)
	}
	if n, _ := views(other); n != 0 {
		t.Fatal("rejected detail must not log a view")
	}
}
