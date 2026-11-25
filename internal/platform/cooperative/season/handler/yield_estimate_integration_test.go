package handler_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/hop-tac-xa/migrations"
)

// expectedKg đọc yield_summary.expected_kg của vụ (chuỗi rỗng khi NULL).
func (f *fixture) expectedKg(season string) string {
	f.t.Helper()
	var v *string
	if err := pgtest.Get(f.t).AdminPool(f.t, migrations.PF).QueryRow(context.Background(),
		`SELECT expected_kg::text FROM pf_coop.yield_summary WHERE tenant_id=$1 AND season_ref=$2::uuid`, f.tenant, season).Scan(&v); err != nil {
		f.t.Fatal(err)
	}
	if v == nil {
		return ""
	}
	return *v
}

func (f *fixture) estimateRows(season string) int {
	f.t.Helper()
	var n int
	if err := pgtest.Get(f.t).AdminPool(f.t, migrations.PF).QueryRow(context.Background(),
		`SELECT count(*) FROM pf_coop.household_yield_estimate_event WHERE tenant_id=$1 AND season_ref=$2::uuid`, f.tenant, season).Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n
}

func estimate(r resp) map[string]any {
	if m, ok := r.body["yield_estimate"].(map[string]any); ok {
		return m
	}
	if m, ok := get(r.body, "season", "yield_estimate").(map[string]any); ok {
		return m
	}
	return nil
}

