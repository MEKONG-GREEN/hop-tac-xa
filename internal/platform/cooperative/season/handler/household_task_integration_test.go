package handler_test

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/store"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
	"github.com/MEKONG-GREEN/hop-tac-xa/migrations"
)

func taskFixture(t *testing.T) (*fixture, string, string) {
	f := newFixture(t)
	f.publishedSOP("AH88 checklist", "OM5451")
	f.householdLand("2", true)
	r := f.memberA().post(f.householdPath(), householdOpenBody(map[string]any{"area_ha": "0.50", "anchor_date": "2026-09-29"}))
	expect(t, r, http.StatusCreated, "open checklist season")
	return f, f.householdPath() + "/" + str(r.body, "season", "season_ref"), str(r.body, "season", "current_event_id")
}
func tickBody(step int, event string) map[string]any {
	return map[string]any{"step_seq": step, "expected_season_event_id": event, "gps": map[string]any{"source": "denied"}, "channel": "web"}
}

func TestAH88_CompleteDoseGPSDurableReplayAndRebuild(t *testing.T) {
	f, path, event := taskFixture(t)
	c := f.memberA()
	k := key()
	b := tickBody(3, event)
	r := c.get(path + "/checklist")
	expect(t, r, http.StatusOK, "read checklist")
	r = c.postKey(path+"/task-completions", k, b)
	expect(t, r, http.StatusCreated, "tick recommended dose")
	if str(r.body, "record", "done_on") != "2026-09-29" || get(r.body, "record", "gps_missing") != true || str(list(get(r.body, "record", "inputs"))[0], "qty") != "25" {
		t.Fatalf("record: %v", r.body)
	}
	ref := str(r.body, "record", "id")
	if str(r.body, "checklist", "progress", "percent") != "20.00" {
		t.Fatalf("progress: %v", r.body)
	}
	expect(t, c.post(path+"/task-completions", b), http.StatusConflict, "new operation cannot tick twice")
	pool := pgtest.Get(t).AdminPool(t, migrations.PF)
	if _, err := pool.Exec(context.Background(), `DELETE FROM shared.idempotency_key WHERE tenant_id=$1 AND idem_key=$2`, f.tenant, k); err != nil {
		t.Fatal(err)
	}
	replay := c.postKey(path+"/task-completions", k, b)
	expect(t, replay, http.StatusCreated, "durable replay")
	if str(replay.body, "record", "id") != ref {
		t.Fatal("duplicate record")
	}
	if _, err := pool.Exec(context.Background(), `DELETE FROM shared.idempotency_key WHERE tenant_id=$1 AND idem_key=$2`, f.tenant, k); err != nil {
		t.Fatal(err)
	}
	expect(t, c.postKey(path+"/task-completions", k, tickBody(2, event)), http.StatusUnprocessableEntity, "changed durable payload")
	legacy := c.open(f.p1, nil)
	expect(t, legacy, http.StatusCreated, "legacy plot shares household budget")
	expect(t, c.post("/v1/cropcycle/farm-logs", map[string]any{"season_ref": str(legacy.body, "season", "season_ref"), "activity_code": "tuoi_nuoc"}), http.StatusCreated, "legacy journal keeps review workflow")
	for table, want := range map[string]int{"farm_log": 2, "household_task_current": 1, "farm_log_current": 1, "farm_log_review": 0} {
		var n int
		if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM pf_coop.`+table+` WHERE tenant_id=$1`, f.tenant).Scan(&n); err != nil || n != want {
			t.Fatalf("%s=%d %v", table, n, err)
		}
	}
	var missing int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM shared.v_missing_rls`).Scan(&missing); err != nil || missing != 0 {
		t.Fatalf("RLS gate %d %v", missing, err)
	}
	if _, err := pool.Exec(context.Background(), `UPDATE pf_coop.farm_log SET event_time=event_time+interval '1 day' WHERE tenant_id=$1`, f.tenant); err == nil {
		t.Fatal("immutable log updated")
	}
	for _, mutate := range []func(*store.FarmLog){func(l *store.FarmLog) { l.TaskCompletion = nil }, func(l *store.FarmLog) { l.TaskCompletion.DoneOn = "-infinity" }, func(l *store.FarmLog) { l.TaskCompletion.StageSeq = 99 }} {
		err := tenant.WithTenantTx(context.Background(), f.db, f.tenant, func(tx pgx.Tx) error {
			original, err := store.FarmLogByID(context.Background(), tx, uuid.MustParse(ref))
			if err != nil {
				return err
			}
			l := *original
			m := *l.TaskCompletion
			l.TaskCompletion = &m
			l.ID = ids.New()
			l.LogRef = l.ID
			l.IdemKey = "shape:" + l.ID.String()
			mutate(&l)
			return store.InsertFarmLog(context.Background(), tx, &l)
		})
		var pge *pgconn.PgError
		if !errors.As(err, &pge) || pge.Code != "23514" || pge.ConstraintName != "farm_log_entry_shape" {
			t.Fatalf("invalid task shape: %v", err)
		}
	}
	expect(t, f.platformAdmin().adminPost("/v1/cropcycle/projections/rebuild", map[string]any{}), http.StatusOK, "replay mixed projections")
	r = c.get(path + "/checklist")
	expect(t, r, http.StatusOK, "read replayed checklist")
	if str(r.body, "progress", "percent") != "20.00" || str(list(r.body, "records")[0], "id") != ref {
		t.Fatalf("replay drift: %v", r.body)
	}
}

