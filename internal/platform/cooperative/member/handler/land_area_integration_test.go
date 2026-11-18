package handler_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	partyport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/party/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
	"github.com/MEKONG-GREEN/hop-tac-xa/migrations"
)

// The legacy KN write route is closed. Exercise the remaining service directly
// so a future caller cannot reduce land below already allocated season area.
func TestAH61_HouseholdLandCannotDropBelowOpenSeasons(t *testing.T) {
	f := newFixture(t)
	tr := f.seedTree(f.root())
	r := f.as(f.tenant, tr.c1, partyport.RoleCoopManager).appDo(http.MethodPost, "/v1/households", farmer(tr.c1, "0907300061"))
	if r.status != http.StatusCreated {
		t.Fatalf("seed household: %d %v", r.status, r.body)
	}
	hh := uuid.MustParse(str(r.body, "household", "household_ref"))
	pool := f.env.AdminPool(t, migrations.PF)
	for _, seed := range []struct {
		tid    uuid.UUID
		area   string
		status string
	}{{f.tenant, "0.25", "open"}, {f.tenant, "0.50", "open"}, {f.tenant, "10", "cancelled"}, {ids.New(), "30", "open"}} {
		_, err := pool.Exec(context.Background(), `INSERT INTO pf_coop.season_current
		 (tenant_id, season_ref, plot_ref, plot_code, household_ref, cooperative_ref, commune_code,
		 season_code, season_year, variety_code, sop_template_ref, sop_version_id, sop_version_no,
		 sowing_date, expected_harvest_date, harvest_open_date, area_ha, status, current_event_id,
		 version, location_status, gps_source, on_behalf, opened_by, opened_at)
		 VALUES ($1,$2,$3,'AH61',$4,$5,'30001','DX',2026,'OM18',$6,$7,1,
		 '2026-01-01','2026-04-01','2026-03-20',$8::numeric,$9,$10,1,'missing','denied',false,$11,now())`,
			seed.tid, ids.New(), ids.New(), hh, tr.c1, ids.New(), ids.New(), seed.area, seed.status, ids.New(), ids.New())
		if err != nil {
			t.Fatal(err)
		}
	}
	ctx := tenant.WithID(context.Background(), f.tenant)
	ctx = authz.WithPrincipal(ctx, authz.Principal{ActorRef: ids.New(), TenantID: f.tenant,
		KN: &authz.KNContext{Role: authz.KNAdmin, Scope: authz.Scope{All: true}}})
	for _, raw := range []string{"0.74", ""} {
		_, err := f.svc.UpdateKNHousehold(ctx, hh, &raw, "Kiểm tra giới hạn diện tích", key())
		var p *problem.Problem
		if !errors.As(err, &p) || p.Status != http.StatusUnprocessableEntity || len(p.Errors) != 1 || p.Errors[0].Field != "cultivated_area_ha" {
			t.Fatalf("land %q below allocated area must fail: %v", raw, err)
		}
	}
	var unchanged bool
	var events int
	if err := pool.QueryRow(context.Background(), `SELECT cultivated_area_ha = 1.25,
	 (SELECT count(*) FROM pf_coop.kn_profile_event WHERE subject_ref=$1)
	 FROM pf_coop.household WHERE household_ref=$1 AND tenant_id=$2`, hh, f.tenant).Scan(&unchanged, &events); err != nil {
		t.Fatal(err)
	}
	if !unchanged || events != 0 {
		t.Fatalf("rejected edit changed projection or ledger: unchanged=%v events=%d", unchanged, events)
	}
	for _, raw := range []string{"0.75", "1.50"} {
		got, err := f.svc.UpdateKNHousehold(ctx, hh, &raw, "Cập nhật diện tích hợp lệ", key())
		if err != nil || got.CultivatedAreaHa == nil || !got.CultivatedAreaHa.Equal(decimal.RequireFromString(raw)) {
			t.Fatalf("valid land %s: %+v %v", raw, got, err)
		}
	}
}
