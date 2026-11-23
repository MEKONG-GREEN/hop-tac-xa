package handler_test

import (
	"net/http"
	"testing"
)

func planChange(event string, change map[string]any) map[string]any {
	return map[string]any{"expected_season_event_id": event, "change": change}
}

func dateWindow(from, to int) map[string]any {
	return map[string]any{"kind": "date_range", "anchor": "nxg", "from": from, "to": to}
}

// task tìm việc theo seq trong checklist trả về.
func checklistTask(body any, seq int) map[string]any {
	for _, s := range list(body, "progress", "stages") {
		for _, t := range list(s, "tasks") {
			if int(num(t, "seq")) == seq {
				m, _ := t.(map[string]any)
				return m
			}
		}
	}
	return nil
}

// AH-113 AC2–AC6: tùy chỉnh quy trình của vụ đang canh tác — thêm, sửa, đổi chỗ, bỏ; việc đã xong bị khóa; lịch sử.
func TestAH113_PlanChangesRunningSeason(t *testing.T) {
	f, path, event := taskFixture(t)
	c := f.memberA()
	expect(t, c.post(path+"/task-completions", tickBody(1, event)), http.StatusCreated, "việc 1 đã xong")

	// AC2: thêm việc vào giai đoạn 1 (đã có việc xong) — checklist tính lại, việc thêm có nhãn, tính vào tiến độ.
	k := key()
	add := planChange(event, map[string]any{"kind": "add", "step": map[string]any{"name": "Phun thuốc cỏ", "stage_seq": 1,
		"activity_code": "phun_thuoc", "window": dateWindow(0, 2), "materials": []map[string]any{{"name": "Thuốc cỏ", "dose_per_ha": "1.5", "unit": "l"}}}})
	r := c.postKey(path+"/plan-changes", k, add)
	expect(t, r, http.StatusOK, "thêm việc")
	if str(r.body, "summary") != `Thêm việc "Phun thuốc cỏ" vào giai đoạn "Chuẩn bị"` || num(r.body, "checklist", "progress", "total") != 6 {
		t.Fatalf("thêm việc: %v", r.body)
	}
	added := checklistTask(r.body["checklist"], 6)
	if added == nil || added["custom"] != true || added["can_complete"] != true || added["stage_seq"] != float64(1) {
		t.Fatalf("việc thêm: %v", added)
	}
	next := str(r.body, "season", "current_event_id")
	if next == event || get(r.body, "season", "sop_snapshot", "custom") == nil {
		t.Fatalf("vụ phải có phiên bản mới và lớp tùy chỉnh: %v", r.body)
	}
	// Phát lại cùng khóa: cùng kết quả, không ghi thêm lịch sử.
	replay := c.postKey(path+"/plan-changes", k, add)
	expect(t, replay, http.StatusOK, "phát lại")
	if str(replay.body, "season", "current_event_id") != next {
		t.Fatalf("phát lại tạo phiên bản khác: %v", replay.body)
	}
	// Phiên bản cũ → 409.
	r = c.post(path+"/plan-changes", planChange(event, map[string]any{"kind": "move", "step_seq": 2, "direction": "up"}))
	expect(t, r, http.StatusConflict, "phiên bản vụ cũ")
	if str(r.body, "code") != "cropcycle.task_season_changed" {
		t.Fatalf("code: %v", r.body)
	}
	// AC3: việc đã xong không sửa, đổi chỗ, bỏ được.
	for _, ch := range []map[string]any{
		{"kind": "edit", "step_seq": 1, "window": dateWindow(-2, -1)},
		{"kind": "move", "step_seq": 1, "direction": "down"},
		{"kind": "remove", "step_seq": 1},
	} {
		r = c.post(path+"/plan-changes", planChange(next, ch))
		expect(t, r, http.StatusConflict, "việc đã xong")
		if str(r.body, "code") != "cropcycle.plan_step_resolved" {
			t.Fatalf("%v: %v", ch, r.body)
		}
	}
	// Cửa sổ Từ > Đến bị chặn tại trường.
	expect(t, c.post(path+"/plan-changes", planChange(next, map[string]any{"kind": "edit", "step_seq": 3, "window": dateWindow(13, 10)})), http.StatusUnprocessableEntity, "Từ > Đến")
	// Sửa ngày việc 3: việc chưa xong tính lại.
	r = c.post(path+"/plan-changes", planChange(next, map[string]any{"kind": "edit", "step_seq": 3, "window": dateWindow(20, 22),
		"materials": []map[string]any{{"material_code": "URE", "dose_per_ha": "60", "unit": "kg"}}}))
	expect(t, r, http.StatusOK, "sửa ngày/vật tư")
	next = str(r.body, "season", "current_event_id")
	if str(checklistTask(r.body["checklist"], 3), "from_date") != "2026-10-19" {
		t.Fatalf("lịch việc 3: %v", checklistTask(r.body["checklist"], 3))
	}
	// Đổi chỗ chỉ đổi thứ tự hiển thị trong giai đoạn.
	r = c.post(path+"/plan-changes", planChange(next, map[string]any{"kind": "move", "step_seq": 6, "direction": "up"}))
	expect(t, r, http.StatusOK, "đổi chỗ")
	next = str(r.body, "season", "current_event_id")
	stage1 := list(list(r.body, "checklist", "progress", "stages")[0], "tasks")
	if num(stage1[0], "seq") != 1 || num(stage1[1], "seq") != 6 || num(stage1[2], "seq") != 2 {
		t.Fatalf("thứ tự giai đoạn 1: %v", stage1)
	}
	// AC4: bỏ việc SOP → Bỏ qua.
	r = c.post(path+"/plan-changes", planChange(next, map[string]any{"kind": "remove", "step_seq": 4}))
	expect(t, r, http.StatusOK, "bỏ việc SOP")
	next = str(r.body, "season", "current_event_id")
	if str(checklistTask(r.body["checklist"], 4), "completion_status") != "skipped" || num(r.body, "checklist", "progress", "skipped") != 1 {
		t.Fatalf("bỏ qua: %v", r.body)
	}
	// Việc tự thêm tích được; khi đã có nhật ký thì không xóa được.
	expect(t, c.post(path+"/task-completions", tickBody(6, next)), http.StatusCreated, "tích việc tự thêm")
	expect(t, c.post(path+"/plan-changes", planChange(next, map[string]any{"kind": "remove", "step_seq": 6})), http.StatusConflict, "việc tự thêm đã xong")

	// AC6: Đại diện chỉ xem; HTX khác không thấy.
	expect(t, f.managerA().post(path+"/plan-changes", planChange(next, map[string]any{"kind": "remove", "step_seq": 5})), http.StatusForbidden, "đại diện chỉ xem")
	expect(t, f.memberB().post(path+"/plan-changes", planChange(next, map[string]any{"kind": "remove", "step_seq": 5})), http.StatusNotFound, "HTX khác")

	// Lịch sử: ai, lúc nào, nội dung — mới nhất trước; Đại diện xem được.
	h := f.managerA().get(path + "/plan-history")
	expect(t, h, http.StatusOK, "lịch sử")
	items := list(h.body, "items")
	if len(items) != 4 || str(items[0], "summary") != `Bỏ việc "Phun thuốc" (Bỏ qua)` || str(items[3], "summary") != `Thêm việc "Phun thuốc cỏ" vào giai đoạn "Chuẩn bị"` ||
		str(items[0], "actor_ref") != c.actor.String() || str(items[0], "event_time") == "" {
		t.Fatalf("lịch sử: %v", h.body)
	}
	expect(t, f.memberB().get(path+"/plan-history"), http.StatusNotFound, "HTX khác không xem lịch sử")
}

