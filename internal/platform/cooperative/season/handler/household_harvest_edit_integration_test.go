package handler_test

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/hop-tac-xa/migrations"
)

// harvestedSeason mở vụ 1,5 ha (đất 2 ha) có việc Thu hoạch số 5 bị bỏ qua trong kế hoạch, việc Thu hoạch đợt 2 (số 6)
// và việc Phơi rơm theo NTT (số 7), rồi khai báo 9,75 t ngày 20/09/2026.
func harvestedSeason(t *testing.T, f *fixture) (path, harvestID string) {
	t.Helper()
	f.publishedSOP("AH189 OM5451", "OM5451")
	f.householdLand("2", true)
	changes := []map[string]any{
		{"kind": "add", "step": map[string]any{"name": "Thu hoạch đợt 2", "stage_seq": 3, "activity_code": "thu_hoach", "window": map[string]any{"kind": "none"}}},
		{"kind": "add", "step": map[string]any{"name": "Phơi rơm", "stage_seq": 3, "activity_code": "khac", "window": map[string]any{"kind": "date_range", "anchor": "ntt", "from": 1, "to": 3}}},
		{"kind": "remove", "step_seq": 5},
	}
	c := f.memberA()
	r := c.post(f.householdPath(), householdOpenBody(map[string]any{"area_ha": "1.50", "anchor_date": "2026-06-01", "plan_changes": changes}))
	expect(t, r, http.StatusCreated, "mở vụ")
	path = f.householdPath() + "/" + str(r.body, "season", "season_ref")
	r = c.post(path+"/harvest", harvestBody(str(r.body, "season", "current_event_id"), "9.75", "2026-09-20"))
	expect(t, r, http.StatusCreated, "khai báo")
	return path, str(r.body, "harvest", "harvest_id")
}

func harvestEditBody(expected, massT, day, reason string) map[string]any {
	b := map[string]any{"expected_harvest_id": expected, "mass_t": massT, "harvest_date": day, "channel": "web"}
	if reason != "" {
		b["reason"] = reason
	}
	return b
}

