package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/store"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ledger"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
)

// AH-62 tests storage only: no new household-opening API is enabled by this package.
func householdStorageFixture(t *testing.T) (*fixture, store.SeasonEvent) {
	t.Helper()
	f := newFixture(t)
	f.publishedSOP("AH62 SOP", "OM5451")
	r := f.memberA().open(f.p1, nil)
	expect(t, r, http.StatusCreated, "legacy plot season")
	var event store.SeasonEvent
	err := tenant.WithTenantTx(context.Background(), f.db, f.tenant, func(tx pgx.Tx) error {
		e, err := store.SeasonEventByID(context.Background(), tx, uuid.MustParse(str(r.body, "event", "id")))
		if err != nil {
			return err
		}
		if e == nil {
			t.Fatal("missing legacy event")
		}
		event = *e
		v, err := store.VersionByID(context.Background(), tx, e.SOPVersionID, false)
		if err != nil {
			return err
		}
		event.SOPSnapshot, err = json.Marshal(map[string]any{
			"template_ref": e.SOPTemplateRef.String(), "version_id": e.SOPVersionID.String(),
			"version_no": e.SOPVersionNo, "crop_code": "rice", "anchor_name": "Ngày xuống giống",
			"stages": []any{}, "steps": v.Steps,
		})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	event.ID, event.SeasonRef, event.IdemKey = ids.New(), ids.New(), key()
	event.EntryKind, event.CropCode, event.PlotRef, event.PlotCode = "household", "rice", uuid.Nil, ""
	event.ExpectedHarvestDate, event.HarvestOpenDate = time.Time{}, time.Time{}
	event.AreaHa = decimal.RequireFromString("0.25")
	return f, event
}

func TestAH62_HouseholdSeasonStorageReplayAndTenant(t *testing.T) {
	f, first := householdStorageFixture(t)
	ctx := context.Background()
	second := first
	second.ID, second.SeasonRef, second.IdemKey = ids.New(), ids.New(), key()
	second.AreaHa = decimal.RequireFromString("0.30")
	err := tenant.WithTenantTx(ctx, f.db, f.tenant, func(tx pgx.Tx) error {
		for _, e := range []*store.SeasonEvent{&first, &second} {
			if err := store.InsertSeasonEvent(ctx, tx, e); err != nil {
				return err
			}
			if err := store.SaveSeason(ctx, tx, f.tenant, store.ApplySeasonEvent(nil, *e)); err != nil {
				return err
			}
			if err := store.RefreshYield(ctx, tx, e.SeasonRef); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	check := func() {
		t.Helper()
		err := tenant.WithTenantTx(ctx, f.db, f.tenant, func(tx pgx.Tx) error {
			legacy, err := store.OpenSeasonOfPlot(ctx, tx, f.p1, false)
			if err != nil {
				return err
			}
			if legacy == nil || legacy.EntryKind != "" || len(legacy.SOPSnapshot) != 0 || legacy.ExpectedHarvestDate.IsZero() ||
				!legacy.AreaHa.Equal(decimal.RequireFromString("1.5")) {
				t.Fatalf("legacy season changed: %+v", legacy)
			}
			verified, err := ledger.Verify(ctx, tx, "pf_coop.season_event", f.tenant, store.StreamSeason(legacy.SeasonRef))
			if err != nil {
				return err
			}
			if !verified.OK || verified.RowsChecked != 1 {
				t.Fatalf("legacy ledger chain: %+v", verified)
			}
			for _, e := range []store.SeasonEvent{first, second} {
				got, err := store.SeasonByRef(ctx, tx, e.SeasonRef, false)
				if err != nil {
					return err
				}
				if got == nil || got.PlotRef != uuid.Nil || got.PlotCode != "" || got.EntryKind != "household" ||
					got.CropCode != "rice" || !got.AreaHa.Equal(e.AreaHa) || !got.ExpectedHarvestDate.IsZero() || !got.HarvestOpenDate.IsZero() {
					t.Fatalf("household season changed: %+v", got)
				}
				var before, after any
				if err := json.Unmarshal(e.SOPSnapshot, &before); err != nil {
					return err
				}
				if err := json.Unmarshal(got.SOPSnapshot, &after); err != nil {
					return err
				}
				if !reflect.DeepEqual(before, after) {
					t.Fatal("SOP snapshot changed during persistence/replay")
				}
				v, err := ledger.Verify(ctx, tx, "pf_coop.season_event", f.tenant, store.StreamSeason(e.SeasonRef))
				if err != nil {
					return err
				}
				if !v.OK || v.RowsChecked != 1 {
					t.Fatalf("household ledger chain: %+v", v)
				}
			}
			var total string
			if err := tx.QueryRow(ctx, `SELECT sum(area_ha)::text FROM pf_coop.season_current
			  WHERE household_ref=$1::uuid AND status='open'`, first.HouseholdRef).Scan(&total); err != nil {
				return err
			}
			if !decimal.RequireFromString(total).Equal(decimal.RequireFromString("2.05")) {
				t.Fatalf("legacy + two household seasons: %s", total)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	check()
	expect(t, f.platformAdmin().adminPost("/v1/cropcycle/projections/rebuild", map[string]any{}), http.StatusOK, "rebuild mixed seasons")
	check()
	foreign := f.memberB().get("/v1/cropcycle/seasons")
	expect(t, foreign, http.StatusOK, "different HTX reads its own seasons")
	if bytes.Contains(foreign.raw, []byte(first.SeasonRef.String())) || bytes.Contains(foreign.raw, []byte(second.SeasonRef.String())) {
		t.Fatalf("household seasons leaked to another HTX: %s", foreign.raw)
	}
	err = tenant.WithTenantTx(ctx, f.db, ids.New(), func(tx pgx.Tx) error {
		got, err := store.SeasonByRef(ctx, tx, first.SeasonRef, false)
		if got != nil {
			t.Fatal("household season leaked across tenants")
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	// Ledger updates remain forbidden even for the new snapshot columns.
	err = tenant.WithTenantTx(ctx, f.db, f.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE pf_coop.season_event SET sop_snapshot='{}'::jsonb WHERE id=$1::uuid`, first.ID)
		return err
	})
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "42501" {
		t.Fatalf("snapshot ledger must remain immutable: %v", err)
	}
}

func TestAH62_HouseholdSeasonStorageRejectsInvalidShape(t *testing.T) {
	f, base := householdStorageFixture(t)
	cases := []struct {
		name   string
		mutate func(*store.SeasonEvent)
	}{
		{"zero area", func(e *store.SeasonEvent) { e.AreaHa = decimal.Zero }},
		{"negative area", func(e *store.SeasonEvent) { e.AreaHa = decimal.NewFromInt(-1) }},
		{"fractional cent", func(e *store.SeasonEvent) { e.AreaHa = decimal.RequireFromString("0.001") }},
		{"missing household", func(e *store.SeasonEvent) { e.HouseholdRef = uuid.Nil }},
		{"missing crop", func(e *store.SeasonEvent) { e.CropCode = "" }},
		{"missing snapshot", func(e *store.SeasonEvent) { e.SOPSnapshot = nil }},
		{"incomplete snapshot", func(e *store.SeasonEvent) { e.SOPSnapshot = json.RawMessage(`{}`) }},
		{"different SOP version", func(e *store.SeasonEvent) { e.SOPVersionID = ids.New() }},
		{"different crop", func(e *store.SeasonEvent) { e.CropCode = "maize" }},
		{"synthetic plot", func(e *store.SeasonEvent) { e.PlotRef = ids.New() }},
		{"missing mode", func(e *store.SeasonEvent) { e.EntryKind = "" }},
		{"unknown mode", func(e *store.SeasonEvent) { e.EntryKind = "other" }},
		{"partial forecast", func(e *store.SeasonEvent) { e.ExpectedHarvestDate = e.SowingDate }},
	}
	for _, tt := range cases {
		for _, projection := range []bool{false, true} {
			label := "event/"
			if projection {
				label = "projection/"
			}
			t.Run(label+tt.name, func(t *testing.T) {
				e := base
				e.ID, e.SeasonRef, e.IdemKey = ids.New(), ids.New(), key()
				tt.mutate(&e)
				err := tenant.WithTenantTx(context.Background(), f.db, f.tenant, func(tx pgx.Tx) error {
					if projection {
						return store.SaveSeason(context.Background(), tx, f.tenant, store.ApplySeasonEvent(nil, e))
					}
					return store.InsertSeasonEvent(context.Background(), tx, &e)
				})
				var pg *pgconn.PgError
				if !errors.As(err, &pg) || pg.Code != "23514" {
					t.Fatalf("expected database check violation: %v", err)
				}
			})
		}
	}
}
