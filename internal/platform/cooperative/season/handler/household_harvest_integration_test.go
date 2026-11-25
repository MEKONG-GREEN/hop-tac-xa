package handler_test

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/hop-tac-xa/migrations"
)

func harvestBody(event, massT, day string) map[string]any {
	return map[string]any{"expected_season_event_id": event, "mass_t": massT, "harvest_date": day, "gps": map[string]any{"source": "denied"}, "channel": "web"}
}

// AH-100: khai báo sản lượng của vụ theo hộ — vụ "Đã thu hoạch", giải phóng diện tích, việc Thu hoạch (kể cả Bỏ qua)
// tự Xong theo ngày thu hoạch, việc theo mốc NTT tính theo ngày đó, so với dự kiến (AH-87), kiểm số/ngày/trạng thái/quyền,
// phát lại.
func TestAH100_DeclareHouseholdHarvest(t *testing.T) {
	f := newFixture(t)
	f.publishedSOP("AH100 OM5451", "OM5451")
	f.householdLand("2", true)
	c := f.memberA()
	// Quy trình của vụ: bỏ việc Thu hoạch số 5 (Bỏ qua), thêm việc Thu hoạch đợt 2 (số 6, không ngày) và việc Phơi rơm theo
	// mốc NTT (số 7, ngày 1–3 sau thu hoạch).
	changes := []map[string]any{
		{"kind": "add", "step": map[string]any{"name": "Thu hoạch đợt 2", "stage_seq": 3, "activity_code": "thu_hoach", "window": map[string]any{"kind": "none"}}},
		{"kind": "add", "step": map[string]any{"name": "Phơi rơm", "stage_seq": 3, "activity_code": "khac", "window": map[string]any{"kind": "date_range", "anchor": "ntt", "from": 1, "to": 3}}},
		{"kind": "remove", "step_seq": 5},
	}
	r := c.post(f.householdPath(), householdOpenBody(map[string]any{"area_ha": "1.50", "anchor_date": "2026-06-01", "plan_changes": changes}))
	expect(t, r, http.StatusCreated, "mở vụ")
	season := str(r.body, "season", "season_ref")
	path := f.householdPath() + "/" + season
	event := str(r.body, "season", "current_event_id")
	cl := c.get(path + "/checklist")
	expect(t, cl, http.StatusOK, "checklist")
	if str(checklistTask(cl.body, 5), "completion_status") != "skipped" || checklistTask(cl.body, 7)["from_date"] != nil {
		t.Fatalf("trước khai báo: %v / %v", checklistTask(cl.body, 5), checklistTask(cl.body, 7))
	}
	// Đất 2 ha, vụ 1,5 ha: vụ 1 ha khác chưa mở được tới khi khai báo giải phóng diện tích.
	other := householdOpenBody(map[string]any{"area_ha": "1.00", "anchor_date": "2026-10-01", "season_year": 2027})
	expectCode(t, c.post(f.householdPath(), other), http.StatusConflict, "cropcycle.household_area_exceeded", "chưa giải phóng")

	// AC3/AC4/AC7: kiểm số, ngày, quyền ở máy chủ.
	for _, bad := range []map[string]any{harvestBody(event, "-1", "2026-09-20"), harvestBody(event, "1.234", "2026-09-20"), harvestBody(event, "abc", "2026-09-20")} {
		if r := c.post(path+"/harvest", bad); r.status != http.StatusUnprocessableEntity {
			t.Fatalf("số sai %v: %d %v", bad["mass_t"], r.status, r.body)
		}
	}
	expect(t, c.post(path+"/harvest", harvestBody(event, "9", "2026-05-31")), http.StatusUnprocessableEntity, "trước mốc gốc")
	expect(t, c.post(path+"/harvest", harvestBody(event, "9", "2099-01-01")), http.StatusUnprocessableEntity, "sau hôm nay")
	expect(t, c.post(path+"/harvest", map[string]any{"expected_season_event_id": event, "mass_t": "9", "gps": map[string]any{"source": "denied"}}), http.StatusUnprocessableEntity, "thiếu ngày")
	expectCode(t, c.post(path+"/harvest", harvestBody(event, "0", "2026-09-20")), http.StatusConflict, "cropcycle.harvest_zero_confirmation_required", "0 cần xác nhận")
	expectCode(t, c.post(path+"/harvest", harvestBody("01a00000-0000-7000-8000-000000000001", "9", "2026-09-20")), http.StatusConflict, "cropcycle.task_season_changed", "phiên bản cũ")
	expect(t, f.managerA().post(path+"/harvest", harvestBody(event, "9", "2026-09-20")), http.StatusForbidden, "Đại diện")
	expect(t, f.memberB().post(path+"/harvest", harvestBody(event, "9", "2026-09-20")), http.StatusNotFound, "HTX khác")

	// AC1: khai báo 9,75 t ngày 20/09/2026.
	k := key()
	body := harvestBody(event, "9.75", "2026-09-20")
	r = c.postKey(path+"/harvest", k, body)
	expect(t, r, http.StatusCreated, "khai báo")
	harvestID := str(r.body, "harvest", "harvest_id")
	if str(r.body, "season", "status") != "completed" || str(r.body, "harvest", "mass_t") != "9.75" || str(r.body, "harvest", "harvest_date") != "2026-09-20" ||
		str(r.body, "harvest", "yield_t_per_ha") != "6.50" || str(r.body, "season", "harvest", "mass_t") != "9.75" {
		t.Fatalf("khai báo: %v", r.body)
	}
	auto := list(r.body, "auto_completed_steps")
	if len(auto) != 2 || auto[0] != float64(5) || auto[1] != float64(6) {
		t.Fatalf("việc Thu hoạch tự Xong: %v", auto)
	}
	// AC2: so với dự kiến (AH-87: 1,5 ha × 6 t/ha = 9,00 t).
	if str(r.body, "season", "yield_estimate", "rice_t") != "9.00" {
		t.Fatalf("dự kiến: %v", r.body["season"])
	}
	// Diện tích được giải phóng.
	expect(t, c.post(f.householdPath(), other), http.StatusCreated, "mở vụ khác sau khi giải phóng diện tích")
	// Việc Thu hoạch (kể cả Bỏ qua) Xong theo ngày thu hoạch; việc NTT tính theo ngày thu hoạch (AC6).
	cl = c.get(path + "/checklist")
	expect(t, cl, http.StatusOK, "checklist sau khai báo")
	for _, seq := range []int{5, 6} {
		if task := checklistTask(cl.body, seq); task["done_on"] != "2026-09-20" {
			t.Fatalf("việc %d: %v", seq, task)
		}
	}
	if task := checklistTask(cl.body, 7); task["from_date"] != "2026-09-21" || task["to_date"] != "2026-09-23" {
		t.Fatalf("việc theo NTT: %v", task)
	}
	var autoFlags int
	if err := pgtest.Get(t).AdminPool(t, migrations.PF).QueryRow(context.Background(), `SELECT count(*) FROM pf_coop.farm_log
 WHERE tenant_id=$1 AND season_ref=$2::uuid AND (task_completion->>'auto_harvest')::boolean`, f.tenant, season).Scan(&autoFlags); err != nil || autoFlags != 2 {
		t.Fatalf("cờ auto_harvest: %d %v", autoFlags, err)
	}
	// yield_summary có sản lượng thực tế.
	var actual string
	if err := pgtest.Get(t).AdminPool(t, migrations.PF).QueryRow(context.Background(), `SELECT actual_kg::text FROM pf_coop.yield_summary
 WHERE tenant_id=$1 AND season_ref=$2::uuid`, f.tenant, season).Scan(&actual); err != nil || actual != "9750.000" {
		t.Fatalf("yield_summary.actual_kg: %s %v", actual, err)
	}

	// Phát lại cùng khóa trả kết quả cũ; đổi nội dung cùng khóa bị từ chối.
	// Biên nhận HTTP trả nguyên phản hồi lần đầu (201); sổ không ghi thêm.
	r = c.postKey(path+"/harvest", k, body)
	if (r.status != http.StatusOK && r.status != http.StatusCreated) || str(r.body, "harvest", "harvest_id") != harvestID {
		t.Fatalf("phát lại: %d %v", r.status, r.body)
	}
	if r := c.postKey(path+"/harvest", k, harvestBody(event, "8", "2026-09-20")); r.status < 400 {
		t.Fatalf("đổi nội dung cùng khóa: %d %v", r.status, r.body)
	}
	// AC5: vụ không còn "Đang canh tác" → từ chối, hướng dẫn Sửa sản lượng.
	expectCode(t, c.post(path+"/harvest", harvestBody(str(r.body, "season", "current_event_id"), "8", "2026-09-20")), http.StatusConflict, "cropcycle.season_state", "đã thu hoạch")
	// Đọc vụ trả sản lượng thực tế cạnh dự kiến (cả Đại diện HTX).
	g := f.managerA().get(path)
	expect(t, g, http.StatusOK, "Đại diện xem")
	if str(g.body, "harvest", "mass_t") != "9.75" || str(g.body, "yield_estimate", "rice_t") != "9.00" || str(g.body, "status") != "completed" {
		t.Fatalf("Đại diện xem: %v", g.body)
	}
}

