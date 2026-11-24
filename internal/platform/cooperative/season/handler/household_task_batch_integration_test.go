package handler_test

import (
	"net/http"
	"testing"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
)

// AH-176: ghi một việc cho nhiều vụ cùng SOP gốc — mỗi vụ một bản ghi, vụ không ghi được bị bỏ qua kèm lý do, quyền.
func TestAH176_TaskBatch(t *testing.T) {
	f, path, event := taskFixture(t)
	c := f.memberA()
	second := c.post(f.householdPath(), householdOpenBody(map[string]any{"area_ha": "0.50", "anchor_date": "2026-09-29", "season_year": 2027}))
	expect(t, second, http.StatusCreated, "mở vụ thứ hai cùng SOP")
	path2, event2 := f.householdPath()+"/"+str(second.body, "season", "season_ref"), str(second.body, "season", "current_event_id")
	tick := c.post(path2+"/task-completions", tickBody(3, event2))
	expect(t, tick, http.StatusCreated, "vụ thứ hai đã xong việc 3")
	event2 = str(tick.body, "checklist", "current_event_id")

	// Danh sách vụ chọn được: cùng SOP gốc, việc 3 của vụ thứ hai đã xong.
	cand := c.get(path + "/task-batch-candidates?step_seq=3")
	expect(t, cand, http.StatusOK, "vụ có thể chọn")
	items := list(cand.body, "items")
	if len(items) != 2 {
		t.Fatalf("ứng viên: %v", cand.body)
	}
	for _, it := range items {
		src := get(it, "is_source") == true
		if src && get(it, "eligible") != true || !src && (get(it, "eligible") != false || str(it, "code") != "cropcycle.task_already_resolved") {
			t.Fatalf("ứng viên: %v", it)
		}
	}
	expect(t, c.get(path+"/task-batch-candidates?step_seq=0"), http.StatusUnprocessableEntity, "thiếu việc")
	expect(t, f.managerA().get(path+"/task-batch-candidates?step_seq=3"), http.StatusForbidden, "đại diện không ghi")

	s1, s2, h := path[len(f.householdPath())+1:], str(second.body, "season", "season_ref"), f.h1.String()
	unknown := ids.New().String()
	body := func(extra map[string]any) map[string]any {
		b := map[string]any{"step_seq": 3, "state": "done", "done_on": "2026-09-29", "note": "Bón thúc đợt 1", "materials": []any{}, "zero_confirmed": true,
			"gps": map[string]any{"source": "denied"}, "channel": "web", "seasons": []map[string]any{
				{"household_ref": h, "season_ref": s1, "expected_season_event_id": event},
				{"household_ref": h, "season_ref": s2, "expected_season_event_id": event2},
				{"household_ref": h, "season_ref": unknown, "expected_season_event_id": event},
			}}
		for k, v := range extra {
			b[k] = v
		}
		return b
	}
	// AC: kiểm dữ liệu chung một lần, quyền.
	expect(t, c.post(path+"/task-batches", body(map[string]any{"done_on": "2099-01-01"})), http.StatusUnprocessableEntity, "ngày sau hôm nay")
	expect(t, c.post(path+"/task-batches", body(map[string]any{"seasons": []any{}})), http.StatusUnprocessableEntity, "chưa chọn vụ")
	expect(t, f.managerA().post(path+"/task-batches", body(nil)), http.StatusForbidden, "đại diện")
	expect(t, f.memberB().post(path+"/task-batches", body(nil)), http.StatusNotFound, "HTX khác")

	// AC1, AC2: vụ nguồn ghi được; vụ đã xong và vụ lạ bị bỏ qua kèm lý do.
	k := key()
	r := c.postKey(path+"/task-batches", k, body(nil))
	expect(t, r, http.StatusOK, "ghi nhiều vụ")
	if num(r.body, "recorded") != 1 || num(r.body, "skipped") != 2 {
		t.Fatalf("kết quả: %v", r.body)
	}
	res := list(r.body, "results")
	if str(res[0], "status") != "recorded" || str(res[0], "record", "note") != "Bón thúc đợt 1" || num(res[0], "record", "step_seq") != 3 ||
		str(res[1], "code") != "cropcycle.task_already_resolved" || str(res[2], "code") != "cropcycle.batch_task_mismatch" {
		t.Fatalf("kết quả từng vụ: %v", res)
	}
	recordID := str(res[0], "record", "id")
	again := c.postKey(path+"/task-batches", k, body(nil))
	expect(t, again, http.StatusOK, "phát lại")
	if str(list(again.body, "results")[0], "record", "id") != recordID {
		t.Fatalf("phát lại tạo bản ghi mới: %v", again.body)
	}
	after := c.get(path + "/checklist")
	expect(t, after, http.StatusOK, "checklist sau khi ghi")
	if len(list(after.body, "records")) != 1 {
		t.Fatalf("vụ nguồn phải có đúng một bản ghi: %v", after.body)
	}
	// Ghi lần nữa với khóa mới: việc đã xong ở mọi vụ → bỏ qua hết.
	r = c.post(path+"/task-batches", body(map[string]any{"seasons": []map[string]any{{"household_ref": h, "season_ref": s1, "expected_season_event_id": str(after.body, "current_event_id")}}}))
	expect(t, r, http.StatusOK, "ghi lại việc đã xong")
	if num(r.body, "recorded") != 0 || str(list(r.body, "results")[0], "code") != "cropcycle.task_already_resolved" {
		t.Fatalf("việc đã xong: %v", r.body)
	}
}
