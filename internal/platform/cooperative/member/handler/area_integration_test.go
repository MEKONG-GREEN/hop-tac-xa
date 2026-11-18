package handler_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"

	coopport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/port"
	partyport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/party/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/migrations"
)

func TestAH50_HouseholdAreasAndScope(t *testing.T) {
	f := newFixture(t)
	tr := f.seedTree(f.root())
	mgr := f.as(f.tenant, tr.c1, partyport.RoleCoopManager)
	h := mgr.appDo(http.MethodPost, "/v1/households", farmer(tr.c1, "0907300050"))
	if h.status != http.StatusCreated {
		t.Fatalf("seed: %d %v", h.status, h.body)
	}
	hh := uuid.MustParse(str(h.body, "household", "household_ref"))
	pool := f.env.AdminPool(t, migrations.PF)
	seed := func(tid, coop uuid.UUID, area, status string, otherHousehold ...uuid.UUID) {
		t.Helper()
		ref := hh
		if len(otherHousehold) > 0 {
			ref = otherHousehold[0]
		}
		_, err := pool.Exec(context.Background(), `INSERT INTO pf_coop.season_current
		 (tenant_id, season_ref, plot_ref, plot_code, household_ref, cooperative_ref, commune_code,
		 season_code, season_year, variety_code, sop_template_ref, sop_version_id, sop_version_no,
		 sowing_date, expected_harvest_date, harvest_open_date, area_ha, status, current_event_id,
		 version, location_status, gps_source, on_behalf, opened_by, opened_at, harvest_id)
		 VALUES ($1,$2,$3,'AH50',$4,$5,'30001','DX',2026,'OM18',$6,$7,1,
		 '2026-01-01','2026-04-01','2026-03-20',$8::numeric,$9,$10,1,'missing','denied',false,$11,now(),
		 CASE WHEN $9='completed' THEN $12::uuid ELSE NULL END)`,
			tid, ids.New(), ids.New(), ref, coop, ids.New(), ids.New(), area, status, ids.New(), ids.New(), ids.New())
		if err != nil {
			t.Fatal(err)
		}
	}
	seed(f.tenant, tr.c1, "0.25", "open")
	seed(f.tenant, tr.c1, "0.50", "open")
	seed(f.tenant, tr.c1, "10", "completed")
	seed(f.tenant, tr.c1, "20", "cancelled")
	seed(f.tenant, tr.c2, "30", "open", ids.New())
	seed(ids.New(), tr.c1, "40", "open")
	path := "/v1/cooperatives/" + tr.c1.String() + "/households"
	assertArea := func(c caller, used, remaining string, allowed bool) {
		t.Helper()
		r := c.appDo(http.MethodGet, path, nil)
		if r.status != http.StatusOK || len(items(r)) != 1 {
			t.Fatalf("list: %d %v", r.status, r.body)
		}
		got := items(r)[0]
		if got["active_area_ha"] != used || got["remaining_area_ha"] != remaining || got["can_open_season"] != allowed {
			t.Fatalf("unexpected areas: %v", got)
		}
		if !allowed && got["open_season_blocked_reason"] != "Hết diện tích để mở vụ." {
			t.Fatalf("missing reason: %v", got)
		}
	}
	for _, c := range []caller{mgr, f.as(f.tenant, tr.c1, coopport.RoleHTXMember), f.as(f.tenant, uuid.Nil, authz.RolePlatformAdmin)} {
		assertArea(c, "0.75", "0.5", true)
	}
	outsider := f.as(f.tenant, tr.c2, coopport.RoleHTXMember).appDo(http.MethodGet, path, nil)
	if outsider.status != http.StatusNotFound || outsider.body["items"] != nil {
		t.Fatalf("cross-HTX leak: %d %v", outsider.status, outsider.body)
	}
	seed(f.tenant, tr.c1, "0.50", "open")
	assertArea(mgr, "1.25", "0", false)
	seed(f.tenant, tr.c1, "0.10", "open")
	assertArea(mgr, "1.35", "0", false)
}
