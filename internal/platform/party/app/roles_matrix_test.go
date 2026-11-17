package app

import (
	"slices"
	"testing"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/party/port"
)

// FN-05/BR-03, TECH-01: quyền của Thành viên HTX ở sổ HTX khớp ma trận Mục V (FN-03: R) — chỉ xem một HTX
// (GetCooperative, giới hạn HTX của mình); danh sách tổ chức/HTX và mọi thao tác quản trị đều không có.
func TestFN05_HTXMemberRolesMatchMatrix(t *testing.T) {
	if port.RoleHTXMember != htxroles.Member {
		t.Fatalf("RoleHTXMember = %q", port.RoleHTXMember)
	}
	if got, want := slices.Contains(coopReaderRoles, htxroles.Member), htxroles.Can(htxroles.Member, "FN-03", htxroles.Read); got != want {
		t.Fatalf("htx_member ∈ coopReaderRoles = %v, ma trận FN-03 R = %v", got, want)
	}
	if slices.Contains(readerRoles, htxroles.Member) {
		t.Fatal("danh sách tổ chức/HTX chưa mở cho Thành viên HTX (chỉ giới hạn theo xã — AH-273)")
	}
	if slices.Contains(adminRoles, htxroles.Member) || slices.Contains(orgNodeRoles, htxroles.Member) {
		t.Fatal("Thành viên HTX không có quyền quản trị sổ HTX (FN-03 không có C/U/D)")
	}
	if !slices.Equal(coopReaderRoles[:len(readerRoles)], readerRoles) {
		t.Fatal("coopReaderRoles phải giữ nguyên các vai đang đọc HTX")
	}
}
