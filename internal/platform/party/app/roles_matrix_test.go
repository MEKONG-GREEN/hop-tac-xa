package app

import (
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/party/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
)

// FN-05/BR-03, TECH-01: quyền của Thành viên HTX ở sổ HTX khớp ma trận Mục V (FN-03: R) — xem HTX và danh sách tổ
// chức/HTX (giới hạn HTX của mình — ownCoopOnly, FN-06); mọi thao tác quản trị đều không có.
func TestFN05_HTXMemberRolesMatchMatrix(t *testing.T) {
	if port.RoleHTXMember != htxroles.Member {
		t.Fatalf("RoleHTXMember = %q", port.RoleHTXMember)
	}
	if got, want := slices.Contains(readerRoles, htxroles.Member), htxroles.Can(htxroles.Member, "FN-03", htxroles.Read); got != want {
		t.Fatalf("htx_member ∈ readerRoles = %v, ma trận FN-03 R = %v", got, want)
	}
	if slices.Contains(adminRoles, htxroles.Member) || slices.Contains(orgNodeRoles, htxroles.Member) {
		t.Fatal("Thành viên HTX không có quyền quản trị sổ HTX (FN-03 không có C/U/D)")
	}
}

// FN-06/BR-02, BR-03: Đại diện HTX và Thành viên HTX chỉ thấy HTX gắn với tài khoản; kèm vai có phạm vi rộng hơn
// (Admin, quản trị sổ tổ chức, khuyến nông, Cục KTHT) thì theo phạm vi của vai đó.
func TestOwnCoopOnlyParty(t *testing.T) {
	org := uuid.New()
	for _, c := range []struct {
		roles []string
		want  bool
	}{
		{[]string{port.RoleHTXMember}, true},
		{[]string{port.RoleCoopManager}, true},
		{[]string{port.RoleCoopManager, port.RoleHTXMember}, true},
		{[]string{port.RoleCoopManager, port.RoleCGHViewer}, false},
		{[]string{port.RoleCoopManager, port.RoleKNOfficer}, false},
		{[]string{port.RoleCoopManager, port.RoleRegistryAdmin}, false},
		{[]string{port.RoleCoopManager, authz.RolePlatformAdmin}, false},
		{[]string{port.RoleKNOfficer}, false},
		{[]string{authz.RolePlatformAdmin}, false},
		{nil, false},
	} {
		if got := ownCoopOnly(authz.Principal{Roles: c.roles, OrgRef: org}); got != c.want {
			t.Errorf("ownCoopOnly(%v) = %v, cần %v", c.roles, got, c.want)
		}
	}
}
