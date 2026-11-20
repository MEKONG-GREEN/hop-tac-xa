package handler_test

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/port"
	shareddb "github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/db"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/inbox"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/outbox"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
	"github.com/MEKONG-GREEN/hop-tac-xa/migrations"
)

type calendarMembers struct {
	tenant, coop uuid.UUID
	refs         []uuid.UUID
	probe        func(context.Context) error
	fail         bool
}

func (m *calendarMembers) Active(ctx context.Context, tid, coop uuid.UUID, refs []uuid.UUID) ([]uuid.UUID, error) {
	if m.probe != nil {
		if err := m.probe(ctx); err != nil {
			return nil, err
		}
	}
	if m.fail {
		return nil, errors.New("directory unavailable")
	}
	out := []uuid.UUID{}
	if tid == m.tenant && coop == m.coop {
		for _, ref := range refs {
			if slices.Contains(m.refs, ref) {
				out = append(out, ref)
			}
		}
	}
	return out, nil
}
func (m *calendarMembers) Page(ctx context.Context, tid, coop uuid.UUID, _, _ int) (port.CalendarMemberPage, error) {
	refs, err := m.Active(ctx, tid, coop, m.refs)
	return port.CalendarMemberPage{Refs: refs}, err
}

type calendarSender struct {
	calls []port.SeasonCalendarPublishedV1
	probe func(context.Context) error
	fail  bool
}

func (n *calendarSender) CalendarPublished(ctx context.Context, _ uuid.UUID, notice port.SeasonCalendarPublishedV1) error {
	if n.probe != nil {
		if err := n.probe(ctx); err != nil {
			return err
		}
	}
	if n.fail {
		return errors.New("notify unavailable")
	}
	n.calls = append(n.calls, notice)
	return nil
}
func calendarBody(actor uuid.UUID) map[string]any {
	return map[string]any{"crop_code": "LUA", "season_code": "DX", "season_year": 2026, "sow_from": "2026-09-01", "sow_to": "2026-09-20", "recipients": []string{actor.String()}}
}
func calendarPath(coop uuid.UUID) string {
	return "/v1/cooperatives/" + coop.String() + "/season-calendars"
}
func calendarEnvelope(t *testing.T, f *fixture, sourceID string) outbox.Envelope {
	t.Helper()
	var env outbox.Envelope
	pool := pgtest.Get(t).AdminPool(t, migrations.PF)
	err := pool.QueryRow(context.Background(), `SELECT id,event_type,schema_version,tenant_id,occurred_at,producer,payload FROM shared.outbox
 WHERE tenant_id=$1::uuid AND event_type=$2::text AND payload->>'source_id'=$3::text`, f.tenant, port.EventSeasonCalendarPublished, sourceID).Scan(&env.EventID, &env.EventType, &env.SchemaVersion, &env.TenantID, &env.OccurredAt, &env.Producer, &env.Payload)
	if err != nil {
		t.Fatal(err)
	}
	return env
}

