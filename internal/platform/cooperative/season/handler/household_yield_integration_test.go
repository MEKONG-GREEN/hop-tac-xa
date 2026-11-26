package handler_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/hop-tac-xa/migrations"
)

// AH-153 AC1–AC3: Đại diện HTX xem sản lượng thực tế so với dự kiến — nhóm chờ khai báo (theo ngày dự kiến, cờ quá hạn)
// và đã khai báo (theo ngày thu hoạch), chênh lệch, tổng hợp toàn bộ bộ lọc, lọc Thành viên/mùa vụ, phân trang, phạm vi.
func TestAH153_HouseholdYields(t *testing.T) {
	f := newFixture(t)
	f.publishedSOP("AH153 OM5451", "OM5451")
	f.householdLand("3", true)
	c := f.memberA()
	open := func(area, anchor, code string) (path, event, riceT string) {
		t.Helper()
		r := c.post(f.householdPath(), householdOpenBody(map[string]any{"area_ha": area, "anchor_date": anchor, "season_code": code}))
		expect(t, r, http.StatusCreated, "mở vụ "+anchor)
		return f.householdPath() + "/" + str(r.body, "season", "season_ref"), str(r.body, "season", "current_event_id"), str(r.body, "season", "yield_estimate", "rice_t")
	}
	pathA, _, _ := open("1.00", "2026-06-01", "DX")
	pathB, _, riceB := open("0.50", "2026-07-01", "HT")
	pathC, eventC, _ := open("0.50", "2026-06-15", "DX")
	pathD, eventD, riceD := open("0.50", "2026-06-10", "DX")
	pathE, eventE, _ := open("0.25", "2026-06-20", "DX")
	// C: Thành viên nhập dự kiến 4 t rồi khai báo 3 t; D: khai báo 2,5 t theo dự kiến hệ thống; E: dừng vụ.
	expect(t, c.post(pathC+"/yield-estimates", map[string]any{"expected_seq": 1, "rice_t": "4.00"}), http.StatusOK, "nhập dự kiến C")
	expect(t, c.post(pathC+"/harvest", harvestBody(eventC, "3", "2026-09-20")), http.StatusCreated, "khai báo C")
	expect(t, c.post(pathD+"/harvest", harvestBody(eventD, "2.5", "2026-09-15")), http.StatusCreated, "khai báo D")
	expect(t, c.post(pathE+"/stop", map[string]any{"expected_season_event_id": eventE, "reason": "Ruộng bị ngập nặng, dừng vụ"}), http.StatusOK, "dừng E")

	// A giả lập vụ mở trước AH-87 (chưa có dòng sổ dự kiến): dự kiến tính tại chỗ theo năng suất quy ước hiện hành.
	ctx := context.Background()
	tx, err := pgtest.Get(t).AdminPool(t, migrations.PF).Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `SET LOCAL session_replication_role = replica`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM pf_coop.household_yield_estimate_event WHERE tenant_id=$1 AND season_ref=$2::uuid`, f.tenant, pathA[len(f.householdPath())+1:]); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	f.params.set(port.ParamConventionalYieldTPerHa, "7")

	manager := f.managerA()
	base := "/v1/cooperatives/" + f.coopA.String() + "/household-yields"
	ref := func(p string) string { return p[len(f.householdPath())+1:] }

	// Chờ khai báo: A (dự kiến 09/09, quá hạn) trước B (09/10).
	r := manager.get(base + "?state=pending")
	expect(t, r, http.StatusOK, "Đại diện xem chờ khai báo")
	items := list(r.body, "items")
	if len(items) != 2 || str(items[0], "season_ref") != ref(pathA) || str(items[1], "season_ref") != ref(pathB) ||
		items[0].(map[string]any)["overdue"] != true || items[1].(map[string]any)["overdue"] != false ||
		str(items[0], "expected_t") != "7.00" || items[0].(map[string]any)["actual_t"] != nil || str(items[0], "expected_source") != "computed" {
		t.Fatalf("chờ khai báo: %v", r.body)
	}
	// Tổng hợp trên toàn bộ bộ lọc (cả hai nhóm, bỏ vụ đã dừng).
	d := func(s string) decimal.Decimal { return decimal.RequireFromString(s) }
	totals := r.body["totals"]
	wantExpected := d("4.00").Add(d(riceD))
	if num(totals, "pending") != 2 || str(totals, "pending_expected_t") != d("7.00").Add(d(riceB)).StringFixed(2) || num(totals, "declared") != 2 || str(totals, "expected_t") != wantExpected.StringFixed(2) ||
		str(totals, "actual_t") != "5.50" || str(totals, "diff_t") != d("5.50").Sub(wantExpected).StringFixed(2) {
		t.Fatalf("tổng hợp: %v", totals)
	}

	// Đã khai báo: C (20/09) trước D (15/09); chênh lệch C = 3 − 4 = −1 t (−25 %).
	r = manager.get(base + "?state=declared")
	expect(t, r, http.StatusOK, "đã khai báo")
	items = list(r.body, "items")
	if len(items) != 2 || str(items[0], "season_ref") != ref(pathC) || str(items[1], "season_ref") != ref(pathD) ||
		str(items[0], "expected_t") != "4.00" || str(items[0], "expected_source") != "entered" || str(items[0], "actual_t") != "3.00" ||
		str(items[0], "diff_t") != "-1.00" || str(items[0], "diff_pct") != "-25.0" || str(items[0], "harvest_date") != "2026-09-20" ||
		str(items[1], "actual_t") != "2.50" || str(items[1], "diff_t") != d("2.50").Sub(d(riceD)).StringFixed(2) {
		t.Fatalf("đã khai báo: %v", r.body)
	}

	// Phân trang theo ngày.
	p1 := manager.get(base + "?state=pending&limit=1")
	next := str(p1.body, "next_cursor")
	if len(list(p1.body, "items")) != 1 || next == "" {
		t.Fatalf("trang 1: %v", p1.body)
	}
	p2 := manager.get(base + "?state=pending&limit=1&cursor=" + next)
	if items := list(p2.body, "items"); len(items) != 1 || str(items[0], "season_ref") != ref(pathB) || p2.body["next_cursor"] != nil {
		t.Fatalf("trang 2: %v", p2.body)
	}

	// Lọc mùa vụ và Thành viên.
	if items := list(manager.get(base+"?state=pending&season_code=HT&season_year=2026").body, "items"); len(items) != 1 || str(items[0], "season_ref") != ref(pathB) {
		t.Fatalf("lọc mùa vụ: %v", items)
	}
	opener := str(items[0], "opened_by")
	if n := len(list(manager.get(base+"?state=declared&opened_by="+opener).body, "items")); n != 2 {
		t.Fatalf("lọc Thành viên: %d", n)
	}
	other := manager.get(base + "?state=declared&opened_by=" + ids.New().String())
	if len(list(other.body, "items")) != 0 || num(other.body, "totals", "declared") != 0 {
		t.Fatalf("lọc Thành viên khác: %v", other.body)
	}

	// Sửa sản lượng thì thực tế đổi theo.
	hC := str(c.get(pathC).body, "harvest", "harvest_id")
	expect(t, c.post(pathC+"/harvest/corrections", harvestEditBody(hC, "3.20", "2026-09-20", "")), http.StatusOK, "sửa C")
	if it := list(manager.get(base+"?state=declared").body, "items")[0]; str(it, "actual_t") != "3.20" || str(it, "diff_t") != "-0.80" || str(it, "diff_pct") != "-20.0" {
		t.Fatalf("sau sửa: %v", it)
	}

	// Vụ đã hoàn thành vẫn thuộc nhóm đã khai báo.
	expect(t, c.post(pathD+"/close", closeBody(str(c.get(pathD).body, "current_event_id"), "", "")), http.StatusCreated, "hoàn thành D")
	if items := list(manager.get(base+"?state=declared").body, "items"); len(items) != 2 || str(items[1], "status") != "closed" || str(items[1], "actual_t") != "2.50" {
		t.Fatalf("vụ đã hoàn thành: %v", items)
	}

	// Kiểm tham số; phạm vi; Thành viên cũng xem được.
	expect(t, manager.get(base), http.StatusUnprocessableEntity, "thiếu state")
	expect(t, manager.get(base+"?state=bogus"), http.StatusUnprocessableEntity, "state lạ")
	expect(t, manager.get(base+"?state=pending&cursor=@@"), http.StatusUnprocessableEntity, "cursor lạ")
	expect(t, c.get(base+"?state=pending"), http.StatusOK, "Thành viên xem được")
	if r := f.memberB().get(base + "?state=pending"); r.status != http.StatusNotFound {
		t.Fatalf("HTX khác: %d %v", r.status, r.body)
	}
}
