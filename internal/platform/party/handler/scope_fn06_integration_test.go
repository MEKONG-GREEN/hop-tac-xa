package handler_test

import (
	"net/http"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/party/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
)

// fn06Tree là cây của seedTree cộng HTX c3 cùng xã 30001 với c1 (HTX ngang hàng, chưa chia sẻ).
type fn06Tree struct {
	tree
	c3 uuid.UUID
}

func (f *fixture) seedFN06(root caller) fn06Tree {
	tr := f.seedTree(root)
	c3 := root.createOrg(map[string]any{"kind": "cooperative", "name": "HTX Cùng Xã B", "admin_unit_code": "30001", "parent_ref": tr.t30001.String()})
	return fn06Tree{tree: tr, c3: c3}
}

// wantOutOfScope: bản ghi ngoài phạm vi → 404 party.org_not_found, không có dữ liệu của HTX trong phản hồi (FN-06/EX-01).
func wantOutOfScope(t *testing.T, r resp, what string) {
	t.Helper()
	wantDenied(t, r, http.StatusNotFound, "party.org_not_found", what)
}

// wantDenied: bị từ chối với đúng mã lỗi, phản hồi không có dữ liệu của HTX.
func wantDenied(t *testing.T, r resp, status int, code, what string) {
	t.Helper()
	if r.status != status || r.body["code"] != code {
		t.Fatalf("%s: %d %v, cần %d %s", what, r.status, r.body, status, code)
	}
	for _, k := range []string{"ref", "name", "admin_unit_code", "items"} {
		if _, ok := r.body[k]; ok {
			t.Fatalf("%s: phản hồi lộ trường %q: %v", what, k, r.body)
		}
	}
}

// FN-06/AC-01, BR-01: Admin xem được mọi HTX — danh sách HTX, danh sách tổ chức và chi tiết HTX.
func TestFN06_AC01_AdminSeesAllCooperatives(t *testing.T) {
	f := newFixture(t)
	tr := f.seedFN06(f.root())
	want := []string{tr.c1.String(), tr.c2.String(), tr.c3.String()}
	slices.Sort(want)
	admin := f.as(f.tenant, uuid.Nil, authz.RolePlatformAdmin)
	for _, path := range []string{"/v1/cooperatives", "/v1/orgs?kind=cooperative"} {
		r := admin.appDo(http.MethodGet, path, nil)
		got := refs(r, "ref")
		slices.Sort(got)
		if r.status != http.StatusOK || !slices.Equal(got, want) {
			t.Fatalf("Admin %s: %d %v, cần %v", path, r.status, got, want)
		}
	}
	if r := admin.appDo(http.MethodGet, "/v1/cooperatives/"+tr.c2.String(), nil); r.status != http.StatusOK || str(r.body, "ref") != tr.c2.String() {
		t.Fatalf("Admin xem HTX tỉnh khác: %d %v", r.status, r.body)
	}
}

// FN-06/AC-03, BR-03: Thành viên HTX chỉ xem HTX của mình — kể cả lọc theo xã; HTX khác (cùng xã hay khác tỉnh) → 404.
func TestFN06_AC03_MemberSeesOwnCooperativeOnly(t *testing.T) {
	f := newFixture(t)
	tr := f.seedFN06(f.root())
	member := f.as(f.tenant, tr.c1, port.RoleHTXMember)
	for _, path := range []string{"/v1/cooperatives", "/v1/cooperatives?admin_unit=30001", "/v1/orgs", "/v1/orgs?kind=cooperative"} {
		if r := member.appDo(http.MethodGet, path, nil); r.status != http.StatusOK || !slices.Equal(refs(r, "ref"), []string{tr.c1.String()}) {
			t.Fatalf("Thành viên HTX %s: %d %v, cần chỉ [c1]", path, r.status, r.body)
		}
	}
	if r := member.appDo(http.MethodGet, "/v1/cooperatives/"+tr.c1.String(), nil); r.status != http.StatusOK || str(r.body, "ref") != tr.c1.String() {
		t.Fatalf("Thành viên HTX xem HTX mình: %d %v", r.status, r.body)
	}
	wantOutOfScope(t, member.appDo(http.MethodGet, "/v1/cooperatives/"+tr.c3.String(), nil), "Thành viên HTX xem HTX cùng xã")
	wantOutOfScope(t, member.appDo(http.MethodGet, "/v1/cooperatives/"+tr.c2.String(), nil), "Thành viên HTX xem HTX tỉnh khác")
}