func TestAH88_RolesScopeStateAndInactiveHousehold(t *testing.T) {
	f, path, event := taskFixture(t)
	for _, c := range []caller{f.managerA(), f.bothA(), f.officerA()} {
		expect(t, c.post(path+"/task-completions", tickBody(1, event)), http.StatusForbidden, "read-only role")
	}
	r := f.managerA().get(path + "/checklist")
	expect(t, r, http.StatusOK, "representative reads")
	stage := list(get(r.body, "progress", "stages"))[0]
	if get(list(stage, "tasks")[0], "can_complete") != false {
		t.Fatal("representative write flag")
	}
	expect(t, f.memberB().post(path+"/task-completions", tickBody(1, event)), http.StatusNotFound, "foreign HTX")
	expect(t, f.as(ids.New(), f.coopA, "htx_member").post(path+"/task-completions", tickBody(1, event)), http.StatusNotFound, "foreign tenant")
	expect(t, f.memberA().post(path+"/task-completions", tickBody(1, ids.New().String())), http.StatusConflict, "stale season")
	expect(t, f.memberA().post(path+"/task-completions", tickBody(99, event)), http.StatusUnprocessableEntity, "not in snapshot")
	for _, field := range []string{"done_on", "actor_ref", "inputs"} {
		body := tickBody(2, event)
		body[field] = "forged"
		expect(t, f.memberA().post(path+"/task-completions", body), http.StatusUnprocessableEntity, "client cannot supply "+field)
	}
	point := tickBody(2, event)
	point["gps"] = map[string]any{"source": "device", "lon": "106.1234567", "lat": "10.1234567", "accuracy_m": "1.25"}
	r = f.memberA().post(path+"/task-completions", point)
	expect(t, r, http.StatusCreated, "unverified GPS without a fake plot")
	if get(r.body, "record", "gps_missing") != false {
		t.Fatal(r.body)
	}
	f.householdLand("2", false)
	expect(t, f.memberA().post(path+"/task-completions", tickBody(1, event)), http.StatusCreated, "inactive household continues old season")
	pool := pgtest.Get(t).AdminPool(t, migrations.PF)
	if _, err := pool.Exec(context.Background(), `UPDATE pf_coop.season_current SET status='completed',harvest_id=$2 WHERE tenant_id=$1`, f.tenant, ids.New()); err != nil {
		t.Fatal(err)
	}
	expect(t, f.memberA().post(path+"/task-completions", tickBody(3, event)), http.StatusUnprocessableEntity, "harvested earlier stage locked")
	expect(t, f.memberA().post(path+"/task-completions", tickBody(5, event)), http.StatusCreated, "harvested last stage may continue")
	if _, err := pool.Exec(context.Background(), `UPDATE pf_coop.season_current SET status='cancelled',harvest_id=NULL WHERE tenant_id=$1`, f.tenant); err != nil {
		t.Fatal(err)
	}
	expect(t, f.memberA().post(path+"/task-completions", tickBody(4, event)), http.StatusUnprocessableEntity, "stopped season")
}

func TestAH88_ConcurrentTickIsUnique(t *testing.T) {
	f, path, event := taskFixture(t)
	c := f.memberA()
	var wg sync.WaitGroup
	statuses := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); statuses <- c.post(path+"/task-completions", tickBody(1, event)).status }()
	}
	wg.Wait()
	close(statuses)
	got := map[int]int{}
	for s := range statuses {
		got[s]++
	}
	if got[201] != 1 || got[409] != 1 {
		t.Fatal(got)
	}
}
