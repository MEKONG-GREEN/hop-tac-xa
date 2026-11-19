package handler_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	partyport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/party/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
)

func TestAH283HouseholdAreas(t *testing.T) {
	f := newFixture(t)
	root := f.root()
	tr := f.seedTree(root)
	read := func() map[uuid.UUID]partyport.CooperativeAreas {
		t.Helper()
		areas, err := f.svc.CooperativeAreas(tenant.WithID(context.Background(), f.tenant), []uuid.UUID{tr.c1, tr.c2})
		if err != nil {
			t.Fatal(err)
		}
		return areas
	}
	assertArea := func(a *decimal.Decimal, want string) {
		t.Helper()
		if a == nil || !a.Equal(decimal.RequireFromString(want)) {
			t.Fatalf("diện tích: %v, cần %s", a, want)
		}
	}
	initial := read()
	assertArea(initial[tr.c1].CultivatedHa, "0")
	assertArea(initial[tr.c1].LivestockHa, "0")
	body := farmer(tr.c1, "0907248301")
	body["cultivated_area_ha"], body["livestock_area_ha"] = "1.5", "0.5"
	r := root.appDo(http.MethodPost, "/v1/households", body)
	if r.status != http.StatusCreated || str(r.body, "household", "livestock_area_ha") != "0.5" {
		t.Fatalf("tạo hộ có diện tích chăn nuôi: %d %v", r.status, r.body)
	}
	areas := read()
	assertArea(areas[tr.c1].CultivatedHa, "1.5")
	assertArea(areas[tr.c1].LivestockHa, "0.5")
	assertArea(areas[tr.c2].CultivatedHa, "0")
	if areas[tr.c1].Households != 1 || areas[tr.c1].MissingLivestock != 0 {
		t.Fatalf("thống kê hộ: %+v", areas[tr.c1])
	}
	// Không đọc nhầm tenant dù cùng truyền ref HTX nguồn.
	foreign, err := f.svc.CooperativeAreas(tenant.WithID(context.Background(), ids.New()), []uuid.UUID{tr.c1})
	if err != nil || foreign[tr.c1].Households != 0 {
		t.Fatalf("tenant khác: %+v %v", foreign, err)
	}
	// Dữ liệu chưa khai không được ngầm trở thành 0 hoặc tổng một phần.
	legacy := farmer(tr.c1, "0907248302")
	delete(legacy, "cultivated_area_ha")
	if r := root.appDo(http.MethodPost, "/v1/households", legacy); r.status != http.StatusCreated {
		t.Fatalf("hộ chưa khai: %d %v", r.status, r.body)
	}
	a := read()[tr.c1]
	if a.Households != 2 || a.CultivatedHa != nil || a.LivestockHa != nil || a.MissingCultivated != 1 || a.MissingLivestock != 1 {
		t.Fatalf("không công bố tổng thiếu nguồn: %+v", a)
	}
}

func TestAH283LivestockAreaValidation(t *testing.T) {
	f := newFixture(t)
	root := f.root()
	tr := f.seedTree(root)
	for _, bad := range []string{"-0.1", "0.00001", "10000000000"} {
		body := farmer(tr.c1, "0907248303")
		body["livestock_area_ha"] = bad
		r := root.appDo(http.MethodPost, "/v1/households", body)
		if r.status != http.StatusUnprocessableEntity {
			t.Fatalf("diện tích %s: %d %v", bad, r.status, r.body)
		}
	}
	body := farmer(tr.c1, "0907248303")
	body["livestock_area_ha"] = "0"
	if r := root.appDo(http.MethodPost, "/v1/households", body); r.status != http.StatusCreated {
		t.Fatalf("chăn nuôi bằng 0 hợp lệ: %d %v", r.status, r.body)
	}
}
