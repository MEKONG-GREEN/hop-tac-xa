package handler_test

import (
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
)

func closeBody(event, cost, note string) map[string]any {
	b := map[string]any{"expected_season_event_id": event, "gps": map[string]any{"source": "denied"}, "channel": "web"}
	if cost != "" {
		b["cost_vnd"] = cost
	}
	if note != "" {
		b["note"] = note
	}
	return b
}

// summarySOP phát hành SOP có việc "Tổng kết vụ" (số 6, giai đoạn cuối, đánh dấu tổng kết) và gắn giống OM5451.
func summarySOP(f *fixture) {
	f.t.Helper()
	steps := append(sopSteps(), map[string]any{"seq": 6, "stage_seq": 3, "task_seq": 2, "name": "Tổng kết vụ", "activity_code": "khac",
		"window": dateRange(98, 100), "mandatory": false, "season_summary": true})
	r := f.sopAdmin.adminPost("/v1/cropcycle/sops", sopBody("AH152 OM5451", map[string]any{"code": "SOP-" + ids.New().String()[30:], "steps": steps}))
	expect(f.t, r, http.StatusCreated, "tạo SOP")
	tmpl, version := str(r.body, "ref"), str(list(r.body, "versions")[0], "id")
	expect(f.t, f.sopAdmin.adminPost("/v1/cropcycle/sop-versions/"+version+"/publish", nil), http.StatusOK, "phát hành SOP")
	expect(f.t, f.sopAdmin.adminPost("/v1/cropcycle/variety-bindings", map[string]any{"variety_code": "OM5451", "template_ref": tmpl}), http.StatusOK, "gắn giống")
}