// AH-87: sản lượng lúa, rơm dự kiến của vụ theo hộ — tính lúc mở vụ (KH-05), Thành viên nhập/xóa, rơm tính lại theo lúa
// có hiệu lực, kiểm số ở máy chủ (KH-26), quyền, phiên bản, phát lại, yield_summary, sửa vụ đổi diện tích.
func TestAH87_HouseholdYieldEstimate(t *testing.T) {
	f := newFixture(t)
	f.publishedSOP("AH87 OM5451", "OM5451")
	f.householdLand("3", true)
	f.params.set(port.ParamConventionalYieldTPerHa, "6.5")
	admin := f.platformAdmin()
	expect(t, admin.adminPost("/v1/cropcycle/straw-factors", map[string]any{"variety_code": "OM5451", "percentage": "40"}), http.StatusCreated, "hệ số rơm")
	c := f.memberA()

	// AC1 (KH-05): mở vụ chụp năng suất quy ước 6,5 t/ha và hệ số rơm 40 % → lúa 1,25 × 6,5 = 8,125 → 8,13; rơm 3,25.
	r := c.post(f.householdPath(), householdOpenBody(map[string]any{"area_ha": "1.25", "anchor_date": "2026-09-29"}))
	expect(t, r, http.StatusCreated, "mở vụ")
	season := str(r.body, "season", "season_ref")
	path := f.householdPath() + "/" + season
	e := estimate(r)
	if e == nil || e["seq"] != float64(1) || e["is_rice"] != true || e["computed_rice_t"] != "8.13" || e["computed_straw_t"] != "3.25" ||
		e["rice_t"] != "8.13" || e["straw_t"] != "3.25" || e["entered_rice_t"] != nil || e["conventional_yield_t_per_ha"] != "6.5" || e["straw_pct"] != "40" {
		t.Fatalf("dự kiến lúc mở vụ: %v", e)
	}
	// AC4: ngày thu hoạch dự kiến là ngày đã lưu của vụ.
	if str(r.body, "season", "expected_harvest_date") == "" {
		t.Fatalf("ngày thu hoạch dự kiến: %v", r.body)
	}
	if got := f.expectedKg(season); got != "8130.000" {
		t.Fatalf("yield_summary.expected_kg sau mở vụ = %s", got)
	}
	// Đổi tham số sau khi mở vụ không làm đổi vụ đã mở.
	f.params.set(port.ParamConventionalYieldTPerHa, "9")
	if e := estimate(c.get(path)); e["computed_rice_t"] != "8.13" {
		t.Fatalf("giá trị đã chụp: %v", e)
	}

	// AC3 (KH-26): máy chủ từ chối số âm, không phải số, quá 2 chữ số lẻ, vượt giới hạn cột; lỗi theo trường.
	for _, bad := range []map[string]any{
		{"expected_seq": 1, "rice_t": "-1"}, {"expected_seq": 1, "rice_t": "abc"}, {"expected_seq": 1, "rice_t": "6.505"},
		{"expected_seq": 1, "straw_t": "100000000"}, {"expected_seq": 1, "rice_t": 6},
	} {
		r := c.post(path+"/yield-estimates", bad)
		if r.status != http.StatusUnprocessableEntity {
			t.Fatalf("%v: %d %v", bad, r.status, r.body)
		}
	}
	expect(t, c.post(path+"/yield-estimates", map[string]any{"rice_t": "7"}), http.StatusUnprocessableEntity, "thiếu expected_seq")
	// Phiên bản cũ, quyền, HTX khác.
	expectCode(t, c.post(path+"/yield-estimates", map[string]any{"expected_seq": 0, "rice_t": "7"}), http.StatusConflict, "cropcycle.yield_estimate_changed", "phiên bản cũ")
	expect(t, f.managerA().post(path+"/yield-estimates", map[string]any{"expected_seq": 1, "rice_t": "7"}), http.StatusForbidden, "Đại diện chỉ xem")
	expect(t, f.memberB().post(path+"/yield-estimates", map[string]any{"expected_seq": 1, "rice_t": "7"}), http.StatusNotFound, "HTX khác")
	if e := estimate(f.managerA().get(path)); e == nil || e["rice_t"] != "8.13" {
		t.Fatalf("Đại diện xem được: %v", e)
	}

	// Trả lời 23/11 câu 1: lúa nhập 10 → rơm hệ thống tính lại = 10 × 40 % = 4,00; nơi khác đọc giá trị nhập.
	key := ids.New().String()
	body := map[string]any{"expected_seq": 1, "rice_t": "10", "straw_t": nil}
	r = c.postKey(path+"/yield-estimates", key, body)
	expect(t, r, http.StatusOK, "nhập lúa")
	e = estimate(r)
	if e["seq"] != float64(2) || e["entered_rice_t"] != "10.00" || e["rice_t"] != "10.00" || e["computed_rice_t"] != "8.13" || e["computed_straw_t"] != "4.00" || e["straw_t"] != "4.00" {
		t.Fatalf("nhập lúa: %v", e)
	}
	if got := f.expectedKg(season); got != "10000.000" {
		t.Fatalf("yield_summary ưu tiên giá trị nhập: %s", got)
	}
	// Phát lại cùng khóa trả kết quả cũ; đổi nội dung cùng khóa bị từ chối.
	r = c.postKey(path+"/yield-estimates", key, body)
	expect(t, r, http.StatusOK, "phát lại")
	if estimate(r)["seq"] != float64(2) || f.estimateRows(season) != 2 {
		t.Fatalf("phát lại không ghi thêm: %v", r.body)
	}
	if r := c.postKey(path+"/yield-estimates", key, map[string]any{"expected_seq": 1, "rice_t": "11"}); r.status < 400 {
		t.Fatalf("đổi nội dung cùng khóa: %d %v", r.status, r.body)
	}

	// Rơm Thành viên nhập giữ nguyên khi lúa đổi; xóa lúa nhập → rơm hệ thống theo lúa hệ thống.
	r = c.post(path+"/yield-estimates", map[string]any{"expected_seq": 2, "rice_t": "10", "straw_t": "3.5"})
	expect(t, r, http.StatusOK, "nhập rơm")
	r = c.post(path+"/yield-estimates", map[string]any{"expected_seq": 3, "straw_t": "3.5"})
	expect(t, r, http.StatusOK, "xóa lúa nhập")
	e = estimate(r)
	if e["entered_rice_t"] != nil || e["rice_t"] != "8.13" || e["computed_straw_t"] != "3.25" || e["straw_t"] != "3.50" {
		t.Fatalf("xóa lúa nhập: %v", e)
	}
	if got := f.expectedKg(season); got != "8130.000" {
		t.Fatalf("yield_summary về giá trị tính: %s", got)
	}

	// Sửa vụ (AH-175) đổi diện tích: tính lại theo tham số đã chụp, giữ giá trị nhập.
	cur := c.get(path)
	r = c.post(path+"/corrections", map[string]any{"expected_season_event_id": str(cur.body, "current_event_id"), "reason": "Đo lại diện tích sau khi sạ", "area_ha": "2.00"})
	expect(t, r, http.StatusOK, "sửa diện tích")
	e = estimate(r)
	if e["seq"] != float64(5) || e["computed_rice_t"] != "13.00" || e["computed_straw_t"] != "5.20" || e["straw_t"] != "3.50" || e["area_ha"] != "2.00" {
		t.Fatalf("sau sửa diện tích: %v", e)
	}

	// Vụ đã dừng không sửa được sản lượng dự kiến.
	cur = c.get(path)
	expect(t, c.post(path+"/stop", map[string]any{"expected_season_event_id": str(cur.body, "current_event_id"), "reason": "Ngập úng mất trắng toàn bộ"}), http.StatusOK, "dừng vụ")
	expectCode(t, c.post(path+"/yield-estimates", map[string]any{"expected_seq": 5, "rice_t": "1"}), http.StatusConflict, "cropcycle.season_state", "vụ đã dừng")
}