// AH-100 KH-19: sản lượng bằng 0 lưu được sau khi xác nhận.
func TestAH100_ZeroHarvestConfirmed(t *testing.T) {
	f := newFixture(t)
	f.publishedSOP("AH100 zero", "OM5451")
	f.householdLand("2", true)
	c := f.memberA()
	r := c.post(f.householdPath(), householdOpenBody(map[string]any{"area_ha": "0.50", "anchor_date": "2026-06-01"}))
	expect(t, r, http.StatusCreated, "mở vụ")
	path := f.householdPath() + "/" + str(r.body, "season", "season_ref")
	b := harvestBody(str(r.body, "season", "current_event_id"), "0", "2026-09-20")
	b["zero_confirmed"] = true
	r = c.post(path+"/harvest", b)
	expect(t, r, http.StatusCreated, "0 đã xác nhận")
	if str(r.body, "harvest", "mass_t") != "0.00" || r.body["harvest"].(map[string]any)["zero_confirmed"] != true {
		t.Fatalf("khai báo 0: %v", r.body)
	}
}

// AH-100: luồng sửa khai báo theo thửa cũ không dùng được cho khai báo của vụ theo hộ.
func TestAH100_LegacyCorrectionRejectsHouseholdHarvest(t *testing.T) {
	f := newFixture(t)
	f.publishedSOP("AH100 legacy", "OM5451")
	f.householdLand("2", true)
	c := f.memberA()
	r := c.post(f.householdPath(), householdOpenBody(map[string]any{"area_ha": "0.50", "anchor_date": "2026-06-01"}))
	expect(t, r, http.StatusCreated, "mở vụ")
	path := f.householdPath() + "/" + str(r.body, "season", "season_ref")
	r = c.post(path+"/harvest", harvestBody(str(r.body, "season", "current_event_id"), "3", "2026-09-20"))
	expect(t, r, http.StatusCreated, "khai báo")
	id := str(r.body, "harvest", "harvest_id")
	r = c.post("/v1/cropcycle/harvests/"+id+"/corrections", map[string]any{"mass_kg": "2000", "correction_reason": "Cân lại tại nhà máy xay"})
	if r.status != http.StatusNotFound {
		t.Fatalf("sửa qua luồng cũ: %d %v", r.status, r.body)
	}
}

// AH-100: hai lần khai báo đồng thời cùng vụ (khác Idempotency-Key) — chỉ một lần thành công, lần kia 409.
func TestAH100_ConcurrentHarvestIsUnique(t *testing.T) {
	f := newFixture(t)
	f.publishedSOP("AH100 concurrent", "OM5451")
	f.householdLand("2", true)
	c := f.memberA()
	r := c.post(f.householdPath(), householdOpenBody(map[string]any{"area_ha": "0.50", "anchor_date": "2026-06-01"}))
	expect(t, r, http.StatusCreated, "mở vụ")
	path := f.householdPath() + "/" + str(r.body, "season", "season_ref")
	event := str(r.body, "season", "current_event_id")
	var wg sync.WaitGroup
	statuses := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			statuses <- c.post(path+"/harvest", harvestBody(event, "3", "2026-09-20")).status
		}()
	}
	wg.Wait()
	close(statuses)
	got := map[int]int{}
	for s := range statuses {
		got[s]++
	}
	if got[201] != 1 || got[409] != 1 {
		t.Fatal(got)
	}
}