// AH-152 AC1/AC3/AC5/AC6: tổng kết tự tính, nhắc khai báo sản lượng, chặn hoàn thành khi chưa có sản lượng, hoàn thành
// vụ kèm chi phí/ghi chú, việc tổng kết tự Xong, bản chụp cố định, vụ đóng chặn ghi việc, sửa sản lượng cần lý do,
// không hủy khai báo; quyền, phát lại.
func TestAH152_SummaryAndClose(t *testing.T) {
	f := newFixture(t)
	summarySOP(f)
	f.householdLand("2", true)
	c := f.memberA()
	r := c.post(f.householdPath(), householdOpenBody(map[string]any{"area_ha": "1.50", "anchor_date": "2026-06-01"}))
	expect(t, r, http.StatusCreated, "mở vụ")
	season := str(r.body, "season", "season_ref")
	path := f.householdPath() + "/" + season
	event := str(r.body, "season", "current_event_id")

	// Bón thúc theo liều khuyến nghị (URE 50 kg/ha × 1,5 ha).
	expect(t, c.postKey(path+"/task-completions", key(), tickBody(3, event)), http.StatusCreated, "tích bón thúc")
	sum := c.get(path + "/summary")
	expect(t, sum, http.StatusOK, "tổng kết khi đang canh tác")
	if str(sum.body, "status") != "open" || sum.body["can_close"] != false || sum.body["harvest_reminder"] != false ||
		sum.body["mass_t"] != nil || num(sum.body, "summary_step_seq") != 6 {
		t.Fatalf("tổng kết khi đang canh tác: %v", sum.body)
	}
	expectCode(t, c.post(path+"/close", closeBody(event, "", "")), http.StatusConflict, "cropcycle.season_harvest_required", "chưa khai báo sản lượng")

	// Xong việc Thu hoạch (giai đoạn cuối, trừ việc tổng kết) mà chưa khai báo → nhắc khai báo sản lượng (AC4).
	expect(t, c.postKey(path+"/task-completions", key(), tickBody(5, event)), http.StatusCreated, "tích thu hoạch")
	sum = c.get(path + "/summary")
	if sum.body["harvest_reminder"] != true {
		t.Fatalf("nhắc khai báo: %v", sum.body)
	}
	expect(t, c.post(path+"/harvest", harvestBody(event, "9.75", "2026-09-20")), http.StatusCreated, "khai báo")
	cur := c.get(path)
	event = str(cur.body, "current_event_id")

	// AC1: năng suất, vật tư, số việc.
	sum = f.managerA().get(path + "/summary")
	expect(t, sum, http.StatusOK, "Đại diện xem tổng kết")
	mats := list(sum.body, "materials")
	tasks := sum.body["tasks"]
	if sum.body["can_close"] != true || str(sum.body, "mass_t") != "9.75" || str(sum.body, "yield_t_per_ha") != "6.50" ||
		len(mats) != 1 || str(mats[0], "name") != "URE" || str(mats[0], "qty") != "75" || str(mats[0], "unit") != "kg" ||
		num(tasks, "total") != 5 || num(tasks, "not_done") != 3 || num(tasks, "early")+num(tasks, "on_time")+num(tasks, "late") != 2 {
		t.Fatalf("tổng kết sau khai báo: %v", sum.body)
	}

	// AC5 và kiểm số ở máy chủ.
	expect(t, f.managerA().post(path+"/close", closeBody(event, "", "")), http.StatusForbidden, "Đại diện")
	expect(t, f.memberB().post(path+"/close", closeBody(event, "", "")), http.StatusNotFound, "HTX khác")
	for _, bad := range []map[string]any{closeBody(event, "-1", ""), closeBody(event, "1234567890123", ""), closeBody(event, "1.5", ""),
		closeBody(event, "", "Gọi 0912345678"), closeBody(event, "", strings.Repeat("a", 1001))} {
		if r := c.post(path+"/close", bad); r.status != http.StatusUnprocessableEntity {
			t.Fatalf("dữ liệu sai %v: %d %v", bad, r.status, r.body)
		}
	}
	expectCode(t, c.post(path+"/close", closeBody("01a00000-0000-7000-8000-000000000001", "", "")), http.StatusConflict, "cropcycle.task_season_changed", "phiên bản cũ")

	// AC6: hoàn thành vụ.
	k := key()
	body := closeBody(event, "15000000", "Vụ được mùa")
	r = c.postKey(path+"/close", k, body)
	expect(t, r, http.StatusCreated, "hoàn thành vụ")
	if str(r.body, "season", "status") != "closed" || str(r.body, "season", "closed_at") == "" || str(r.body, "summary", "cost_vnd") != "15000000" ||
		str(r.body, "summary", "note") != "Vụ được mùa" || str(r.body, "summary", "closed_at") == "" || str(r.body, "season", "harvest", "mass_t") != "9.75" {
		t.Fatalf("hoàn thành vụ: %v", r.body)
	}
	rp := c.postKey(path+"/close", k, body)
	if (rp.status != http.StatusOK && rp.status != http.StatusCreated) || str(rp.body, "season", "current_event_id") != str(r.body, "season", "current_event_id") {
		t.Fatalf("phát lại: %d %v", rp.status, rp.body)
	}
	expectCode(t, c.postKey(path+"/close", k, closeBody(event, "1", "")), http.StatusUnprocessableEntity, "idempotency_key_reused", "khóa dùng lại")
	expectCode(t, c.post(path+"/close", closeBody(str(r.body, "season", "current_event_id"), "", "")), http.StatusConflict, "cropcycle.season_state", "đã hoàn thành")
	if n := len(f.outbox("pf.cooperative.household_season.closed")); n != 1 {
		t.Fatalf("sự kiện closed: %d", n)
	}

	// Việc tổng kết thành Xong; vụ đóng không ghi việc.
	cl := c.get(path + "/checklist")
	if task := checklistTask(cl.body, 6); task["done_on"] == nil || task["can_complete"] != false {
		t.Fatalf("việc tổng kết: %v", task)
	}
	closedEvent := str(r.body, "season", "current_event_id")
	expectCode(t, c.postKey(path+"/task-completions", key(), tickBody(4, closedEvent)), http.StatusUnprocessableEntity, "cropcycle.task_season_state", "ghi việc vụ đóng")

	// Sửa sản lượng sau hoàn thành: bắt buộc lý do; bản chụp tổng kết giữ nguyên; không hủy khai báo.
	h1 := str(r.body, "season", "harvest", "harvest_id")
	expect(t, c.post(path+"/harvest/corrections", harvestEditBody(h1, "10", "2026-09-20", "")), http.StatusUnprocessableEntity, "thiếu lý do")
	e := c.post(path+"/harvest/corrections", harvestEditBody(h1, "10", "2026-09-20", "Cân lại tại kho của HTX"))
	expect(t, e, http.StatusOK, "sửa có lý do")
	if str(e.body, "season", "status") != "closed" || str(e.body, "season", "harvest", "mass_t") != "10.00" {
		t.Fatalf("sửa sau hoàn thành: %v", e.body)
	}
	sum = c.get(path + "/summary")
	if str(sum.body, "mass_t") != "9.75" || str(sum.body, "status") != "closed" || str(sum.body, "cost_vnd") != "15000000" {
		t.Fatalf("bản chụp cố định: %v", sum.body)
	}
	h2 := str(e.body, "harvest", "harvest_id")
	expectCode(t, c.post(path+"/harvest/void", map[string]any{"expected_harvest_id": h2, "reason": "Khai báo nhầm vụ khác", "channel": "web"}),
		http.StatusConflict, "cropcycle.season_state", "hủy khai báo vụ đóng")
	if v := f.actualKg(season); v == nil || *v != "10000.000" {
		t.Fatalf("yield_summary: %v", v)
	}

	// Dựng lại projection từ sổ: vụ vẫn "Đã hoàn thành vụ" với khai báo hiện hành.
	expect(t, f.platformAdmin().adminPost("/v1/cropcycle/projections/rebuild", map[string]any{}), http.StatusOK, "dựng lại projection")
	if g := c.get(path); str(g.body, "status") != "closed" || str(g.body, "harvest", "mass_t") != "10.00" || str(g.body, "closed_at") == "" {
		t.Fatalf("sau dựng lại: %v", g.body)
	}
	if v := f.actualKg(season); v == nil || *v != "10000.000" {
		t.Fatalf("yield_summary sau dựng lại: %v", v)
	}

	// Danh sách lọc theo trạng thái "Đã hoàn thành vụ".
	ls := c.get(f.householdPath() + "?status=closed")
	expect(t, ls, http.StatusOK, "lọc closed")
	if items := list(ls.body, "items"); len(items) != 1 || str(items[0], "status") != "closed" {
		t.Fatalf("lọc closed: %v", ls.body)
	}
}

