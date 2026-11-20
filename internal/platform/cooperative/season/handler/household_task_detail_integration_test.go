package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	seasonapp "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/app"
	mdport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/masterdata/port"
	syncport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/db"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
	"github.com/MEKONG-GREEN/hop-tac-xa/migrations"
)

func TestAH99NativeSyncBindsAccountAndDeviceTime(t *testing.T) {
	f, path, event := taskFixture(t)
	c := f.memberA()
	season := uuid.MustParse(path[strings.LastIndex(path, "/")+1:])
	p := authz.Principal{TenantID: c.tid, ActorRef: c.actor, OrgRef: c.org, Roles: []string{"htx_member"}}
	payload := detailedBody(event, "partial", "1.2345")
	delete(payload, "channel")
	payload["cooperative_ref"], payload["household_ref"], payload["season_ref"] = f.coopA, f.h1, season
	payload["recorded_by"], payload["tenant_id"] = p.ActorRef, p.TenantID
	payload["done_on"] = "2026-09-25"
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	rec := syncport.Record{ID: ids.New(), IdemKey: key(), Entity: "household_task_record", Op: syncport.OpCreate, EventTime: time.Date(2026, 9, 25, 3, 0, 0, 0, time.UTC), DeviceID: ids.New(), Payload: raw}
	sink := seasonapp.SyncSink{Svc: f.mod.svc}
	ctx := authz.WithPrincipal(context.Background(), p)
	result := sink.Accept(ctx, authz.Scope{}, rec)
	if result.Status != syncport.StatusAccepted {
		t.Fatalf("native offline record: %+v", result)
	}
	if result = sink.Accept(ctx, authz.Scope{}, rec); result.Status != syncport.StatusDuplicate {
		t.Fatalf("durable replay: %+v", result)
	}
	list, err := f.mod.svc.HouseholdChecklist(ctx, f.coopA, f.h1, season)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Records) != 1 || !list.Records[0].EventTime.Equal(rec.EventTime) || !list.Records[0].RecordedAt.After(rec.EventTime) || list.Records[0].TaskCompletion.Details.Backfilled {
		t.Fatal("device creation and server receive timestamps/backfill were conflated")
	}
	p.Roles = []string{"coop_manager"}
	if result = sink.Accept(authz.WithPrincipal(context.Background(), p), authz.Scope{}, rec); result.Status != syncport.StatusRejected || result.Code != "cropcycle.read_only" {
		t.Fatalf("role revocation replay: %+v", result)
	}
	p.Roles = []string{"htx_member"}
	p.ActorRef = ids.New()
	if result = sink.Accept(authz.WithPrincipal(context.Background(), p), authz.Scope{}, rec); result.Code != "forbidden" {
		t.Fatalf("different account: %+v", result)
	}
}

func detailedBody(event, state, dose string) map[string]any {
	b := tickBody(3, event)
	b["state"] = state
	b["done_on"] = today
	b["note"] = "Ghi thực tế ngoài đồng"
	b["materials"] = []map[string]any{{"material_code": "URE", "unit": "kg", "dose_per_ha": dose}}
	return b
}

func TestAH99ActualDosePartialDoneAndDurableReplay(t *testing.T) {
	f, path, event := taskFixture(t)
	member := f.memberA()
	operation := key()
	body := detailedBody(event, "partial", "1.2345")
	first := member.postKey(path+"/task-records", operation, body)
	expect(t, first, 201, "record partial actual dose")
	actual := list(get(first.body, "record", "details", "actual_materials"))[0]
	if str(actual, "quantity") != "0.61725" || str(actual, "dose_per_ha") != "1.2345" || str(actual, "group") != "phan_bon" {
		t.Fatalf("actual quantity must preserve exact dose*area: %v", actual)
	}
	if str(first.body, "checklist", "progress", "percent") != "0.00" {
		t.Fatal("partial record marked task done")
	}
	if get(first.body, "record", "details", "request_hash") != nil {
		t.Fatal("internal request hash leaked to UI")
	}
	second := member.post(path+"/task-records", detailedBody(event, "partial", "2"))
	expect(t, second, 201, "another partial")
	last := member.post(path+"/task-records", detailedBody(event, "done", "3"))
	expect(t, last, 201, "finish same task")
	if str(last.body, "checklist", "progress", "percent") != "20.00" || len(list(get(last.body, "checklist", "records"))) != 3 {
		t.Fatal("multiple actual records were lost or completed early")
	}
	expect(t, member.post(path+"/task-records", detailedBody(event, "done", "3")), 409, "resolved task cannot get a new completion")
	pool := pgtest.Get(t).AdminPool(t, migrations.PF)
	var recommendation string
	if err := pool.QueryRow(context.Background(), `SELECT task_completion->'step'->'materials'->0->>'dose_per_ha' FROM pf_coop.farm_log WHERE id=$1`, str(first.body, "record", "id")).Scan(&recommendation); err != nil || recommendation != "50" {
		t.Fatalf("recommendation overwritten: %s %v", recommendation, err)
	}
	if _, err := pool.Exec(context.Background(), `DELETE FROM shared.idempotency_key WHERE tenant_id=$1 AND idem_key=$2`, f.tenant, operation); err != nil {
		t.Fatal(err)
	}
	replay := member.postKey(path+"/task-records", operation, body)
	expect(t, replay, 201, "durable replay without duplicating work")
	if str(replay.body, "record", "id") != str(first.body, "record", "id") {
		t.Fatal("replay created another record")
	}
}

