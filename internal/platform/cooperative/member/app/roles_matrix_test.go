package app

import (
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	port "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
)

// FN-05/BR-03, TECH-01: quyền của Thành viên HTX ở API nông hộ khớp ma trận Mục V (FN-04: R) — đọc danh sách, không
// ghi. Admin/Đại diện HTX lệch ma trận (vd platform_admin chưa ghi nông hộ) không sửa ở AH-272.
func TestFN05_HTXMemberRolesMatchMatrix(t *testing.T) {
	if port.RoleHTXMember != htxroles.Member {
		t.Fatalf("RoleHTXMember = %q", port.RoleHTXMember)
	}
	if got, want := slices.Contains(householdReaderRoles, htxroles.Member), htxroles.Can(htxroles.Member, "FN-04", htxroles.Read); got != want {
		t.Fatalf("htx_member ∈ householdReaderRoles = %v, ma trận FN-04 R = %v", got, want)
	}
	writes := htxroles.Can(htxroles.Member, "FN-04", htxroles.Create) || htxroles.Can(htxroles.Member, "FN-04", htxroles.Update) ||
		htxroles.Can(htxroles.Member, "FN-04", htxroles.Delete)
	if slices.Contains(householdWriterRoles, htxroles.Member) != writes {
		t.Fatalf("htx_member ∈ householdWriterRoles phải khớp ma trận FN-04 C/U/D = %v", writes)
	}
}

// Thành viên HTX chỉ thấy HTX của mình như quản lý HTX; kiêm Đại diện HTX thì theo Đại diện HTX (BR-02).
func TestIsOwnCoopOnly(t *testing.T) {
	org := uuid.New()
	for _, c := range []struct {
		roles []string
		want  bool
	}{
		{[]string{"htx_member"}, true},
		{[]string{"coop_manager"}, true},
		{[]string{"htx_member", "coop_manager"}, true},
		{[]string{"htx_member", authz.RolePlatformAdmin}, false},
		{[]string{"coop_manager", "kn_officer"}, false},
		{[]string{"kn_officer"}, false},
	} {
		if got := isOwnCoopOnly(authz.Principal{Roles: c.roles, OrgRef: org}); got != c.want {
			t.Errorf("isOwnCoopOnly(%v) = %v, cần %v", c.roles, got, c.want)
		}
	}
}
