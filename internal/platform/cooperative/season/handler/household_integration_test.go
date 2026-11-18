package handler_test

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"github.com/google/uuid"

	port "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/hop-tac-xa/migrations"
)

func (f *fixture) householdPath() string {
	return "/v1/cooperatives/" + f.coopA.String() + "/households/" + f.h1.String() + "/seasons"
}

func householdOpenBody(extra map[string]any) map[string]any {
	b := map[string]any{"crop_code": "LUA", "season_code": "DX", "season_year": 2026, "variety_code": "OM5451", "area_ha": "0.25", "anchor_date": "2030-01-01", "channel": "web"}
	for k, v := range extra {
		b[k] = v
	}
	return b
}

func (f *fixture) householdLand(area any, active bool) {
	f.t.Helper()
	_, err := pgtest.Get(f.t).AdminPool(f.t, migrations.PF).Exec(context.Background(), `UPDATE pf_coop.household SET cultivated_area_ha=$1::numeric,active=$2 WHERE tenant_id=$3 AND household_ref=$4`, area, active, f.tenant, f.h1)
	if err != nil {
		f.t.Fatal(err)
	}
}

func TestAH61_OpenReadSnapshotGPSAndIdempotency(t *testing.T) {
	f := newFixture(t)
	tmpl, version := f.publishedSOP("AH61 snapshot", "OM5451")
	c := f.memberA()
	path := f.householdPath()
	idem := key()
	body := householdOpenBody(nil)
	r := c.postKey(path, idem, body)
	expect(t, r, http.StatusCreated, "open household season with future anchor and denied GPS")
	season := r.body["season"].(map[string]any)
	ref := str(r.body, "season", "season_ref")
	if season["entry_kind"] != "household" || season["status"] != "open" || season["gps_missing"] != true || season["area_ha"] != "0.25" || season["anchor_date"] != "2030-01-01" || season["plot_ref"] != nil {
		t.Fatalf("household result: %v", season)
	}
	if str(r.body, "season", "sop_snapshot", "version_id") != version || str(r.body, "season", "sop_snapshot", "crop_code") != "LUA" {
		t.Fatalf("missing pinned snapshot: %v", r.body)
	}
	replay := c.postKey(path, idem, body)
	expect(t, replay, http.StatusCreated, "same receipt")
	if str(replay.body, "season", "season_ref") != ref {
		t.Fatal("replay allocated another season")
	}
	expect(t, c.postKey(path, idem, householdOpenBody(map[string]any{"area_ha": "0.30"})), http.StatusConflict, "same key changed payload")
	// Published template metadata can change; a season keeps its own copy.
	if _, err := pgtest.Get(t).AdminPool(t, migrations.PF).Exec(context.Background(), `UPDATE pf_coop.sop_template SET name='SOP renamed' WHERE tenant_id=$1 AND ref=$2`, f.tenant, uuid.MustParse(tmpl)); err != nil {
		t.Fatal(err)
	}
	d := f.managerA().get(path + "/" + ref)
	expect(t, d, http.StatusOK, "representative reads snapshot")
	if str(d.body, "sop_snapshot", "name") != "AH61 snapshot" {
		t.Fatalf("snapshot changed: %v", d.body)
	}
	expect(t, c.get("/v1/cropcycle/seasons/"+ref), http.StatusNotFound, "household cannot leak through plot DTO")
	for _, item := range list(c.get("/v1/cropcycle/seasons").body, "items") {
		if item.(map[string]any)["season_ref"] == ref {
			t.Fatal("household appeared in legacy list")
		}
	}
	second := c.post(path, householdOpenBody(map[string]any{"anchor_date": "2020-01-01", "gps": map[string]any{"lon": "105.6", "lat": "10.05", "accuracy_m": "4"}}))
	expect(t, second, http.StatusCreated, "past anchor and GPS declaration")
	if second.body["season"].(map[string]any)["gps_missing"] != false {
		t.Fatal("provided GPS marked missing")
	}
	pg := f.managerA().get(path + "?limit=1")
	expect(t, pg, http.StatusOK, "paginated household seasons")
	if len(list(pg.body, "items")) != 1 || pg.body["next_cursor"] == nil {
		t.Fatalf("pagination: %v", pg.body)
	}
	var events, legacy int
	if err := pgtest.Get(t).AdminPool(t, migrations.PF).QueryRow(context.Background(), `SELECT
	 (SELECT count(*) FROM shared.outbox WHERE tenant_id=$1 AND event_type=$2),
	 (SELECT count(*) FROM shared.outbox WHERE tenant_id=$1 AND event_type=$3)`, f.tenant, port.EventHouseholdSeasonOpened, port.EventSeasonOpened).Scan(&events, &legacy); err != nil {
		t.Fatal(err)
	}
	if events != 2 || legacy != 0 {
		t.Fatalf("outbox new=%d legacy=%d", events, legacy)
	}
}

