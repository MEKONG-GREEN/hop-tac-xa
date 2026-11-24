package handler_test

import (
	"net/http"
	"testing"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
)

// AH-175: sửa mốc gốc, diện tích, giống/SOP của vụ đang canh tác kèm lý do; kiểm diện tích không tính chính vụ; đổi
// giống chỉ khi chưa có nhật ký; lịch sử; quyền.
func TestAH175_EditHouseholdSeason(t *testing.T) {
	f := newFixture(t)
	_, specific := f.publishedSOP("AH175 OM5451", "OM5451")
	_, generic := f.publishedSOP("AH175 generic", "")
	f.householdLand("2", true)
	c := f.memberA()
	r := c.post(f.householdPath(), householdOpenBody(map[string]any{"area_ha": "0.50", "anchor_date": "2026-09-29"}))
	expect(t, r, http.StatusCreated, "mở vụ")
	path := f.householdPath() + "/" + str(r.body, "season", "season_ref")
	event := str(r.body, "season", "current_event_id")
	if str(r.body, "season", "sop_snapshot", "version_id") != specific {
		t.Fatalf("SOP: %v", r.body)
	}
	edit := func(ev string, extra map[string]any) map[string]any {
		b := map[string]any{"expected_season_event_id": ev, "reason": "Sạ lại do mưa lớn đầu vụ", "channel": "web"}
		for k, v := range extra {
			b[k] = v
		}
		return b
	}

	// AC4/AC5: kiểm nhập, phiên bản, quyền.
	expect(t, c.post(path+"/corrections", edit(event, map[string]any{"reason": "ngắn", "anchor_date": "2026-10-05"})), http.StatusUnprocessableEntity, "lý do ngắn")
	expect(t, c.post(path+"/corrections", edit(event, nil)), http.StatusUnprocessableEntity, "không có gì để sửa")
	expect(t, c.post(path+"/corrections", edit(event, map[string]any{"area_ha": "0"})), http.StatusUnprocessableEntity, "diện tích 0")
	expect(t, c.post(path+"/corrections", edit(ids.New().String(), map[string]any{"anchor_date": "2026-10-05"})), http.StatusConflict, "phiên bản cũ")
	expect(t, f.managerA().post(path+"/corrections", edit(event, map[string]any{"anchor_date": "2026-10-05"})), http.StatusForbidden, "Đại diện")
	expect(t, f.memberB().post(path+"/corrections", edit(event, map[string]any{"anchor_date": "2026-10-05"})), http.StatusNotFound, "HTX khác")

	// AC1: đổi mốc gốc → ngày dự kiến tính lại (ngoài khung cũ vẫn được — Q-13).
	r = c.post(path+"/corrections", edit(event, map[string]any{"anchor_date": "2026-10-05"}))
	expect(t, r, http.StatusOK, "đổi mốc gốc")
	if str(r.body, "anchor_date") != "2026-10-05" || str(r.body, "expected_harvest_date") == "" {
		t.Fatalf("mốc gốc: %v", r.body)
	}
	event = str(r.body, "current_event_id")
	cl := c.get(path + "/checklist")
	expect(t, cl, http.StatusOK, "checklist")
	if task := list(get(list(cl.body, "progress", "stages")[0], "tasks"))[1]; str(task, "from_date") != "2026-10-05" {
		t.Fatalf("việc tính lại theo mốc mới: %v", task)
	}

	// AC3: diện tích tối đa = đất − vụ đang canh tác khác (không tính chính vụ).
	expect(t, c.post(f.householdPath(), householdOpenBody(map[string]any{"area_ha": "1.00", "anchor_date": "2026-09-29", "season_year": 2027})), http.StatusCreated, "vụ khác 1 ha")
	r = c.post(path+"/corrections", edit(event, map[string]any{"area_ha": "1.01"}))
	if r.status != http.StatusConflict || r.body["code"] != "cropcycle.household_area_exceeded" || r.body["remaining_area_ha"] != "1" {
		t.Fatalf("vượt diện tích: %d %v", r.status, r.body)
	}
	r = c.post(path+"/corrections", edit(event, map[string]any{"area_ha": "1.00"}))
	expect(t, r, http.StatusOK, "đúng diện tích tối đa")
	event = str(r.body, "current_event_id")

	// AC2: đổi giống bắt buộc chọn SOP theo giống mới; thay bản chụp.
	expect(t, c.post(path+"/corrections", edit(event, map[string]any{"variety_code": "ST25"})), http.StatusUnprocessableEntity, "thiếu SOP khi đổi giống")
	r = c.post(path+"/corrections", edit(event, map[string]any{"variety_code": "ST25", "sop_version_id": generic}))
	expect(t, r, http.StatusOK, "đổi giống")
	if str(r.body, "variety_code") != "ST25" || str(r.body, "sop_snapshot", "version_id") != generic {
		t.Fatalf("đổi giống: %v", r.body)
	}
	event = str(r.body, "current_event_id")
	// KH-10: có nhật ký thì không đổi giống/SOP.
	cl = c.get(path + "/checklist")
	expect(t, c.post(path+"/task-completions", tickBody(2, str(cl.body, "current_event_id"))), http.StatusCreated, "ghi nhật ký")
	r = c.post(path+"/corrections", edit(event, map[string]any{"variety_code": "OM5451", "sop_version_id": specific}))
	if r.status != http.StatusConflict || r.body["code"] != "cropcycle.season_has_logs" {
		t.Fatalf("có nhật ký: %d %v", r.status, r.body)
	}
	// Mọi lần sửa có lịch sử.
	h := c.get(path + "/plan-history")
	expect(t, h, http.StatusOK, "lịch sử")
	if n := len(list(h.body, "items")); n < 3 {
		t.Fatalf("lịch sử sửa vụ: %d %v", n, h.body)
	}
	// Vụ đã dừng không sửa được.
	expect(t, c.post(path+"/stop", map[string]any{"expected_season_event_id": event, "reason": "Ngập úng mất trắng toàn bộ"}), http.StatusOK, "dừng vụ")
	r = c.get(path)
	expect(t, c.post(path+"/corrections", edit(str(r.body, "current_event_id"), map[string]any{"anchor_date": "2026-10-06"})), http.StatusConflict, "vụ đã dừng")
}