func TestAH58_CalendarPublishEditCancelScopeAndDurableReplay(t *testing.T) {
	f := newFixture(t)
	member, unselected := f.memberA(), f.memberA()
	manager := f.managerA()
	members := &calendarMembers{tenant: f.tenant, coop: f.coopA, refs: []uuid.UUID{member.actor, unselected.actor}}
	sender := &calendarSender{}
	f.mod.svc.CalendarMembers = members
	f.mod.svc.CalendarNotifier = sender
	path := calendarPath(f.coopA)
	b := calendarBody(member.actor)
	k := key()
	expect(t, member.post(path, b), http.StatusForbidden, "Member cannot publish")
	expect(t, f.managerB().post(path, b), http.StatusNotFound, "other coop cannot publish")
	bad := calendarBody(f.memberB().actor)
	expect(t, manager.post(path, bad), http.StatusUnprocessableEntity, "recipient outside coop")
	bad = calendarBody(member.actor)
	bad["sow_to"] = "2026-08-31"
	expect(t, manager.post(path, bad), http.StatusUnprocessableEntity, "reversed dates")
	r := manager.postKey(path, k, b)
	expect(t, r, http.StatusCreated, "publish")
	ref, event := str(r.body, "calendar_ref"), str(r.body, "current_event_id")
	f.publishedSOP("Calendar guidance", "OM5451")
	f.householdLand("1", true)
	opened := member.post(f.householdPath(), householdOpenBody(map[string]any{"anchor_date": "2026-09-29"}))
	expect(t, opened, http.StatusCreated, "Member may open outside calendar window")
	detail := "/v1/cropcycle/calendars/" + ref
	view := member.get(detail)
	expect(t, view, http.StatusOK, "selected Member reads")
	if _, ok := view.body["recipients"]; ok {
		t.Fatal("Member response exposes recipient list")
	}
	expect(t, unselected.get(detail), http.StatusNotFound, "unselected Member hidden")
	expect(t, f.memberB().get(detail), http.StatusNotFound, "foreign coop detail hidden")
	expect(t, f.as(ids.New(), f.coopA, port.RoleCoopManager).get(detail), http.StatusNotFound, "foreign tenant detail hidden")
	if got := len(list(get(member.get(path).body, "items"))); got != 1 {
		t.Fatalf("member list=%d", got)
	}
	if got := len(list(get(unselected.get(path).body, "items"))); got != 0 {
		t.Fatalf("unselected list=%d", got)
	}
	expect(t, member.get("/v1/cooperatives/"+f.coopA.String()+"/season-calendar-recipients"), http.StatusForbidden, "Member cannot read account directory")
	expect(t, manager.get("/v1/cooperatives/"+f.coopA.String()+"/season-calendar-recipients"), http.StatusOK, "manager recipient directory")
	env := calendarEnvelope(t, f, event)
	processed, err := inbox.Process(context.Background(), f.db, port.CalendarNoticeConsumer, env, f.mod.svc.HandleCalendarNotice)
	if err != nil || !processed || len(sender.calls) != 1 {
		t.Fatalf("publish delivery=%v %v calls=%d", processed, err, len(sender.calls))
	}
	processed, err = inbox.Process(context.Background(), f.db, port.CalendarNoticeConsumer, env, f.mod.svc.HandleCalendarNotice)
	if err != nil || processed || len(sender.calls) != 1 {
		t.Fatal("duplicate inbox redelivered")
	}
	edit := calendarBody(member.actor)
	edit["expected_event_id"] = event
	edit["sow_from"] = "2026-09-05"
	revised := manager.post(path+"/"+ref+"/revisions", edit)
	expect(t, revised, http.StatusCreated, "edit published calendar")
	expect(t, manager.post(path+"/"+ref+"/revisions", edit), http.StatusConflict, "stale expected version")
	env = calendarEnvelope(t, f, str(revised.body, "current_event_id"))
	_, err = inbox.Process(context.Background(), f.db, port.CalendarNoticeConsumer, env, f.mod.svc.HandleCalendarNotice)
	if err != nil || len(sender.calls) != 2 || sender.calls[0].SourceID == sender.calls[1].SourceID {
		t.Fatalf("edit must notify again: %v %+v", err, sender.calls)
	}
	cancel := map[string]any{"expected_event_id": str(revised.body, "current_event_id")}
	cancelled := manager.post(path+"/"+ref+"/cancel", cancel)
	expect(t, cancelled, http.StatusCreated, "cancel")
	expect(t, member.get(detail), http.StatusNotFound, "cancelled deep link cannot open season")
	if got := len(list(get(member.get(path).body, "items"))); got != 0 {
		t.Fatal("cancelled calendar remains in Member list")
	}
	expect(t, manager.get(detail), http.StatusOK, "management retains cancelled history")
	var count int
	pool := pgtest.Get(t).AdminPool(t, migrations.PF)
	var seasonEvent uuid.UUID
	if err = pool.QueryRow(context.Background(), "SELECT current_event_id FROM pf_coop.season_current WHERE tenant_id=$1 AND season_ref=$2", f.tenant, str(opened.body, "season", "season_ref")).Scan(&seasonEvent); err != nil {
		t.Fatal(err)
	}
	if seasonEvent.String() != str(opened.body, "season", "current_event_id") {
		t.Fatal("cancel changed opened season")
	}
	if err = pool.QueryRow(context.Background(), "SELECT count(*) FROM shared.outbox WHERE tenant_id=$1 AND event_type=$2", f.tenant, port.EventSeasonCalendarPublished).Scan(&count); err != nil || count != 2 {
		t.Fatalf("cancel emitted notice: %d %v", count, err)
	}
	if _, err = pool.Exec(context.Background(), "DELETE FROM shared.idempotency_key WHERE tenant_id=$1 AND idem_key=$2", f.tenant, k); err != nil {
		t.Fatal(err)
	}
	members.fail = true // success replay does not depend on a live directory/catalog
	replay := manager.postKey(path, k, b)
	expect(t, replay, http.StatusCreated, "durable replay after cancellation")
	if str(replay.body, "calendar_ref") != ref {
		t.Fatal("replay created another calendar")
	}
}

