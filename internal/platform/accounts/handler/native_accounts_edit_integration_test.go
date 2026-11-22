package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/clock"
	"github.com/MEKONG-GREEN/hop-tac-xa/migrations"
)

// AH-276 (FN-07/AC-05..AC-08, QTPQ-Q-21..Q-23): Admin sửa, khóa, mở khóa tài khoản HTX.
func TestNativeFN07EditLockUnlock(t *testing.T) {
	f := newFixtureAt(t, clock.System{}, true)
	ctx := context.Background()
	admin := f.as(f.tenant, uuid.Nil, htxroles.Admin)
	create := func(username, phone string, roles ...string) uuid.UUID {
		t.Helper()
		r := admin.do(http.MethodPost, "/v1/accounts/htx-users", map[string]any{"username": username, "full_name": "Người " + username, "phone": phone, "cooperative_ref": f.coop30292.String(), "roles": roles})
		if r.status != http.StatusCreated {
			t.Fatalf("create %s: %d %v", username, r.status, r.body)
		}
		return uuid.MustParse(str(r.body, "user", "id"))
	}
	rep := create("edit-rep", "0912000001", htxroles.Representative, htxroles.Member)
	member := create("edit-member", "0912000002", htxroles.Member)
	edit := func(id uuid.UUID, idem string, body map[string]any) resp {
		t.Helper()
		b, _ := json.Marshal(body)
		return admin.raw(http.MethodPost, "/v1/accounts/htx-users/"+id.String()+"/revisions", idem, "application/json", b)
	}
	roles := func(id uuid.UUID) []string {
		t.Helper()
		all, err := f.dir.RealmRoles(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		slices.Sort(all)
		return slices.DeleteFunc(all, func(r string) bool { return r != htxroles.Representative && r != htxroles.Member })
	}

	// AC-N1, AC-N2: đổi HTX của tài khoản kiêm hai vai → cả hai vai chuyển HTX, phiên cũ bị thu hồi.
	before := f.dir.Logouts(rep)
	moved := map[string]any{"full_name": "Đại diện mới", "phone": "0912000001", "cooperative_ref": f.coop30301.String()}
	if r := edit(rep, "edit-move-coop", moved); r.status != http.StatusOK || str(r.body, "user", "org_ref") != f.coop30301.String() {
		t.Fatalf("move cooperative: %d %v", r.status, r.body)
	}
	if u, _ := f.dir.GetUser(ctx, rep); u.OrgRef != f.coop30301 || u.Username != "edit-rep" {
		t.Fatalf("IAM after move: %+v", u.OrgRef)
	}
	if got := roles(rep); !slices.Equal(got, []string{htxroles.Representative, htxroles.Member}) {
		t.Fatalf("both roles kept: %v", got)
	}
	if f.dir.Logouts(rep) <= before {
		t.Fatal("open sessions must be revoked after a cooperative change")
	}
	// AC-N2: token còn hạn mang HTX cũ bị từ chối ở thao tác kế tiếp (401 → đăng nhập lại); token mới qua cổng.
	stale := caller{f: f, actor: rep, token: f.iss.Sign(t, authztest.Token{Subject: rep, TenantID: f.tenant, Roles: []string{htxroles.Representative, htxroles.Member}, OrgRef: f.coop30292, MFA: true})}
	if r := stale.do(http.MethodGet, "/v1/accounts/htx-users", nil); r.status != http.StatusUnauthorized {
		t.Fatalf("stale cooperative token: %d %v", r.status, r.body)
	}
	fresh := caller{f: f, actor: rep, token: f.iss.Sign(t, authztest.Token{Subject: rep, TenantID: f.tenant, Roles: []string{htxroles.Representative, htxroles.Member}, OrgRef: f.coop30301, MFA: true})}
	if r := fresh.do(http.MethodGet, "/v1/accounts/htx-users", nil); r.status != http.StatusForbidden {
		t.Fatalf("new cooperative token passes the session gate (then FN-07 denies): %d %v", r.status, r.body)
	}
	// Phát lại cùng khóa: không ghi lại, bản lưu không chứa PII.
	if r := edit(rep, "edit-move-coop", moved); r.status != http.StatusOK {
		t.Fatalf("replay: %d %v", r.status, r.body)
	}
	var cached []byte
	if err := f.env.AdminPool(t, migrations.PF).QueryRow(ctx, `SELECT response_body FROM shared.idempotency_key WHERE tenant_id=$1 AND idem_key=$2`, f.tenant, "edit-move-coop").Scan(&cached); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(cached), "0912000001") || strings.Contains(string(cached), "Đại diện mới") || strings.Contains(string(cached), "edit-rep") {
		t.Fatal("IAM profile PII persisted in replay cache")
	}

	// AC-N3: bỏ kiêm Thành viên HTX — vai chính giữ nguyên, không thu hồi phiên.
	before = f.dir.Logouts(rep)
	if r := edit(rep, key(), map[string]any{"full_name": "Đại diện mới", "phone": "0912000001", "cooperative_ref": f.coop30301.String(), "also_member": false}); r.status != http.StatusOK {
		t.Fatalf("toggle member: %d %v", r.status, r.body)
	}
	if got := roles(rep); !slices.Equal(got, []string{htxroles.Representative}) || f.dir.Logouts(rep) != before {
		t.Fatalf("roles after toggle: %v", got)
	}
	// Thành viên HTX không kiêm được.
	if r := edit(member, key(), map[string]any{"full_name": "Người edit-member", "phone": "0912000002", "cooperative_ref": f.coop30292.String(), "also_member": true}); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("member cannot be dual: %d %v", r.status, r.body)
	}

	// AC-06: tên đăng nhập và vai không sửa được (trường lạ bị từ chối).
	for _, extra := range []string{"username", "roles"} {
		body := map[string]any{"full_name": "X", "phone": "0912000002", "cooperative_ref": f.coop30292.String(), extra: "edited"}
		if r := edit(member, key(), body); r.status != http.StatusBadRequest {
			t.Fatalf("%s must not be editable: %d", extra, r.status)
		}
	}
	// EX-02..EX-05.
	for name, tc := range map[string]struct {
		body   map[string]any
		status int
		field  string
	}{
		"missing name":    {map[string]any{"full_name": " ", "phone": "0912000002", "cooperative_ref": f.coop30292.String()}, http.StatusUnprocessableEntity, "full_name"},
		"bad phone":       {map[string]any{"full_name": "A", "phone": "912000002", "cooperative_ref": f.coop30292.String()}, http.StatusUnprocessableEntity, "phone"},
		"duplicate phone": {map[string]any{"full_name": "A", "phone": "0912000001", "cooperative_ref": f.coop30292.String()}, http.StatusConflict, ""},
		"no cooperative":  {map[string]any{"full_name": "A", "phone": "0912000002"}, http.StatusUnprocessableEntity, "cooperative_ref"},
	} {
		r := edit(member, key(), tc.body)
		if r.status != tc.status || (tc.field != "" && !strings.Contains(string(mustJSON(r.body)), tc.field)) {
			t.Fatalf("%s: %d %v", name, r.status, r.body)
		}
	}
	if u, _ := f.dir.GetUser(ctx, member); u.Phone != "0912000002" || u.OrgRef != f.coop30292 {
		t.Fatal("rejected edits must not change IAM")
	}

	// AC-07: khóa → đăng nhập bị chặn và phiên bị thu hồi; mở khóa → đăng nhập lại được.
	before = f.dir.Logouts(member)
	if r := admin.do(http.MethodPost, "/v1/accounts/htx-users/"+member.String()+"/lock", nil); r.status != http.StatusOK || r.body["user"].(map[string]any)["enabled"] != false {
		t.Fatalf("lock: %d %v", r.status, r.body)
	}
	if u, _ := f.dir.GetUser(ctx, member); u.Enabled || f.dir.Logouts(member) <= before {
		t.Fatal("locked account must not log in and must lose sessions")
	}
	if r := admin.do(http.MethodPost, "/v1/accounts/htx-users/"+member.String()+"/lock", nil); r.status != http.StatusConflict {
		t.Fatalf("lock twice: %d %v", r.status, r.body)
	}
	if r := admin.do(http.MethodPost, "/v1/accounts/htx-users/"+member.String()+"/unlock", nil); r.status != http.StatusOK || r.body["user"].(map[string]any)["enabled"] != true {
		t.Fatalf("unlock: %d %v", r.status, r.body)
	}
	if u, _ := f.dir.GetUser(ctx, member); !u.Enabled {
		t.Fatal("unlocked account logs in again")
	}

	// BR-07, AC-08: không tự khóa chính mình; vai khác Admin không có chức năng.
	self := f.as(f.tenant, uuid.Nil, htxroles.Admin)
	if r := self.do(http.MethodPost, "/v1/accounts/htx-users/"+self.actor.String()+"/lock", nil); r.status != http.StatusForbidden {
		t.Fatalf("self lock: %d %v", r.status, r.body)
	}
	for _, role := range []string{htxroles.Representative, htxroles.Member} {
		who := f.as(f.tenant, f.coop30292, role)
		if r := who.do(http.MethodPost, "/v1/accounts/htx-users/"+member.String()+"/lock", nil); r.status != http.StatusForbidden {
			t.Fatalf("%s lock: %d", role, r.status)
		}
	}
	other := f.as(uuid.New(), uuid.Nil, htxroles.Admin)
	if r := other.do(http.MethodPost, "/v1/accounts/htx-users/"+member.String()+"/lock", nil); r.status != http.StatusNotFound {
		t.Fatalf("cross tenant lock: %d %v", r.status, r.body)
	}
}
