package handler_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/store"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ledger"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
	"github.com/MEKONG-GREEN/hop-tac-xa/migrations"
)

func TestAH57_CalendarLedgerReplayScopeAndRebuild(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	e := store.CalendarEvent{ID: ids.New(), TenantID: f.tenant, CalendarRef: ids.New(), CooperativeRef: f.coopA,
		Action: "published", ActorRef: ids.New(), EventTime: startTime, IdemKey: key(), Draft: domain.CalendarDraft{
			CropCode: "LUA", SeasonCode: "DX", SeasonYear: 2026, SowFrom: domain.Day(2026, 12, 25), SowTo: domain.Day(2027, 1, 5), Recipients: []uuid.UUID{ids.New(), ids.New()}}}
	write := func(e *store.CalendarEvent) error {
		return tenant.WithTenantTx(ctx, f.db, f.tenant, func(tx pgx.Tx) error {
			if err := store.LockCalendar(ctx, tx, e.CalendarRef); err != nil {
				return err
			}
			if err := store.InsertCalendarEvent(ctx, tx, e); err != nil {
				return err
			}
			return store.SaveCalendar(ctx, tx, *e)
		})
	}
	if err := write(&e); err != nil {
		t.Fatal(err)
	}
	first := e
	reason := "Sửa khung ngày lịch thời vụ"
	e.ID = ids.New()
	e.Action = "edited"
	e.IdemKey = key()
	e.SupersedesID = &first.ID
	e.CorrectionReason = &reason
	e.Draft.SowFrom = domain.Day(2026, 12, 26)
	if err := write(&e); err != nil {
		t.Fatal(err)
	}
	edited := e
	e.ID = ids.New()
	e.Action = "cancelled"
	e.IdemKey = key()
	e.SupersedesID = &edited.ID
	if err := write(&e); err != nil {
		t.Fatal(err)
	}
	if e.Seq != 3 {
		t.Fatalf("version=%d", e.Seq)
	}
	err := tenant.WithTenantTx(ctx, f.db, f.tenant, func(tx pgx.Tx) error {
		// Replayed old projection writes must not restore the calendar after cancel.
		if err := store.SaveCalendar(ctx, tx, first); err != nil {
			return err
		}
		old, err := store.CalendarByIdem(ctx, tx, edited.IdemKey)
		if err != nil {
			return err
		}
		if old == nil || old.ID != edited.ID {
			t.Fatal("durable lookup lost edited revision")
		}
		cur, err := store.CalendarCurrent(ctx, tx, e.CalendarRef)
		if err != nil {
			return err
		}
		if cur == nil || cur.ID != e.ID || cur.Action != "cancelled" {
			t.Fatal("cancelled calendar restored")
		}
		n, err := store.RebuildCalendars(ctx, tx)
		if err != nil {
			return err
		}
		if n != 1 {
			t.Fatalf("rebuild count=%d", n)
		}
		cur, err = store.CalendarCurrent(ctx, tx, e.CalendarRef)
		if err != nil {
			return err
		}
		if cur == nil || cur.ID != e.ID || len(cur.Draft.Recipients) != 2 {
			t.Fatal("rebuild changed source snapshot")
		}
		chain, err := ledger.Verify(ctx, tx, "pf_coop.season_calendar_event", f.tenant, store.StreamCalendar(e.CalendarRef))
		if err != nil {
			return err
		}
		if !chain.OK || chain.RowsChecked != 3 {
			t.Fatalf("chain=%+v", chain)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name      string
		tid, node uuid.UUID
		want      int
	}{
		{"own HTX", f.tenant, f.coopA, 1}, {"other HTX", f.tenant, f.coopB, 0},
		{"missing projected node", f.tenant, ids.New(), 0}, {"other tenant", ids.New(), uuid.Nil, 0}, {"node-less admin/job", f.tenant, uuid.Nil, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tenant.WithTenantTx(ctx, f.db, tc.tid, func(tx pgx.Tx) error {
				node := ""
				if tc.node != uuid.Nil {
					node = tc.node.String()
				}
				if _, err := tx.Exec(ctx, "SELECT set_config('app.node_id',$1::text,true)", node); err != nil {
					return err
				}
				for _, table := range []string{"season_calendar_event", "season_calendar_current"} {
					var n int
					err := tx.QueryRow(ctx, "SELECT count(DISTINCT calendar_ref) FROM pf_coop."+table+" WHERE calendar_ref=$1::uuid", e.CalendarRef).Scan(&n)
					if err != nil {
						return err
					}
					if n != tc.want {
						t.Fatalf("%s count=%d want=%d", table, n, tc.want)
					}
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
	pool := pgtest.Get(t).AdminPool(t, migrations.PF)
	for _, node := range []uuid.UUID{f.coopB, ids.New()} {
		err := tenant.WithTenantTx(ctx, f.db, f.tenant, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, "SELECT set_config('app.node_id',$1::text,true)", node.String()); err != nil {
				return err
			}
			bad := first
			bad.ID, bad.CalendarRef, bad.IdemKey = ids.New(), ids.New(), key()
			return store.InsertCalendarEvent(ctx, tx, &bad)
		})
		var p *pgconn.PgError
		if !errors.As(err, &p) || p.Code != "42501" {
			t.Fatalf("out-of-scope source write allowed: %v", err)
		}
	}
	err = tenant.WithTenantTx(ctx, f.db, f.tenant, func(tx pgx.Tx) error {
		bad := e
		bad.ID, bad.IdemKey, bad.CooperativeRef = ids.New(), key(), f.coopB
		if err := store.InsertCalendarEvent(ctx, tx, &bad); err != nil {
			return err
		}
		return store.SaveCalendar(ctx, tx, bad)
	})
	if err == nil || err.Error() != "calendar: cooperative identity cannot change" {
		t.Fatalf("calendar rebound to another cooperative: %v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE pf_coop.season_calendar_event SET crop_code='KHAC' WHERE tenant_id=$1::uuid", f.tenant); err == nil {
		t.Fatal("immutable source updated")
	}
	var missing int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM shared.v_missing_rls").Scan(&missing); err != nil || missing != 0 {
		t.Fatalf("RLS gate=%d %v", missing, err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM shared.v_missing_immutability").Scan(&missing); err != nil || missing != 0 {
		t.Fatalf("ledger gate=%d %v", missing, err)
	}
	for _, mutate := range []func(*store.CalendarEvent){
		func(e *store.CalendarEvent) { e.Draft.SowTo = e.Draft.SowFrom.AddDate(0, 0, -1) },
		func(e *store.CalendarEvent) { e.Draft.Recipients = nil },
		func(e *store.CalendarEvent) { e.Draft.Recipients = []uuid.UUID{uuid.Nil} },
		func(e *store.CalendarEvent) { e.Draft.Recipients = make([]uuid.UUID, 501) },
	} {
		bad := first
		bad.ID = ids.New()
		bad.CalendarRef = ids.New()
		bad.IdemKey = key()
		mutate(&bad)
		err := write(&bad)
		var p *pgconn.PgError
		if !errors.As(err, &p) || p.Code != "23514" {
			t.Fatalf("invalid source not rejected by CHECK: %v", err)
		}
	}
}