// FN-06/AC-06, BR-02, AC-N2 (API): Đại diện HTX chỉ thấy HTX của mình, không còn thấy HTX cùng xã; gọi trực tiếp API
// chi tiết HTX ngoài phạm vi → 404, không có dữ liệu. Tài khoản kiêm hai vai dùng phạm vi của Đại diện HTX.
func TestFN06_AC06_RepresentativeOutOfScopeDenied(t *testing.T) {
	f := newFixture(t)
	tr := f.seedFN06(f.root())
	for name, c := range map[string]caller{
		"Đại diện HTX": f.as(f.tenant, tr.c1, port.RoleCoopManager),
		"kiêm hai vai": f.as(f.tenant, tr.c1, port.RoleCoopManager, port.RoleHTXMember),
		"kèm vai thửa": f.as(f.tenant, tr.c1, port.RoleCoopManager, "plot_viewer"),
	} {
		for _, path := range []string{"/v1/cooperatives", "/v1/orgs"} {
			if r := c.appDo(http.MethodGet, path, nil); r.status != http.StatusOK || !slices.Equal(refs(r, "ref"), []string{tr.c1.String()}) {
				t.Fatalf("%s %s: %d %v, cần chỉ [c1]", name, path, r.status, r.body)
			}
		}
		if r := c.appDo(http.MethodGet, "/v1/cooperatives/"+tr.c1.String(), nil); r.status != http.StatusOK {
			t.Fatalf("%s xem HTX mình: %d %v", name, r.status, r.body)
		}
		wantOutOfScope(t, c.appDo(http.MethodGet, "/v1/cooperatives/"+tr.c3.String(), nil), name+" xem HTX cùng xã")
		wantOutOfScope(t, c.appDo(http.MethodGet, "/v1/cooperatives/"+tr.c2.String(), nil), name+" xem HTX tỉnh khác")
	}
}

// AC-N1, FN-06/EX-02 (API): tài khoản Đại diện HTX / Thành viên HTX chưa gắn HTX không nhận dữ liệu của HTX nào —
// danh sách rỗng; chi tiết HTX bị tầng dùng chung từ chối (403 node_scope_required — "tài khoản chưa gắn đơn vị",
// web hiện câu "Tài khoản chưa được gắn HTX…").
func TestFN06_ACN1_NoCoopAccountSeesNothing(t *testing.T) {
	f := newFixture(t)
	tr := f.seedFN06(f.root())
	for _, role := range []string{port.RoleCoopManager, port.RoleHTXMember} {
		c := f.as(f.tenant, uuid.Nil, role)
		for _, path := range []string{"/v1/cooperatives", "/v1/orgs"} {
			if r := c.appDo(http.MethodGet, path, nil); r.status != http.StatusOK || len(items(r)) != 0 {
				t.Fatalf("%s chưa gắn HTX %s: %d %v, cần rỗng", role, path, r.status, r.body)
			}
		}
		wantDenied(t, c.appDo(http.MethodGet, "/v1/cooperatives/"+tr.c1.String(), nil), http.StatusForbidden, "node_scope_required", role+" chưa gắn HTX xem HTX")
	}
}

// Đối chứng (quyết định 17/11/2026): phạm vi theo xã của vai khuyến nông không đổi — cán bộ tổ xã 30001 vẫn thấy
// cả hai HTX của xã; kèm vai Đại diện HTX cũng vậy.
func TestFN06_KNOfficerScopeUnchanged(t *testing.T) {
	f := newFixture(t)
	tr := f.seedFN06(f.root())
	want := []string{tr.c1.String(), tr.c3.String()}
	slices.Sort(want)
	for name, c := range map[string]caller{
		"khuyến nông":            f.as(f.tenant, tr.t30001, port.RoleKNOfficer),
		"khuyến nông + Đại diện": f.as(f.tenant, tr.c1, port.RoleCoopManager, port.RoleKNOfficer),
	} {
		got := refs(c.appDo(http.MethodGet, "/v1/cooperatives", nil), "ref")
		slices.Sort(got)
		if !slices.Equal(got, want) {
			t.Fatalf("%s: danh sách HTX %v, cần %v", name, got, want)
		}
		orgs := refs(c.appDo(http.MethodGet, "/v1/orgs?kind=cooperative", nil), "ref")
		slices.Sort(orgs)
		if !slices.Equal(orgs, want) {
			t.Fatalf("%s: danh sách tổ chức %v, cần %v", name, orgs, want)
		}
		if r := c.appDo(http.MethodGet, "/v1/cooperatives/"+tr.c3.String(), nil); r.status != http.StatusOK {
			t.Fatalf("%s xem HTX cùng xã: %d %v", name, r.status, r.body)
		}
	}
}