// AH-152 AC2: còn việc giai đoạn cuối chưa Xong/Bỏ qua thì không hoàn thành vụ; quy trình không có việc tổng kết vẫn
// hoàn thành được; hai yêu cầu đồng thời chỉ một thành công.
func TestAH152_CloseLastStageAndConcurrency(t *testing.T) {
	f := newFixture(t)
	path, _ := harvestedSeason(t, f)
	c := f.memberA()
	event := str(c.get(path).body, "current_event_id")
	r := c.post(path+"/close", closeBody(event, "", ""))
	expectCode(t, r, http.StatusConflict, "cropcycle.season_last_stage_unresolved", "việc giai đoạn cuối")
	sum := c.get(path + "/summary")
	if us := list(sum.body, "unresolved_steps"); len(us) != 1 || int(us[0].(float64)) != 7 || sum.body["summary_step_seq"] != nil {
		t.Fatalf("việc chưa xong: %v", sum.body)
	}
	expect(t, c.postKey(path+"/task-completions", key(), tickBody(7, event)), http.StatusCreated, "tích phơi rơm")

	var wg sync.WaitGroup
	statuses := make([]int, 2)
	for i := range statuses {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			statuses[i] = c.post(path+"/close", closeBody(event, "", "")).status
		}(i)
	}
	wg.Wait()
	created := 0
	for _, s := range statuses {
		switch s {
		case http.StatusCreated:
			created++
		case http.StatusConflict:
		default:
			t.Fatalf("đồng thời: %v", statuses)
		}
	}
	if created != 1 {
		t.Fatalf("đồng thời: %v", statuses)
	}
	if str(c.get(path).body, "status") != "closed" {
		t.Fatal("vụ chưa đóng")
	}
}