func TestAH61_HouseholdValidationAndRoles(t *testing.T) {
	f := newFixture(t)
	f.publishedSOP("AH61 validation", "OM5451")
	path := f.householdPath()
	for _, c := range []caller{f.managerA(), f.bothA(), f.farmer(f.h1)} {
		expect(t, c.post(path, householdOpenBody(nil)), http.StatusForbidden, "read-only/legacy roles cannot open household season")
	}
	for _, c := range []caller{f.memberB(), f.memberNoCoop(), f.as(ids.New(), f.coopA, port.RoleHTXMember)} {
		expect(t, c.post(path, householdOpenBody(nil)), http.StatusNotFound, "hidden household")
	}
	for _, raw := range []string{"0", "-1", "0.001", "0.25001", "1e-2", "1,25", " 0.25", "NaN"} {
		r := f.memberA().post(path, householdOpenBody(map[string]any{"area_ha": raw}))
		expect(t, r, http.StatusUnprocessableEntity, "invalid area "+raw)
	}
	for _, extra := range []map[string]any{{"anchor_date": "2026-02-30"}, {"crop_code": "CON"}, {"variety_code": "ARA"}, {"season_year": 1999}, {"channel": "sync"}, {"gps": map[string]any{"lon": "181", "lat": "10"}}, {"plot_ref": ids.New().String()}} {
		expect(t, f.memberA().post(path, householdOpenBody(extra)), http.StatusUnprocessableEntity, "server validation")
	}
	f.householdLand(nil, true)
	r := f.memberA().post(path, householdOpenBody(nil))
	expect(t, r, http.StatusUnprocessableEntity, "unknown land")
	if r.body["code"] != "cropcycle.household_land_unknown" {
		t.Fatalf("wrong code %v", r.body)
	}
	f.householdLand("1", false)
	r = f.memberA().post(path, householdOpenBody(nil))
	expect(t, r, http.StatusUnprocessableEntity, "inactive household")
	if r.body["code"] != "cropcycle.household_inactive" {
		t.Fatalf("wrong code %v", r.body)
	}
	f.householdLand("1", true)
	r = f.platformAdmin().post(path, householdOpenBody(nil))
	expect(t, r, http.StatusCreated, "admin opens for selected cooperative")
	ref := str(r.body, "season", "season_ref")
	f.householdLand("1", false)
	expect(t, f.managerA().get(path+"/"+ref), http.StatusOK, "inactive household keeps existing season readable")
	expect(t, f.memberB().get(path+"/"+ref), http.StatusNotFound, "other cooperative cannot read")
	other := f.as(ids.New(), f.coopA, port.RoleHTXMember).get(path + "/" + ref)
	expect(t, other, http.StatusNotFound, "other tenant cannot read")
}

func TestAH61_ConcurrentHouseholdCapacityAndLegacyOpening(t *testing.T) {
	f := newFixture(t)
	f.publishedSOP("AH61 capacity", "OM5451")
	f.householdLand("1", true)
	start := make(chan struct{})
	results := make(chan resp, 2)
	var wg sync.WaitGroup
	for _, c := range []caller{f.memberA(), f.memberA()} {
		wg.Add(1)
		go func(c caller) {
			defer wg.Done()
			<-start
			results <- c.post(f.householdPath(), householdOpenBody(map[string]any{"area_ha": "0.60"}))
		}(c)
	}
	close(start)
	wg.Wait()
	close(results)
	created, conflict := 0, 0
	for r := range results {
		switch r.status {
		case http.StatusCreated:
			created++
		case http.StatusConflict:
			conflict++
			if r.body["code"] != "cropcycle.household_area_exceeded" || r.body["remaining_area_ha"] != "0.4" {
				t.Fatalf("conflict receipt: %v", r.body)
			}
		default:
			t.Fatalf("concurrent result: %d %v", r.status, r.body)
		}
	}
	if created != 1 || conflict != 1 {
		t.Fatalf("created=%d conflicts=%d", created, conflict)
	}
	legacy := f.memberA().open(f.p1, nil)
	expect(t, legacy, http.StatusConflict, "legacy opening cannot bypass household capacity")
	if legacy.body["code"] != "cropcycle.household_area_exceeded" {
		t.Fatalf("wrong legacy conflict: %v", legacy.body)
	}
	// Exactly the remaining capacity is valid and creates another independent season.
	expect(t, f.memberA().post(f.householdPath(), householdOpenBody(map[string]any{"area_ha": "0.40"})), http.StatusCreated, "remaining capacity")
}

func TestAH61_SOPPriorityAndNoActiveSOP(t *testing.T) {
	f := newFixture(t)
	_, generic := f.publishedSOP("AH61 generic", "")
	_, specific := f.publishedSOP("AH61 specific", "OM5451")
	path := f.householdPath()
	r := f.memberA().post(path, householdOpenBody(nil))
	expect(t, r, http.StatusCreated, "specific SOP wins")
	if str(r.body, "season", "sop_snapshot", "version_id") != specific {
		t.Fatalf("wrong SOP: %v", r.body)
	}
	expect(t, f.memberA().post(path, householdOpenBody(map[string]any{"sop_version_id": generic})), http.StatusUnprocessableEntity, "generic cannot override active specific SOP")
	r = f.memberA().post(path, householdOpenBody(map[string]any{"variety_code": "ST25"}))
	expect(t, r, http.StatusUnprocessableEntity, "multiple generic SOPs require explicit choice")
	r = f.memberA().post(path, householdOpenBody(map[string]any{"variety_code": "ST25", "sop_version_id": generic}))
	expect(t, r, http.StatusCreated, "explicit eligible generic SOP")
	r = f.memberA().post(path, householdOpenBody(map[string]any{"crop_code": "CAF", "variety_code": "ARA"}))
	expect(t, r, http.StatusUnprocessableEntity, "no matching crop SOP")
	if r.body["code"] != "cropcycle.no_active_sop_for_crop" {
		t.Fatalf("no SOP: %v", r.body)
	}
}
