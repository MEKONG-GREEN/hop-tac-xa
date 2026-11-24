package handler_test

import (
	"net/http"
	"testing"
)

func selfMadePlan(withHarvest bool) map[string]any {
	steps := []map[string]any{
		{"seq": 1, "stage_seq": 1, "task_seq": 1, "name": "Làm đất", "activity_code": "lam_dat", "window": dateRange(-3, -1), "day_offset": 0, "window_days": 0, "mandatory": true},
		{"seq": 2, "stage_seq": 2, "task_seq": 1, "name": "Tưới nước", "activity_code": "tuoi_nuoc", "window": map[string]any{"kind": "none"}, "day_offset": 0, "window_days": 0, "mandatory": false},
	}
	if withHarvest {
		steps = append(steps, map[string]any{"seq": 3, "stage_seq": 2, "task_seq": 2, "name": "Thu hái", "activity_code": "thu_hoach", "window": dateRange(200, 210), "day_offset": 0, "window_days": 0, "mandatory": true})
	}
	return map[string]any{
		"stages": []map[string]any{{"seq": 1, "name": "Chuẩn bị", "from_day": -3, "to_day": 1}, {"seq": 2, "name": "Chăm sóc", "from_day": 2, "to_day": 250}},
		"steps":  steps,
	}
}

// AH-162 (KH-12): tự tạo quy trình khi loại cây chưa có SOP đang áp dụng; chỉ gắn vụ; kiểm như soạn SOP.
func TestAH162_SelfMadePlan(t *testing.T) {
	f := newFixture(t)
	f.publishedSOP("AH162 lúa", "OM5451")
	f.householdLand("3", true)
	c := f.memberA()
	coffee := func(extra map[string]any) map[string]any {
		b := householdOpenBody(map[string]any{"crop_code": "CAF", "variety_code": "ARA", "area_ha": "0.50", "anchor_date": "2026-09-29"})
		for k, v := range extra {
			b[k] = v
		}
		return b
	}
	// Loại cây đã có SOP đang áp dụng → phải chọn SOP.
	r := c.post(f.householdPath(), householdOpenBody(map[string]any{"area_ha": "0.50", "self_made_plan": selfMadePlan(false)}))
	expect(t, r, http.StatusConflict, "đã có SOP")
	if str(r.body, "code") != "cropcycle.sop_available" {
		t.Fatalf("code: %v", r.body)
	}
	// AC2: thứ tự không liên tục hoặc Từ > Đến → từ chối.
	bad := selfMadePlan(false)
	bad["steps"].([]map[string]any)[0]["seq"] = 5
	expect(t, c.post(f.householdPath(), coffee(map[string]any{"self_made_plan": bad})), http.StatusUnprocessableEntity, "thứ tự không liên tục")
	bad = selfMadePlan(false)
	bad["steps"].([]map[string]any)[0]["window"] = dateRange(3, 1)
	expect(t, c.post(f.householdPath(), coffee(map[string]any{"self_made_plan": bad})), http.StatusUnprocessableEntity, "Từ > Đến")
	expect(t, c.post(f.householdPath(), coffee(map[string]any{"self_made_plan": map[string]any{"stages": []any{}, "steps": []any{}}})), http.StatusUnprocessableEntity, "trống")

	// Xem trước theo quy trình tự tạo.
	hh := "/v1/cooperatives/" + f.coopA.String() + "/households/" + f.h1.String()
	p := c.post(hh+"/season-plan-preview", map[string]any{"crop_code": "CAF", "variety_code": "ARA", "area_ha": "0.50", "anchor_date": "2026-09-29", "self_made_plan": selfMadePlan(false)})
	expect(t, p, http.StatusOK, "xem trước")
	if len(list(p.body, "stages")) != 2 || get(p.body, "expected_harvest_date") != nil {
		t.Fatalf("xem trước: %v", p.body)
	}

	// AC1, AC3: mở vụ với quy trình tự tạo; không có việc Thu hoạch thì không có ngày thu hoạch dự kiến.
	r = c.post(f.householdPath(), coffee(map[string]any{"self_made_plan": selfMadePlan(false), "self_made_anchor": "Ngày trồng"}))
	expect(t, r, http.StatusCreated, "mở vụ tự tạo")
	if get(r.body, "season", "sop_snapshot", "self_made") != true || str(r.body, "season", "sop_snapshot", "anchor_name") != "Ngày trồng" ||
		get(r.body, "season", "expected_harvest_date") != nil || len(list(r.body, "season", "sop_snapshot", "steps")) != 2 {
		t.Fatalf("vụ tự tạo: %v", r.body)
	}
	path := f.householdPath() + "/" + str(r.body, "season", "season_ref")
	ch := c.get(path + "/checklist")
	expect(t, ch, http.StatusOK, "checklist")
	expect(t, c.post(path+"/task-completions", tickBody(1, str(ch.body, "current_event_id"))), http.StatusCreated, "ghi nhật ký vụ tự tạo")
	// Sao chép vụ tự tạo (AH-139) vẫn dùng được.
	expect(t, c.post(f.householdPath(), coffee(map[string]any{"copy_from_season_ref": str(r.body, "season", "season_ref"), "season_year": 2027, "anchor_date": "2027-01-10"})),
		http.StatusCreated, "sao chép vụ tự tạo")

	// Có việc Thu hoạch thì có ngày thu hoạch dự kiến.
	r = c.post(f.householdPath(), coffee(map[string]any{"self_made_plan": selfMadePlan(true), "area_ha": "0.25"}))
	expect(t, r, http.StatusCreated, "mở vụ tự tạo có thu hoạch")
	if str(r.body, "season", "expected_harvest_date") != "2027-04-17" {
		t.Fatalf("ngày thu hoạch dự kiến: %v", r.body)
	}
	// AC4: Đại diện HTX không có chức năng này.
	expect(t, f.managerA().post(f.householdPath(), coffee(map[string]any{"self_made_plan": selfMadePlan(false)})), http.StatusForbidden, "đại diện")
}