func TestAH58_AdminOtherCoopAndMissingDependencies(t *testing.T) {
	f := newFixture(t)
	member := f.memberB()
	f.mod.svc.CalendarMembers = &calendarMembers{tenant: f.tenant, coop: f.coopB, refs: []uuid.UUID{member.actor}}
	path := calendarPath(f.coopB)
	b := calendarBody(member.actor)
	expect(t, f.platformAdmin().post(path, b), http.StatusCreated, "Admin publishes for another coop")
	expect(t, f.managerA().get("/v1/cooperatives/"+f.coopB.String()+"/season-calendar-recipients"), http.StatusNotFound, "directory scope precedes IO")
	f.mod.svc.CalendarMembers = nil
	expect(t, f.managerB().post(path, b), http.StatusServiceUnavailable, "missing directory fails closed")
	f.mod.svc.CalendarMembers = &calendarMembers{tenant: f.tenant, coop: f.coopB, refs: []uuid.UUID{member.actor}}
	f.mod.svc.Catalogs = nil
	expect(t, f.managerB().post(path, b), http.StatusServiceUnavailable, "missing catalogs fail closed")
}

func TestAH58_NotificationPoolOneRetryAndCancelledQueue(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cfg := f.pool.Config()
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	f.db = shareddb.ForModule(pool, shareddb.Module("pf/cooperative"))
	f.mod.svc.DB = f.db
	probe := func(ctx context.Context) error {
		return tenant.WithTenantTx(ctx, f.db, f.tenant, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, "SELECT 1"); return err })
	}
	member := f.memberA()
	members := &calendarMembers{tenant: f.tenant, coop: f.coopA, refs: []uuid.UUID{member.actor}, probe: probe}
	sender := &calendarSender{probe: probe, fail: true}
	f.mod.svc.CalendarMembers = members
	f.mod.svc.CalendarNotifier = sender
	manager := f.managerA()
	path := calendarPath(f.coopA)
	r := manager.post(path, calendarBody(member.actor))
	expect(t, r, http.StatusCreated, "publish with pool one")
	env := calendarEnvelope(t, f, str(r.body, "current_event_id"))
	if _, err = inbox.Process(ctx, f.db, port.CalendarNoticeConsumer, env, f.mod.svc.HandleCalendarNotice); err == nil {
		t.Fatal("notification failure was acknowledged")
	}
	sender.fail = false
	if processed, err := inbox.Process(ctx, f.db, port.CalendarNoticeConsumer, env, f.mod.svc.HandleCalendarNotice); err != nil || !processed || len(sender.calls) != 1 {
		t.Fatalf("retry/pool one: %v %v", processed, err)
	}
	r = manager.post(path, calendarBody(member.actor))
	expect(t, r, http.StatusCreated, "second calendar")
	env = calendarEnvelope(t, f, str(r.body, "current_event_id"))
	expect(t, manager.post(path+"/"+str(r.body, "calendar_ref")+"/cancel", map[string]any{"expected_event_id": str(r.body, "current_event_id")}), http.StatusCreated, "cancel before dispatch")
	if _, err = inbox.Process(ctx, f.db, port.CalendarNoticeConsumer, env, f.mod.svc.HandleCalendarNotice); err != nil || len(sender.calls) != 1 {
		t.Fatalf("cancelled queue delivered: %v", err)
	}
}

func TestAH58_ConcurrentEditsOneRevision(t *testing.T) {
	f := newFixture(t)
	member := f.memberA()
	f.mod.svc.CalendarMembers = &calendarMembers{tenant: f.tenant, coop: f.coopA, refs: []uuid.UUID{member.actor}}
	manager := f.managerA()
	path := calendarPath(f.coopA)
	r := manager.post(path, calendarBody(member.actor))
	expect(t, r, http.StatusCreated, "publish")
	edit := calendarBody(member.actor)
	edit["expected_event_id"] = str(r.body, "current_event_id")
	path += "/" + str(r.body, "calendar_ref") + "/revisions"
	results := make(chan int, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() { results <- manager.post(path, edit).status })
	}
	wg.Wait()
	close(results)
	statuses := []int{}
	for status := range results {
		statuses = append(statuses, status)
	}
	slices.Sort(statuses)
	if !slices.Equal(statuses, []int{http.StatusCreated, http.StatusConflict}) {
		t.Fatalf("concurrent edits=%v", statuses)
	}
}
