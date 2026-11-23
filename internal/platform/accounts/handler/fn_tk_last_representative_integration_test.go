package handler_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/clock"
)

// FN-TK-02/EX-04 (AH-223): khóa Đại diện HTX cuối cùng còn hoạt động của một HTX → hỏi xác nhận "HTX sẽ không còn người
// duyệt kết quả lô"; còn Đại diện HTX khác đang hoạt động thì không hỏi. Khóa Thành viên HTX không bao giờ hỏi.
func TestNativeFNTK02_EX04_KhoaDaiDienCuoiCung(t *testing.T) {
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
	lock := func(id uuid.UUID, query string) resp {
		t.Helper()
		return admin.do(http.MethodPost, "/v1/accounts/htx-users/"+id.String()+"/lock"+query, nil)
	}
	repA := create("ex04-rep-a", "0912000101", htxroles.Representative)
	repB := create("ex04-rep-b", "0912000102", htxroles.Representative, htxroles.Member)
	member := create("ex04-member", "0912000103", htxroles.Member)

	// Còn Đại diện HTX khác đang hoạt động → khóa ngay.
	if r := lock(repA, ""); r.status != http.StatusOK {
		t.Fatalf("lock with another active representative: %d %v", r.status, r.body)
	}
	// repB giờ là Đại diện cuối cùng còn hoạt động → 409, tài khoản chưa bị khóa.
	r := lock(repB, "")
	if r.status != http.StatusConflict || r.body["code"] != "accounts.last_representative" || r.body["title"] != "HTX sẽ không còn người duyệt kết quả lô" {
		t.Fatalf("last representative without confirmation: %d %v", r.status, r.body)
	}
	if u, _ := f.dir.GetUser(ctx, repB); !u.Enabled {
		t.Fatal("unconfirmed lock must not disable the account")
	}
	// Thành viên HTX: không hỏi.
	if r := lock(member, ""); r.status != http.StatusOK {
		t.Fatalf("lock member: %d %v", r.status, r.body)
	}
	// Xác nhận → khóa.
	if r := lock(repB, "?confirm_no_representative=true"); r.status != http.StatusOK || r.body["user"].(map[string]any)["enabled"] != false {
		t.Fatalf("confirmed lock: %d %v", r.status, r.body)
	}
	if u, _ := f.dir.GetUser(ctx, repB); u.Enabled {
		t.Fatal("confirmed lock disables the account")
	}
	// Mở khóa không hỏi.
	if r := admin.do(http.MethodPost, "/v1/accounts/htx-users/"+repA.String()+"/unlock", nil); r.status != http.StatusOK {
		t.Fatalf("unlock: %d %v", r.status, r.body)
	}
}
