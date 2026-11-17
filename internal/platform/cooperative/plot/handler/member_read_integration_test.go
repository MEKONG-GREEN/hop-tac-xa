package handler_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	coopport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/port"
)

// AH-39 (KHMV-FN-02 R, KH-02): Thành viên HTX đọc thửa và diện tích đất của HTX mình để mở vụ; không thấy HTX khác,
// chưa gắn HTX thì không thấy gì; không ghi thửa.
func TestKHMV_FN02_MemberReadsOwnCoopPlots(t *testing.T) {
	f := newFixture(t)
	nat := f.national()
	a1, _ := nat.createPlot("31117", f.coopA, rect(0, 0, 100, 100))
	b1, _ := nat.createPlot("31117", f.coopB, rect(600, 0, 100, 100))

	m := f.as(f.tenant, f.coopA, coopport.RoleHTXMember)
	r := m.appDo(http.MethodGet, "/v1/cooperatives/"+f.coopA.String()+"/plots", nil)
	if ids := featureIDs(r); r.status != http.StatusOK || len(ids) != 1 || ids[0] != a1.String() {
		t.Fatalf("Thành viên HTX A đọc thửa HTX A: %d %s", r.status, r.raw)
	}
	if str(list(r.body, "features")[0], "properties", "area_ha") == "" {
		t.Fatalf("thiếu diện tích: %s", r.raw)
	}
	r = m.appDo(http.MethodGet, "/v1/cooperatives/"+f.coopB.String()+"/plots", nil)
	if r.status != http.StatusForbidden || code(r) != "geo.out_of_scope" || len(list(r.body, "features")) != 0 {
		t.Fatalf("Thành viên HTX A đọc thửa HTX B: %d %s", r.status, r.raw)
	}
	r = m.appDo(http.MethodGet, "/v1/plots/"+b1.String(), nil)
	expect(t, r, http.StatusNotFound, "Thành viên HTX A đọc một thửa HTX B")
	if seen := featureIDs(m.appDo(http.MethodGet, bboxPath(1000), nil)); contains(seen, b1.String()) || !contains(seen, a1.String()) {
		t.Fatalf("khung nhìn của Thành viên HTX A: %v", seen)
	}

	// Chưa gắn HTX (fail-closed).
	none := f.as(f.tenant, uuid.Nil, coopport.RoleHTXMember)
	r = none.appDo(http.MethodGet, "/v1/cooperatives/"+f.coopA.String()+"/plots", nil)
	if r.status != http.StatusForbidden || len(list(r.body, "features")) != 0 {
		t.Fatalf("Thành viên HTX chưa gắn HTX: %d %s", r.status, r.raw)
	}

	// Không ghi thửa (dịch vụ).
	r = m.appDo(http.MethodPost, "/v1/plots", map[string]any{"commune_code": "31117", "cooperative_ref": f.coopA.String(), "geometry": rect(0, 300, 50, 50)})
	expect(t, r, http.StatusForbidden, "Thành viên HTX tạo thửa")
}
