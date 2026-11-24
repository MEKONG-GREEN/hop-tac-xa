package handler_test

import (
	"net/http"
	"testing"
)

// AH-70: dừng vụ đang canh tác kèm lý do — giải phóng diện tích, giữ dữ liệu, vụ đã dừng không ghi được; quyền.
func TestAH70_StopHouseholdSeason(t *testing.T) {
	f, path, event := taskFixture(t)
	c := f.memberA()
	stop := func(reason, ev string) map[string]any {
		return map[string]any{"expected_season_event_id": ev, "reason": reason, "channel": "web"}
	}
	expect(t, c.post(path+"/task-completions", tickBody(2, event)), http.StatusCreated, "nhật ký trước khi dừng")
	big := householdOpenBody(map[string]any{"area_ha": "2.00", "anchor_date": "2026-09-29", "season_year": 2027})
	r := c.post(f.householdPath(), big)
	if r.status != http.StatusConflict || r.body["code"] != "cropcycle.household_area_exceeded" {
		t.Fatalf("diện tích còn bị vụ chiếm: %d %v", r.status, r.body)
	}

	// AC2: thiếu lý do, phiên bản cũ; AC4: Đại diện không dừng được, HTX khác không thấy.
	expect(t, c.post(path+"/stop", stop("ngắn", event)), http.StatusUnprocessableEntity, "lý do quá ngắn")
	expect(t, c.post(path+"/stop", stop("Liên hệ 0912345678 để biết", event)), http.StatusUnprocessableEntity, "lý do chứa SĐT")
	expect(t, c.post(path+"/stop", stop("Ngập úng mất trắng toàn bộ", key())), http.StatusConflict, "phiên bản vụ cũ")
	expect(t, f.managerA().post(path+"/stop", stop("Ngập úng mất trắng toàn bộ", event)), http.StatusForbidden, "Đại diện")
	expect(t, f.memberB().post(path+"/stop", stop("Ngập úng mất trắng toàn bộ", event)), http.StatusNotFound, "HTX khác")

	// AC1: dừng vụ → "Đã dừng vụ", có lý do; phát lại cùng khóa trả kết quả cũ.
	k := key()
	r = c.postKey(path+"/stop", k, stop("Ngập úng mất trắng toàn bộ", event))
	expect(t, r, http.StatusOK, "dừng vụ")
	if str(r.body, "status") != "cancelled" || str(r.body, "stop_reason") != "Ngập úng mất trắng toàn bộ" {
		t.Fatalf("vụ sau khi dừng: %v", r.body)
	}
	stopped := str(r.body, "current_event_id")
	r = c.postKey(path+"/stop", k, stop("Ngập úng mất trắng toàn bộ", event))
	expect(t, r, http.StatusOK, "phát lại")
	if str(r.body, "current_event_id") != stopped {
		t.Fatalf("phát lại tạo sự kiện mới: %v", r.body)
	}
	// AC2/AC3: dừng lần nữa bị từ chối; vụ đã dừng không ghi nhật ký; dữ liệu giữ nguyên, đọc được.
	expect(t, c.post(path+"/stop", stop("Ngập úng mất trắng toàn bộ", stopped)), http.StatusConflict, "vụ đã dừng")
	expect(t, c.post(path+"/task-completions", tickBody(3, stopped)), http.StatusUnprocessableEntity, "không ghi nhật ký vụ đã dừng")
	cl := c.get(path + "/checklist")
	expect(t, cl, http.StatusOK, "nhật ký vẫn đọc được")
	if len(list(cl.body, "records")) != 1 {
		t.Fatalf("dữ liệu phải giữ nguyên: %v", cl.body)
	}
	expect(t, f.managerA().get(path), http.StatusOK, "Đại diện xem vụ đã dừng")

	// AC1: diện tích được giải phóng — mở được vụ dùng hết đất.
	expect(t, c.post(f.householdPath(), big), http.StatusCreated, "mở vụ trên diện tích đã giải phóng")
}