// AH-113 AC1: tùy chỉnh trước khi lưu vụ — xem trước và bản chụp lưu đúng tùy chỉnh; SOP chuẩn không đổi.
func TestAH113_PlanChangesAtOpen(t *testing.T) {
	f := newFixture(t)
	_, version := f.publishedSOP("AH113 mở vụ", "OM5451")
	f.householdLand("2", true)
	changes := []map[string]any{
		{"kind": "add", "step": map[string]any{"name": "Xử lý giống", "stage_seq": 1, "activity_code": "khac", "window": map[string]any{"kind": "none"}}},
		{"kind": "move", "step_seq": 6, "direction": "up"},
		{"kind": "remove", "step_seq": 4},
	}
	hh := "/v1/cooperatives/" + f.coopA.String() + "/households/" + f.h1.String()
	p := f.memberA().post(hh+"/season-plan-preview", map[string]any{"crop_code": "LUA", "variety_code": "OM5451", "sop_version_id": version,
		"area_ha": "0.50", "anchor_date": "2026-09-29", "plan_changes": changes})
	expect(t, p, http.StatusOK, "xem trước có tùy chỉnh")
	stage1 := list(list(p.body, "stages")[0], "steps")
	if len(stage1) != 3 || num(stage1[1], "seq") != 6 || get(stage1[1], "custom") != true {
		t.Fatalf("xem trước: %v", p.body)
	}
	bad := f.memberA().post(f.householdPath(), householdOpenBody(map[string]any{"area_ha": "0.50", "plan_changes": []map[string]any{{"kind": "edit", "step_seq": 99, "window": map[string]any{"kind": "none"}}}}))
	expect(t, bad, http.StatusUnprocessableEntity, "thay đổi lỗi khi mở vụ")
	r := f.memberA().post(f.householdPath(), householdOpenBody(map[string]any{"area_ha": "0.50", "anchor_date": "2026-09-29", "plan_changes": changes}))
	expect(t, r, http.StatusCreated, "mở vụ có tùy chỉnh")
	path := f.householdPath() + "/" + str(r.body, "season", "season_ref")
	if len(list(r.body, "season", "sop_snapshot", "steps")) != 5 || len(list(r.body, "season", "sop_snapshot", "custom", "added")) != 1 {
		t.Fatalf("bản chụp: %v", r.body)
	}
	ch := f.memberA().get(path + "/checklist")
	expect(t, ch, http.StatusOK, "checklist")
	if num(ch.body, "progress", "total") != 6 || num(ch.body, "progress", "skipped") != 1 || str(checklistTask(ch.body, 4), "completion_status") != "skipped" {
		t.Fatalf("checklist sau mở vụ: %v", ch.body)
	}
	// Mở vụ không ghi lịch sử tùy chỉnh (tùy chỉnh nằm trong bản chụp lúc mở).
	h := f.memberA().get(path + "/plan-history")
	expect(t, h, http.StatusOK, "lịch sử")
	if len(list(h.body, "items")) != 0 {
		t.Fatalf("lịch sử: %v", h.body)
	}
	// SOP chuẩn không đổi.
	v := f.memberA().get("/v1/cropcycle/sop-versions/" + version)
	if v.status == http.StatusOK && len(list(v.body, "steps")) != 5 {
		t.Fatalf("SOP chuẩn bị đổi: %v", v.body)
	}
}
