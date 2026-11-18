package handler_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	port "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/hop-tac-xa/migrations"
)

func (f *fixture) planPreviewPath() string {
	return "/v1/cooperatives/" + f.coopA.String() + "/households/" + f.h1.String() + "/season-plan-preview"
}

func planBody(version string) map[string]any {
	return map[string]any{"crop_code": "LUA", "variety_code": "OM5451", "sop_version_id": version,
		"area_ha": "0.25", "anchor_date": "2030-01-01"}
}

func firstPlanStep(r resp) map[string]any {
	return list(list(r.body, "stages")[0], "steps")[0].(map[string]any)
}

func TestAH71_PreviewRecalculatesWithoutWritingSeasonOrYield(t *testing.T) {
	f := newFixture(t)
	_, version := f.publishedSOP("AH71 preview", "OM5451")
	c := f.memberA()
	body := planBody(version)
	r := c.post(f.planPreviewPath(), body)
	expect(t, r, http.StatusOK, "member previews full schedule")
	if firstPlanStep(r)["from_date"] != "2029-12-29" || r.body["current_event_id"] != nil {
		t.Fatalf("preview: %v", r.body)
	}
	body["anchor_date"], body["area_ha"] = "2030-01-02", "0.50"
	r = c.post(f.planPreviewPath(), body)
	expect(t, r, http.StatusOK, "changed anchor and area recalculates")
	if firstPlanStep(r)["from_date"] != "2029-12-30" {
		t.Fatalf("stale plan: %v", r.body)
	}
	pool := pgtest.Get(t).AdminPool(t, migrations.PF)
	for _, table := range []string{"season_event", "season_current", "yield_summary"} {
		var count int
		if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM pf_coop.`+table+` WHERE tenant_id=$1`, f.tenant).Scan(&count); err != nil || count != 0 {
			t.Fatalf("preview wrote %s: %d (%v)", table, count, err)
		}
	}
	// 17:00 UTC is the next civil day in Vietnam.
	f.clk.Advance(14 * time.Hour)
	r = c.post(f.planPreviewPath(), body)
	expect(t, r, http.StatusOK, "GMT+7 business day")
	if r.body["as_of_date"] != "2026-09-30" {
		t.Fatalf("wrong business day: %v", r.body)
	}
}

func TestAH71_PermissionsScopeAndRequiredSelection(t *testing.T) {
	f := newFixture(t)
	_, version := f.publishedSOP("AH71 permissions", "OM5451")
	path, body := f.planPreviewPath(), planBody(version)
	for _, c := range []caller{f.managerA(), f.bothA(), f.officerA(), f.farmer(f.h1)} {
		expect(t, c.post(path, body), http.StatusForbidden, "read-only role cannot calculate")
	}
	for _, c := range []caller{f.memberB(), f.memberNoCoop(), f.as(ids.New(), f.coopA, port.RoleHTXMember)} {
		expect(t, c.post(path, body), http.StatusNotFound, "wrong scope cannot preview")
	}
	expect(t, f.platformAdmin().post(path, body), http.StatusOK, "admin previews selected HTX")
	for field, value := range map[string]any{"variety_code": "", "sop_version_id": "", "area_ha": "0.001", "anchor_date": "invalid"} {
		b := planBody(version)
		b[field] = value
		expect(t, f.memberA().post(path, b), http.StatusUnprocessableEntity, "required valid selection: "+field)
	}
}

func TestAH71_ReadUsesPinnedSnapshotAfterMasterRepublished(t *testing.T) {
	f := newFixture(t)
	tmpl, version := f.publishedSOP("AH71 snapshot", "OM5451")
	f.householdLand("1", true)
	r := f.memberA().post(f.householdPath(), householdOpenBody(nil))
	expect(t, r, http.StatusCreated, "open pinned season")
	ref := str(r.body, "season", "season_ref")
	path := f.householdPath() + "/" + ref + "/plan"
	plan := f.managerA().get(path)
	expect(t, plan, http.StatusOK, "representative reads plan")
	if firstPlanStep(plan)["from_date"] != "2029-12-29" || plan.body["sop_version_id"] != version || plan.body["current_event_id"] != str(r.body, "season", "current_event_id") {
		t.Fatalf("snapshot plan: %v", plan.body)
	}
	v2 := f.sopAdmin.adminPost("/v1/cropcycle/sops/"+tmpl+"/versions", map[string]any{"change_note": "Thay lịch chuẩn"})
	expect(t, v2, http.StatusCreated, "new master version")
	v2id := str(list(v2.body, "versions")[1], "id")
	steps := sopSteps()
	steps[0]["window"] = dateRange(-7, -5)
	expect(t, f.sopAdmin.adminPost("/v1/cropcycle/sop-versions/"+v2id+"/steps", map[string]any{"stages": sopStages(), "steps": steps}), http.StatusOK, "edit new master")
	expect(t, f.sopAdmin.adminPost("/v1/cropcycle/sop-versions/"+v2id+"/publish", nil), http.StatusOK, "publish new master")
	plan = f.managerA().get(path)
	expect(t, plan, http.StatusOK, "old season stays readable")
	if firstPlanStep(plan)["from_date"] != "2029-12-29" {
		t.Fatalf("master replaced snapshot: %v", plan.body)
	}
	preview := f.memberA().post(f.planPreviewPath(), planBody(v2id))
	expect(t, preview, http.StatusOK, "new season uses new master")
	if firstPlanStep(preview)["from_date"] != "2029-12-25" {
		t.Fatalf("preview did not update: %v", preview.body)
	}
	expect(t, f.memberB().get(path), http.StatusNotFound, "other HTX cannot read plan")
}