func TestAH99ValidationBackfillZeroAndScope(t *testing.T) {
	f, path, event := taskFixture(t)
	member := f.memberA()
	for _, tc := range []struct {
		change func(map[string]any)
		status int
	}{
		{func(b map[string]any) { b["done_on"] = "2026-09-30" }, 422},
		{func(b map[string]any) {
			b["materials"] = []map[string]any{{"material_code": "URE", "dose_per_ha": "-1"}}
		}, 422},
		{func(b map[string]any) {
			b["materials"] = []map[string]any{{"material_code": "URE", "dose_per_ha": "0"}}
		}, 409},
		{func(b map[string]any) { b["done_on"] = "2026-09-25" }, 422},
		{func(b map[string]any) {
			b["file_refs"] = []string{ids.New().String(), ids.New().String(), ids.New().String(), ids.New().String(), ids.New().String(), ids.New().String()}
		}, 422},
		{func(b map[string]any) { b["actor_ref"] = ids.New().String() }, 422},
	} {
		body := detailedBody(event, "partial", "1")
		tc.change(body)
		expect(t, member.post(path+"/task-records", body), tc.status, "invalid detail")
	}
	for _, who := range []caller{f.managerA(), f.bothA(), f.officerA()} {
		expect(t, who.post(path+"/task-records", detailedBody(event, "partial", "1")), 403, "read-only role")
	}
	expect(t, f.memberB().post(path+"/task-records", detailedBody(event, "partial", "1")), 404, "foreign cooperative")
	expect(t, f.as(ids.New(), f.coopA, "htx_member").post(path+"/task-records", detailedBody(event, "partial", "1")), 404, "foreign tenant")
	zero := detailedBody(event, "partial", "0")
	zero["zero_confirmed"] = true
	expect(t, member.post(path+"/task-records", zero), 201, "confirmed zero allowed")
	backfill := detailedBody(event, "partial", "1")
	backfill["done_on"] = "2026-09-25"
	backfill["backfill_reason"] = "Mất kết nối khi đi đồng"
	r := member.post(path+"/task-records", backfill)
	expect(t, r, 201, "backfill with reason")
	if get(r.body, "record", "details", "backfilled") != true {
		t.Fatal("missing backfill marker")
	}
}

type detailCatalogProbe struct {
	mdport.Catalogs
	probe func(context.Context) error
}

func (p detailCatalogProbe) List(ctx context.Context, kind string) ([]mdport.CatalogItem, error) {
	if err := p.probe(ctx); err != nil {
		return nil, err
	}
	return p.Catalogs.List(ctx, kind)
}

type detailImageProbe struct{ probe func(context.Context) error }

func (p detailImageProbe) VerifyOwnedImages(ctx context.Context, _ uuid.UUID, _ []uuid.UUID, _ int64) error {
	return p.probe(ctx)
}

func TestAH99CatalogAndPhotoPortsRunOutsideSingleConnectionTransaction(t *testing.T) {
	f, path, event := taskFixture(t)
	f.mod.svc.DB = db.ForModule(pgtest.Get(t).SizedPool(t, migrations.PF, dbadmin.AppPlatform, 1), db.Module("pf/cooperative"))
	var calls atomic.Int32
	probe := func(ctx context.Context) error {
		calls.Add(1)
		ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		return tenant.WithTenantTx(ctx, f.mod.svc.DB, f.tenant, func(tx pgx.Tx) error { var one int; return tx.QueryRow(ctx, "SELECT 1").Scan(&one) })
	}
	f.mod.svc.Catalogs = detailCatalogProbe{Catalogs: f.catalogs, probe: probe}
	f.mod.svc.TaskImages = detailImageProbe{probe: probe}
	body := detailedBody(event, "done", "10")
	body["file_refs"] = []string{ids.New().String()}
	r := f.memberA().post(path+"/task-records", body)
	expect(t, r, http.StatusCreated, "ports can reuse one PF connection")
	if calls.Load() != 2 {
		t.Fatalf("catalog/photo checks count=%d", calls.Load())
	}
}

func TestAH99UndatedTaskDoesNotRequireBackfillReason(t *testing.T) {
	f := newFixture(t)
	steps := sopSteps()
	steps[3]["window"] = map[string]any{"kind": "none"}
	template, version := f.createDraft("AH99 không ngày", map[string]any{"steps": steps})
	expect(t, f.sopAdmin.adminPost("/v1/cropcycle/sop-versions/"+version+"/publish", nil), 200, "publish undated SOP")
	expect(t, f.sopAdmin.adminPost("/v1/cropcycle/variety-bindings", map[string]any{"variety_code": "OM5451", "template_ref": template}), 200, "bind undated SOP")
	f.householdLand("2", true)
	member := f.memberA()
	opened := member.post(f.householdPath(), householdOpenBody(map[string]any{"area_ha": "0.50", "anchor_date": today}))
	expect(t, opened, 201, "open season")
	body := tickBody(4, str(opened.body, "season", "current_event_id"))
	body["state"] = "done"
	body["done_on"] = "2026-09-01"
	r := member.post(f.householdPath()+"/"+str(opened.body, "season", "season_ref")+"/task-records", body)
	expect(t, r, 201, "undated old task without reason")
	if get(r.body, "record", "details", "backfilled") != false {
		t.Fatal("undated task falsely marked backfill")
	}
}
