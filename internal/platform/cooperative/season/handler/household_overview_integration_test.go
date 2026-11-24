package handler_test

import (
	"net/http"
	"testing"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
)

// AH-140: Đại diện HTX xem chỉ đọc danh sách vụ của HTX kèm tiến độ; lọc trạng thái, người mở vụ; phạm vi; ghi bị chặn.
func TestAH140_HouseholdSeasonOverview(t *testing.T) {
	f, path, event := taskFixture(t)
	member, manager := f.memberA(), f.managerA()
	base := "/v1/cooperatives/" + f.coopA.String() + "/household-seasons"
	r := manager.get(base)
	expect(t, r, http.StatusOK, "Đại diện xem danh sách")
	items := list(r.body, "items")
	if len(items) != 1 {
		t.Fatalf("danh sách: %v", r.body)
	}
	it := items[0]
	if str(it, "status") != "open" || str(it, "household_ref") != f.h1.String() || str(it, "progress", "percent") != "0.00" ||
		num(it, "progress", "due") != 1 || num(it, "progress", "overdue") != 1 || num(it, "progress", "total") != 5 {
		t.Fatalf("vụ: %v", it)
	}
	// Lọc theo trạng thái và Thành viên mở vụ.
	if n := len(list(manager.get(base+"?status=completed").body, "items")); n != 0 {
		t.Fatalf("lọc đã thu hoạch: %d", n)
	}
	if n := len(list(manager.get(base+"?opened_by="+str(it, "opened_by")).body, "items")); n != 1 {
		t.Fatalf("lọc theo Thành viên: %d", n)
	}
	if n := len(list(manager.get(base+"?opened_by="+ids.New().String()).body, "items")); n != 0 {
		t.Fatalf("lọc Thành viên khác: %d", n)
	}
	expect(t, manager.get(base+"?status=bogus"), http.StatusUnprocessableEntity, "trạng thái lạ")
	expect(t, member.get(base), http.StatusOK, "Thành viên xem được")
	// AC3: HTX khác bị từ chối; AC4: Đại diện gọi API ghi bị từ chối.
	if r := f.memberB().get(base); r.status != http.StatusNotFound && r.status != http.StatusForbidden {
		t.Fatalf("HTX khác: %d %v", r.status, r.body)
	}
	expect(t, manager.post(path+"/task-completions", tickBody(2, event)), http.StatusForbidden, "Đại diện không ghi")
}