// AH-87 câu 4: vụ không phải lúa không có rơm; gửi rơm bị từ chối cả lệnh, không lưu phần còn lại.
func TestAH87_NonRiceRejectsStraw(t *testing.T) {
	f := newFixture(t)
	f.householdLand("2", true)
	c := f.memberA()
	r := c.post(f.householdPath(), householdOpenBody(map[string]any{"crop_code": "CAF", "variety_code": "ARA", "area_ha": "0.50",
		"anchor_date": "2026-09-29", "self_made_plan": selfMadePlan(false)}))
	expect(t, r, http.StatusCreated, "mở vụ cà phê")
	path := f.householdPath() + "/" + str(r.body, "season", "season_ref")
	e := estimate(r)
	if e["is_rice"] != false || e["straw_pct"] != nil || e["computed_straw_t"] != nil || e["straw_t"] != nil || e["computed_rice_t"] != "3.00" {
		t.Fatalf("vụ không phải lúa: %v", e)
	}
	r = c.post(path+"/yield-estimates", map[string]any{"expected_seq": 1, "rice_t": "2", "straw_t": "1"})
	if r.status != http.StatusUnprocessableEntity || get(r.body, "errors") == nil && r.body["field"] != "straw_t" {
		t.Fatalf("gửi rơm cho vụ không phải lúa: %d %v", r.status, r.body)
	}
	if e := estimate(c.get(path)); e["seq"] != float64(1) || e["entered_rice_t"] != nil {
		t.Fatalf("không lưu phần còn lại: %v", e)
	}
	expect(t, c.post(path+"/yield-estimates", map[string]any{"expected_seq": 1, "rice_t": "2", "straw_t": nil}), http.StatusOK, "rơm rỗng hợp lệ")
}

// AH-87: vụ mở trước AH-87 (chưa có dòng trong sổ) đọc thì tính tại chỗ theo tham số hiện hành, không ghi; lần ghi
// đầu tiên chụp tham số.
func TestAH87_LegacySeasonComputedOnRead(t *testing.T) {
	f := newFixture(t)
	f.publishedSOP("AH87 legacy", "OM5451")
	f.householdLand("2", true)
	c := f.memberA()
	r := c.post(f.householdPath(), householdOpenBody(map[string]any{"area_ha": "1.00", "anchor_date": "2026-09-29"}))
	expect(t, r, http.StatusCreated, "mở vụ")
	season := str(r.body, "season", "season_ref")
	path := f.householdPath() + "/" + season
	// Giả lập vụ mở trước AH-87: sổ chỉ ghi thêm nên chỉ xóa được bằng siêu người dùng, tắt trigger trong giao dịch.
	ctx := context.Background()
	tx, err := pgtest.Get(t).AdminPool(t, migrations.PF).Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `SET LOCAL session_replication_role = replica`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM pf_coop.household_yield_estimate_event WHERE tenant_id=$1 AND season_ref=$2::uuid`, f.tenant, season); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	f.params.set(port.ParamConventionalYieldTPerHa, "7")
	e := estimate(c.get(path))
	if e == nil || e["seq"] != float64(0) || e["computed_rice_t"] != "7.00" || e["computed_straw_t"] != "2.45" || e["updated_at"] != nil {
		t.Fatalf("tính tại chỗ: %v", e)
	}
	if n := f.estimateRows(season); n != 0 {
		t.Fatalf("đọc không được ghi: %d", n)
	}
	r = c.post(path+"/yield-estimates", map[string]any{"expected_seq": 0, "rice_t": nil})
	expect(t, r, http.StatusOK, "lần ghi đầu")
	// seq do đầu chuỗi của sổ cấp (giả lập ở trên xóa dòng nhưng không lùi đầu chuỗi), chỉ cần đã ghi.
	if e := estimate(r); e["seq"] == float64(0) || e["conventional_yield_t_per_ha"] != "7" || f.estimateRows(season) != 1 {
		t.Fatalf("lần ghi đầu chụp tham số: %v", e)
	}
}
