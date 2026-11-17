package handler_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	coopport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/port"
	partyport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/party/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
)

// FN-06 (ADM-02, TECH-01): danh sách nông hộ của một HTX theo phạm vi dữ liệu — Admin mọi HTX (AC-01); Đại diện HTX,
// Thành viên HTX chỉ HTX của mình, HTX khác (kể cả cùng xã) → 404, không có dữ liệu (AC-03, AC-06, AC-N2); tài khoản
// chưa gắn HTX → 403 node_scope_required, không có dữ liệu (AC-N1); vai khuyến nông giữ phạm vi theo xã (đối chứng).
func TestFN06_HouseholdsScope(t *testing.T) {
	f := newFixture(t)
	root := f.root()
	tr := f.seedTree(root)
	c3 := root.createOrg(map[string]any{"kind": "cooperative", "name": "HTX Cùng Xã B", "admin_unit_code": "30001", "parent_ref": tr.t30001.String()})
	h1 := str(f.as(f.tenant, tr.c1, partyport.RoleCoopManager).appDo(http.MethodPost, "/v1/households", farmer(tr.c1, "0907300001")).body, "household", "household_ref")
	h3 := str(f.as(f.tenant, c3, partyport.RoleCoopManager).appDo(http.MethodPost, "/v1/households", farmer(c3, "0907300003")).body, "household", "household_ref")
	if h1 == "" || h3 == "" {
		t.Fatalf("không tạo được hộ mẫu: %q %q", h1, h3)
	}
	path := func(coop uuid.UUID) string { return "/v1/cooperatives/" + coop.String() + "/households" }
	wantOnly := func(c caller, coop uuid.UUID, h, what string) {
		t.Helper()
		r := c.appDo(http.MethodGet, path(coop), nil)
		if r.status != http.StatusOK || len(items(r)) != 1 || items(r)[0]["household_ref"] != h {
			t.Fatalf("%s: %d %v", what, r.status, r.body)
		}
	}
	wantDenied := func(c caller, coop uuid.UUID, status int, code, what string) {
		t.Helper()
		r := c.appDo(http.MethodGet, path(coop), nil)
		if r.status != status || r.body["code"] != code {
			t.Fatalf("%s: %d %v, cần %d %s", what, r.status, r.body, status, code)
		}
		if _, ok := r.body["items"]; ok {
			t.Fatalf("%s: phản hồi có dữ liệu: %v", what, r.body)
		}
	}

	// AC-01: Admin xem nông hộ của mọi HTX.
	admin := f.as(f.tenant, uuid.Nil, authz.RolePlatformAdmin)
	wantOnly(admin, tr.c1, h1, "Admin xem hộ c1")
	wantOnly(admin, c3, h3, "Admin xem hộ c3")

	// AC-N2 (API), AC-06: Đại diện HTX gọi trực tiếp API danh sách hộ của HTX khác → 404, không có dữ liệu.
	mgr := f.as(f.tenant, tr.c1, partyport.RoleCoopManager)
	wantOnly(mgr, tr.c1, h1, "Đại diện HTX xem hộ HTX mình")
	wantDenied(mgr, c3, http.StatusNotFound, "party.org_not_found", "Đại diện HTX xem hộ HTX cùng xã")
	wantDenied(mgr, tr.c2, http.StatusNotFound, "party.org_not_found", "Đại diện HTX xem hộ HTX tỉnh khác")

	// AC-03: Thành viên HTX chỉ xem hộ của HTX mình.
	member := f.as(f.tenant, tr.c1, coopport.RoleHTXMember)
	wantOnly(member, tr.c1, h1, "Thành viên HTX xem hộ HTX mình")
	wantDenied(member, c3, http.StatusNotFound, "party.org_not_found", "Thành viên HTX xem hộ HTX cùng xã")

	// AC-N1: tài khoản chưa gắn HTX → "tài khoản chưa gắn đơn vị", không có dữ liệu.
	for _, role := range []string{partyport.RoleCoopManager, coopport.RoleHTXMember} {
		wantDenied(f.as(f.tenant, uuid.Nil, role), tr.c1, http.StatusForbidden, "node_scope_required", role+" chưa gắn HTX")
	}

	// Đối chứng: cán bộ khuyến nông xã 30001 vẫn xem hộ của mọi HTX trong xã.
	officer := f.as(f.tenant, tr.t30001, partyport.RoleKNOfficer)
	wantOnly(officer, c3, h3, "khuyến nông xem hộ HTX trong xã")
	wantOnly(officer, tr.c1, h1, "khuyến nông xem hộ c1")
	// Kèm Thành viên HTX không thu hẹp phạm vi của vai khuyến nông (htxroles.OwnCoopOnly xét vai rộng hơn).
	wantOnly(f.as(f.tenant, tr.t30001, coopport.RoleHTXMember, partyport.RoleKNOfficer), c3, h3, "khuyến nông + Thành viên xem hộ HTX trong xã")
}
