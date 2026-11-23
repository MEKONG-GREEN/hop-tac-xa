package handler_test

import (
	"net/http"
	"testing"
)

func adhocBody(event string, extra map[string]any) map[string]any {
	b := map[string]any{"expected_season_event_id": event, "stage_seq": 2, "name": "Bắt ốc bươu vàng", "activity_code": "khac",
		"state": "done", "done_on": "2026-09-29", "materials": []any{}, "gps": map[string]any{"source": "denied"}, "channel": "web"}
	for k, v := range extra {
		b[k] = v
	}
	return b
}

// AH-163: việc phát sinh ngoài quy trình — nhãn "Ngoài SOP", không tính tiến độ; kiểm như form chi tiết; quyền.
func TestAH163_AdhocTask(t *testing.T) {
	f, path, event := taskFixture(t)
	c := f.memberA()
	before := c.get(path + "/checklist")
	expect(t, before, http.StatusOK, "checklist trước")

	// AC3: thiếu nội dung, ngày sau hôm nay, liều âm → từ chối.
	for name, extra := range map[string]map[string]any{
		"thiếu nội dung":   {"name": "  "},
		"ngày sau hôm nay": {"done_on": "2099-01-01"},
		"liều âm":          {"materials": []map[string]any{{"name": "Vôi", "unit": "kg", "dose_per_ha": "-1"}}},
		"giai đoạn lạ":     {"stage_seq": 9},
		"loại việc lạ":     {"activity_code": "bay_bay"},
	} {
		expect(t, c.post(path+"/adhoc-records", adhocBody(event, extra)), http.StatusUnprocessableEntity, name)
	}
	// AC4: Đại diện chỉ xem; HTX khác không thấy.
	expect(t, f.managerA().post(path+"/adhoc-records", adhocBody(event, nil)), http.StatusForbidden, "đại diện")
	expect(t, f.memberB().post(path+"/adhoc-records", adhocBody(event, nil)), http.StatusNotFound, "HTX khác")

	// AC1, AC2: bản ghi gắn nhãn Ngoài SOP, số việc riêng, tiến độ không đổi.
	k := key()
	r := c.postKey(path+"/adhoc-records", k, adhocBody(event, map[string]any{"note": "Sau mưa lớn"}))
	expect(t, r, http.StatusCreated, "ghi việc phát sinh")
	if get(r.body, "record", "adhoc") != true || str(r.body, "record", "adhoc_name") != "Bắt ốc bươu vàng" || num(r.body, "record", "step_seq") <= 1000 ||
		num(r.body, "record", "stage_seq") != 2 || str(r.body, "record", "activity_code") != "khac" {
		t.Fatalf("bản ghi: %v", r.body)
	}
	if num(r.body, "checklist", "progress", "total") != num(before.body, "progress", "total") || num(r.body, "checklist", "progress", "done") != 0 {
		t.Fatalf("tiến độ phải giữ nguyên: %v", r.body["checklist"])
	}
	first := num(r.body, "record", "step_seq")
	expect(t, c.postKey(path+"/adhoc-records", k, adhocBody(event, map[string]any{"note": "Sau mưa lớn"})), http.StatusCreated, "phát lại")
	r2 := c.post(path+"/adhoc-records", adhocBody(event, map[string]any{"name": "Dặm lúa"}))
	expect(t, r2, http.StatusCreated, "việc phát sinh thứ hai")
	if num(r2.body, "record", "step_seq") != first+1 || len(list(r2.body, "checklist", "records")) != 2 {
		t.Fatalf("số việc riêng tăng dần: %v", r2.body)
	}
	// Sửa/xóa như nhật ký việc (AH-126).
	logRef, id := str(r2.body, "record", "log_ref"), str(r2.body, "record", "id")
	v := c.post(path+"/task-records/"+logRef+"/void", editBody(id, "Ghi nhầm việc phát sinh", nil))
	expect(t, v, http.StatusOK, "xóa việc phát sinh")
	if len(list(v.body, "checklist", "records")) != 1 {
		t.Fatalf("sau khi xóa: %v", v.body)
	}
}