// pubHarvestMasses đọc pubview v1_harvest_declaration của vụ trong ngữ cảnh tenant (view chạy theo quyền chủ, RLS lọc
// theo app.tenant_id).
func (f *fixture) pubHarvestMasses(season string) []string {
	f.t.Helper()
	ctx := context.Background()
	tx, err := pgtest.Get(f.t).AdminPool(f.t, migrations.PF).Begin(ctx)
	if err != nil {
		f.t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, f.tenant.String()); err != nil {
		f.t.Fatal(err)
	}
	rows, err := tx.Query(ctx, `SELECT mass_kg::text FROM pf_cropcycle_pub.v1_harvest_declaration WHERE stream_key = 'harvest:' || $1 ORDER BY seq`, season)
	if err != nil {
		f.t.Fatal(err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err != nil {
			f.t.Fatal(err)
		}
		out = append(out, m)
	}
	return out
}

func (f *fixture) actualKg(season string) *string {
	f.t.Helper()
	var v *string
	if err := pgtest.Get(f.t).AdminPool(f.t, migrations.PF).QueryRow(context.Background(),
		`SELECT actual_kg::text FROM pf_coop.yield_summary WHERE tenant_id=$1 AND season_ref=$2::uuid`, f.tenant, season).Scan(&v); err != nil {
		f.t.Fatal(err)
	}
	return v
}

// AH-189 AC1/AC5: sửa sản lượng giữ lịch sử cũ → mới; kiểm như khai báo; việc tự Xong đổi theo ngày mới; quyền.
func TestAH189_CorrectHouseholdHarvest(t *testing.T) {
	f := newFixture(t)
	path, h1 := harvestedSeason(t, f)
	c := f.memberA()
	season := str(c.get(path).body, "season_ref")

	expect(t, f.managerA().post(path+"/harvest/corrections", harvestEditBody(h1, "10", "2026-09-20", "")), http.StatusForbidden, "Đại diện")
	expect(t, f.memberB().post(path+"/harvest/corrections", harvestEditBody(h1, "10", "2026-09-20", "")), http.StatusNotFound, "HTX khác")
	expectCode(t, c.post(path+"/harvest/corrections", harvestEditBody("01a00000-0000-7000-8000-000000000001", "10", "2026-09-20", "")), http.StatusConflict, "cropcycle.concurrent_edit", "phiên bản cũ")
	expect(t, c.post(path+"/harvest/corrections", harvestEditBody(h1, "-1", "2026-09-20", "")), http.StatusUnprocessableEntity, "số âm")
	expect(t, c.post(path+"/harvest/corrections", harvestEditBody(h1, "10", "2026-05-31", "")), http.StatusUnprocessableEntity, "trước mốc gốc")
	expect(t, c.post(path+"/harvest/corrections", harvestEditBody(h1, "9.75", "2026-09-20", "")), http.StatusUnprocessableEntity, "không đổi gì")
	expect(t, c.post(path+"/harvest/corrections", harvestEditBody(h1, "10", "2026-09-20", "ngắn")), http.StatusUnprocessableEntity, "lý do ngắn")
	expectCode(t, c.post(path+"/harvest/corrections", harvestEditBody(h1, "0", "2026-09-20", "")), http.StatusConflict, "cropcycle.harvest_zero_confirmation_required", "0 cần xác nhận")

	// Sửa sản lượng và ngày, không nhập lý do (vụ "Đã thu hoạch").
	r := c.post(path+"/harvest/corrections", harvestEditBody(h1, "10.20", "2026-09-22", ""))
	expect(t, r, http.StatusOK, "sửa lần 1")
	h2 := str(r.body, "harvest", "harvest_id")
	if h2 == h1 || str(r.body, "harvest", "mass_t") != "10.20" || str(r.body, "season", "harvest", "harvest_date") != "2026-09-22" || str(r.body, "season", "status") != "completed" {
		t.Fatalf("sửa lần 1: %v", r.body)
	}
	if v := f.actualKg(season); v == nil || *v != "10200.000" {
		t.Fatalf("yield_summary sau sửa: %v", v)
	}
	// Việc Thu hoạch tự Xong đổi theo ngày mới; việc NTT tính theo ngày mới.
	cl := c.get(path + "/checklist")
	for _, seq := range []int{5, 6} {
		if task := checklistTask(cl.body, seq); task["done_on"] != "2026-09-22" {
			t.Fatalf("việc %d sau sửa ngày: %v", seq, task)
		}
	}
	if task := checklistTask(cl.body, 7); task["from_date"] != "2026-09-23" {
		t.Fatalf("việc NTT sau sửa ngày: %v", task)
	}
	// Bản sửa cũ không dùng lại được (phiên bản đã đổi); sửa lần 2 kèm lý do.
	expectCode(t, c.post(path+"/harvest/corrections", harvestEditBody(h1, "11", "2026-09-22", "")), http.StatusConflict, "cropcycle.concurrent_edit", "phiên bản cũ sau sửa")
	r = c.post(path+"/harvest/corrections", harvestEditBody(h2, "10.50", "2026-09-22", "Cân lại tại kho của HTX"))
	expect(t, r, http.StatusOK, "sửa lần 2")

	// Lịch sử cũ → mới, người, lý do (cả Đại diện xem được).
	hist := f.managerA().get(path + "/harvest/history")
	expect(t, hist, http.StatusOK, "lịch sử")
	items := list(hist.body, "items")
	if len(items) != 3 || str(items[0], "kind") != "declared" || str(items[1], "kind") != "corrected" || str(items[2], "kind") != "corrected" ||
		str(items[0], "mass_t") != "9.75" || str(items[1], "mass_t") != "10.20" || str(items[2], "mass_t") != "10.50" ||
		str(items[1], "reason") != "Thành viên HTX sửa sản lượng" || str(items[2], "reason") != "Cân lại tại kho của HTX" ||
		str(items[1], "supersedes_id") != h1 || str(items[2], "declared_by") == "" {
		t.Fatalf("lịch sử: %v", items)
	}
}

// AH-189 AC2/AC3/AC4: hủy khai báo — chặn vượt diện tích, vụ về "Đang canh tác", việc tự Xong về trạng thái trước,
// việc NTT về trạng thái trước khai báo, khai báo lại được; số liệu theo xã không đếm khai báo đã hủy.
func TestAH189_VoidHouseholdHarvest(t *testing.T) {
	f := newFixture(t)
	path, h1 := harvestedSeason(t, f)
	c := f.memberA()
	season := str(c.get(path).body, "season_ref")
	void := func(expected, reason string) map[string]any {
		return map[string]any{"expected_harvest_id": expected, "reason": reason, "channel": "web"}
	}
	reason := "Khai báo nhầm vụ của hộ khác"
	expect(t, c.post(path+"/harvest/void", void(h1, "")), http.StatusUnprocessableEntity, "thiếu lý do")
	expect(t, f.managerA().post(path+"/harvest/void", void(h1, reason)), http.StatusForbidden, "Đại diện")
	expectCode(t, c.post(path+"/harvest/void", void("01a00000-0000-7000-8000-000000000001", reason)), http.StatusConflict, "cropcycle.concurrent_edit", "phiên bản cũ")

	// AC3: sau khai báo, hộ mở vụ 1 ha khác; hủy sẽ làm 1,5 + 1 > 2 ha → từ chối.
	other := c.post(f.householdPath(), householdOpenBody(map[string]any{"area_ha": "1.00", "anchor_date": "2026-10-01", "season_year": 2027}))
	expect(t, other, http.StatusCreated, "mở vụ khác")
	r := c.post(path+"/harvest/void", void(h1, reason))
	if r.status != http.StatusConflict || r.body["code"] != "cropcycle.household_area_exceeded" || r.body["remaining_area_ha"] != "1" {
		t.Fatalf("hủy vượt diện tích: %d %v", r.status, r.body)
	}
	otherPath := f.householdPath() + "/" + str(other.body, "season", "season_ref")
	expect(t, c.post(otherPath+"/stop", map[string]any{"expected_season_event_id": str(other.body, "season", "current_event_id"), "reason": "Mở nhầm vụ, dừng để hủy khai báo"}), http.StatusOK, "dừng vụ khác")

	// AC2: hủy thành công.
	k := key()
	r = c.postKey(path+"/harvest/void", k, void(h1, reason))
	expect(t, r, http.StatusOK, "hủy khai báo")
	if str(r.body, "status") != "open" || r.body["harvest"] != nil {
		t.Fatalf("sau hủy: %v", r.body)
	}
	if v := f.actualKg(season); v != nil {
		t.Fatalf("yield_summary.actual_kg sau hủy: %v", *v)
	}
	cl := c.get(path + "/checklist")
	if task := checklistTask(cl.body, 5); task["completion_status"] != "skipped" || task["done_on"] != nil {
		t.Fatalf("việc Bỏ qua khôi phục: %v", task)
	}
	if task := checklistTask(cl.body, 6); task["done_on"] != nil {
		t.Fatalf("việc tự Xong được hoàn tác: %v", task)
	}
	if task := checklistTask(cl.body, 7); task["from_date"] != nil {
		t.Fatalf("việc NTT về trước khai báo: %v", task)
	}
	// Phát lại cùng khóa trả kết quả cũ.
	if again := c.postKey(path+"/harvest/void", k, void(h1, reason)); again.status != http.StatusOK || str(again.body, "status") != "open" {
		t.Fatalf("phát lại: %d %v", again.status, again.body)
	}
	// AC4 (trạng thái): vụ đang canh tác không sửa, không hủy được.
	expectCode(t, c.post(path+"/harvest/void", void(h1, reason)), http.StatusConflict, "cropcycle.season_state", "hủy lần 2")
	expectCode(t, c.post(path+"/harvest/corrections", harvestEditBody(h1, "9", "2026-09-20", "")), http.StatusConflict, "cropcycle.season_state", "sửa vụ đang canh tác")
	// Số liệu theo xã (pubview) không còn đếm khai báo đã hủy.
	if got := f.pubHarvestMasses(season); len(got) != 0 {
		t.Fatalf("pubview sau hủy: %v", got)
	}

	// Khai báo lại sau khi hủy.
	cur := c.get(path)
	r = c.post(path+"/harvest", harvestBody(str(cur.body, "current_event_id"), "8.40", "2026-09-25"))
	expect(t, r, http.StatusCreated, "khai báo lại")
	if task := checklistTask(c.get(path+"/checklist").body, 6); task["done_on"] != "2026-09-25" {
		t.Fatalf("việc Thu hoạch tự Xong lại: %v", task)
	}
	if got := f.pubHarvestMasses(season); len(got) != 1 || got[0] != "8400.000" {
		t.Fatalf("pubview sau khai báo lại: %v", got)
	}
	hist := c.get(path + "/harvest/history")
	items := list(hist.body, "items")
	if len(items) != 3 || str(items[0], "kind") != "declared" || str(items[1], "kind") != "voided" || str(items[2], "kind") != "declared" || str(items[1], "reason") != reason {
		t.Fatalf("lịch sử: %v", items)
	}
}

// AH-189: sửa và hủy đồng thời trên cùng khai báo — khóa vụ tuần tự hóa, một lần thành công, lần kia 409.
func TestAH189_ConcurrentEditAndVoid(t *testing.T) {
	f := newFixture(t)
	path, h1 := harvestedSeason(t, f)
	c := f.memberA()
	var wg sync.WaitGroup
	statuses := make(chan int, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		statuses <- c.post(path+"/harvest/corrections", harvestEditBody(h1, "10", "2026-09-20", "")).status
	}()
	go func() {
		defer wg.Done()
		statuses <- c.post(path+"/harvest/void", map[string]any{"expected_harvest_id": h1, "reason": "Khai báo nhầm vụ của hộ khác"}).status
	}()
	wg.Wait()
	close(statuses)
	got := map[int]int{}
	for s := range statuses {
		got[s]++
	}
	if got[200] != 1 || got[409] != 1 {
		t.Fatal(got)
	}
}
