package handler_test

import (
	"net/http"
	"testing"
)

// AH-139: sao chép giống và quy trình (kèm tùy chỉnh AH-113) từ vụ trước của hộ; vẫn kiểm diện tích, phạm vi.
func TestAH139_CopyFromPreviousSeason(t *testing.T) {
	f, path, event := taskFixture(t)
	c := f.memberA()
	src := path[len(f.householdPath())+1:]
	add := planChange(event, map[string]any{"kind": "add", "step": map[string]any{"name": "Xử lý giống", "stage_seq": 1,
		"activity_code": "khac", "window": map[string]any{"kind": "none"}}})
	expect(t, c.post(path+"/plan-changes", add), http.StatusOK, "tùy chỉnh vụ nguồn")

	copyBody := func() map[string]any {
		return householdOpenBody(map[string]any{"area_ha": "0.50", "anchor_date": "2027-01-10", "copy_from_season_ref": src, "season_year": 2027})
	}
	// Giống khác vụ nguồn → 422; vụ nguồn của HTX khác → 404.
	bad := copyBody()
	bad["variety_code"] = "KHAC"
	expect(t, c.post(f.householdPath(), bad), http.StatusUnprocessableEntity, "giống khác vụ nguồn")
	expect(t, f.memberB().post(f.householdPath(), copyBody()), http.StatusNotFound, "HTX khác")
	// AC2: diện tích vượt còn lại → từ chối.
	over := copyBody()
	over["area_ha"] = "5.00"
	expect(t, c.post(f.householdPath(), over), http.StatusConflict, "vượt diện tích")

	// AC1: vụ mới cùng giống, cùng quy trình kèm tùy chỉnh; ngày dự kiến theo mốc mới.
	r := c.post(f.householdPath(), copyBody())
	expect(t, r, http.StatusCreated, "sao chép")
	if str(r.body, "season", "copied_from") != src || str(r.body, "season", "variety_code") != "OM5451" ||
		len(list(r.body, "season", "sop_snapshot", "custom", "added")) != 1 || str(r.body, "season", "anchor_date") != "2027-01-10" {
		t.Fatalf("vụ sao chép: %v", r.body)
	}
	ch := c.get(f.householdPath() + "/" + str(r.body, "season", "season_ref") + "/checklist")
	expect(t, ch, http.StatusOK, "checklist vụ mới")
	added := checklistTask(ch.body, 6)
	if added == nil || added["custom"] != true || num(ch.body, "progress", "done") != 0 {
		t.Fatalf("quy trình vụ mới: %v", ch.body)
	}
	if str(checklistTask(ch.body, 1), "from_date") != "2027-01-07" {
		t.Fatalf("ngày dự kiến theo mốc mới: %v", checklistTask(ch.body, 1))
	}
	// Đại diện chỉ xem: không mở vụ bằng sao chép.
	expect(t, f.managerA().post(f.householdPath(), copyBody()), http.StatusForbidden, "đại diện")
}
