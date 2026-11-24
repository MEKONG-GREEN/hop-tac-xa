package handler_test

import (
	"net/http"
	"testing"
)

// AH-124: chi tiết vụ — % tiến độ, số việc đến hạn, số việc trễ; Đại diện xem được; HTX khác bị từ chối.
func TestAH124_ChecklistDueOverdueCounts(t *testing.T) {
	f, path, event := taskFixture(t)
	c := f.memberA()
	r := c.get(path + "/checklist")
	expect(t, r, http.StatusOK, "checklist")
	// 29/09 là ngày xuống giống: "Gieo sạ" (0..1) đến hạn, "Làm đất" (-3..-1) trễ.
	if num(r.body, "progress", "due") != 1 || num(r.body, "progress", "overdue") != 1 || str(r.body, "progress", "percent") != "0.00" {
		t.Fatalf("tiến độ: %v", r.body["progress"])
	}
	expect(t, c.post(path+"/task-completions", tickBody(2, event)), http.StatusCreated, "ghi xong Gieo sạ")
	r = f.managerA().get(path + "/checklist")
	expect(t, r, http.StatusOK, "Đại diện xem")
	if num(r.body, "progress", "due") != 0 || num(r.body, "progress", "overdue") != 1 || str(r.body, "progress", "percent") != "20.00" {
		t.Fatalf("sau khi ghi: %v", r.body["progress"])
	}
	expect(t, f.memberB().get(path+"/checklist"), http.StatusNotFound, "HTX khác")
}
