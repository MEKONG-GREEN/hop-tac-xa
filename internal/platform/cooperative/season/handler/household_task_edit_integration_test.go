package handler_test

import (
	"net/http"
	"testing"
)

func editBody(expected, reason string, extra map[string]any) map[string]any {
	b := map[string]any{"expected_record_id": expected, "reason": reason, "channel": "web"}
	for k, v := range extra {
		b[k] = v
	}
	return b
}

// AH-126: sửa, xóa mềm, bỏ tích nhật ký việc kèm lý do; lịch sử chỉ ghi thêm; quyền.
func TestAH126_EditVoidHistory(t *testing.T) {
	f, path, event := taskFixture(t)
	c := f.memberA()
	r := c.post(path+"/task-completions", tickBody(3, event))
	expect(t, r, http.StatusCreated, "tích việc 3")
	first := str(r.body, "record", "id")
	logRef := str(r.body, "record", "log_ref")
	doneOn := str(r.body, "record", "done_on")
	rec := path + "/task-records/" + logRef
	edit := map[string]any{"state": "partial", "done_on": doneOn, "materials": []any{}, "note": "Mới bón một nửa"}

	// AC5: thiếu lý do hoặc lý do quá ngắn → từ chối.
	expect(t, c.post(rec+"/corrections", editBody(first, "", edit)), http.StatusUnprocessableEntity, "thiếu lý do")
	expect(t, c.post(rec+"/corrections", editBody(first, "ngắn", edit)), http.StatusUnprocessableEntity, "lý do ngắn")
	expect(t, c.post(rec+"/void", editBody(first, "", nil)), http.StatusUnprocessableEntity, "xóa thiếu lý do")
	// AC6: Đại diện chỉ xem; HTX khác không thấy.
	expect(t, f.managerA().post(rec+"/corrections", editBody(first, "Sửa lại ngày làm cho đúng", edit)), http.StatusForbidden, "đại diện")
	expect(t, f.memberB().post(rec+"/corrections", editBody(first, "Sửa lại ngày làm cho đúng", edit)), http.StatusNotFound, "HTX khác")
	// Ngày làm xong sau ngày ghi bị chặn như khi ghi.
	expect(t, c.post(rec+"/corrections", editBody(first, "Sửa lại ngày làm cho đúng", map[string]any{"state": "done", "done_on": "2099-01-01", "materials": []any{}})),
		http.StatusUnprocessableEntity, "ngày sau hôm nay")

	// AC2: sửa kèm lý do → bản ghi bù, việc chưa xong; GPS và thời điểm ghi giữ nguyên.
	r = c.post(rec+"/corrections", editBody(first, "Thực tế mới bón một nửa diện tích", edit))
	expect(t, r, http.StatusOK, "sửa")
	second := str(r.body, "record", "id")
	if second == first || str(r.body, "record", "log_ref") != logRef || str(r.body, "record", "state") != "partial" || num(r.body, "record", "version") != 2 ||
		str(r.body, "record", "correction_reason") != "Thực tế mới bón một nửa diện tích" {
		t.Fatalf("bản ghi bù: %v", r.body)
	}
	if str(checklistTask(r.body["checklist"], 3), "completion_status") == "done_on_time" || checklistTask(r.body["checklist"], 3)["can_complete"] != true {
		t.Fatalf("việc 3 phải về chưa xong: %v", checklistTask(r.body["checklist"], 3))
	}
	// Phiên bản cũ → 409.
	r = c.post(rec+"/corrections", editBody(first, "Sửa lần nữa theo bản cũ", edit))
	expect(t, r, http.StatusConflict, "phiên bản cũ")
	if str(r.body, "code") != "cropcycle.task_record_changed" {
		t.Fatalf("code: %v", r.body)
	}
	r = c.post(rec+"/corrections", editBody(second, "Đã bón đủ phần còn lại", map[string]any{"state": "done", "done_on": doneOn, "materials": []any{}}))
	expect(t, r, http.StatusOK, "sửa về đã xong")
	third := str(r.body, "record", "id")

	// AC4: bỏ tích kèm lý do → bản ghi ẩn, việc về chưa xong.
	k := key()
	r = c.postKey(rec+"/void", k, editBody(third, "Tích nhầm việc, chưa làm", nil))
	expect(t, r, http.StatusOK, "bỏ tích")
	if str(r.body, "record", "state") != "voided" || checklistTask(r.body["checklist"], 3)["can_complete"] != true {
		t.Fatalf("bỏ tích: %v", r.body)
	}
	for _, rr := range list(r.body, "checklist", "records") {
		if str(rr, "log_ref") == logRef {
			t.Fatal("bản ghi đã xóa còn trong checklist")
		}
	}
	expect(t, c.postKey(rec+"/void", k, editBody(third, "Tích nhầm việc, chưa làm", nil)), http.StatusOK, "phát lại cùng khóa")
	r = c.post(rec+"/void", editBody(str(r.body, "record", "id"), "Xóa lần nữa cho chắc", nil))
	expect(t, r, http.StatusConflict, "đã xóa")
	if str(r.body, "code") != "cropcycle.task_record_voided" {
		t.Fatalf("code: %v", r.body)
	}

	// Lịch sử: đủ 4 phiên bản, Đại diện xem được; GPS và thời điểm ghi không đổi.
	h := f.managerA().get(rec + "/history")
	expect(t, h, http.StatusOK, "lịch sử")
	items := list(h.body, "items")
	if len(items) != 4 {
		t.Fatalf("lịch sử: %v", h.body)
	}
	for i, want := range []string{"done", "partial", "done", "voided"} {
		if str(items[i], "state") != want || num(items[i], "version") != float64(i+1) || str(items[i], "event_time") != str(items[0], "event_time") ||
			get(items[i], "gps_missing") != get(items[0], "gps_missing") {
			t.Fatalf("phiên bản %d: %v", i+1, items[i])
		}
	}
	if str(items[1], "note") != "Mới bón một nửa" || str(items[3], "correction_reason") != "Tích nhầm việc, chưa làm" || str(items[0], "correction_reason") != "" {
		t.Fatalf("nội dung lịch sử: %v", items)
	}
	expect(t, f.memberB().get(rec+"/history"), http.StatusNotFound, "HTX khác không xem lịch sử")
	// Việc lại tích được sau khi bỏ tích.
	ch := c.get(path + "/checklist")
	expect(t, ch, http.StatusOK, "checklist")
	expect(t, c.post(path+"/task-completions", tickBody(3, str(ch.body, "current_event_id"))), http.StatusCreated, "tích lại")
}
