package handler_test

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/app"
	mdport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
)

const factorsPath = "/v1/cropcycle/straw-factors"

func TestAH282FactorHistoryAndTimeEffectivePort(t *testing.T) {
	f := newFixture(t)
	admin := f.platformAdmin()
	list := admin.adminGet(factorsPath)
	expect(t, list, 200, "default factors")
	for _, row := range listItems(list) {
		if row["percentage"] != "35" || row["configured"] != false {
			t.Fatal("missing configuration must show 35%")
		}
	}
	body := map[string]any{"variety_code": "OM5451", "percentage": "30"}
	idem := key()
	first := admin.adminPostKey(factorsPath, body, idem)
	expect(t, first, 201, "create factor")
	firstAt, err := time.Parse(time.RFC3339Nano, str(first.body, "effective_at"))
	if err != nil {
		t.Fatal(err)
	}
	changed := admin.adminPost(factorsPath+"/OM5451/revisions", map[string]any{"percentage": "40", "expected_revision": 1})
	expect(t, changed, 200, "change factor")
	if changed.body["previous_percentage"] != "30" || changed.body["revision"] != float64(2) {
		t.Fatal("missing previous value/revision")
	}
	replay := admin.adminPostKey(factorsPath, body, idem)
	expect(t, replay, 201, "original create replay")
	h := admin.adminGet(factorsPath + "/OM5451/history")
	expect(t, h, 200, "history")
	if len(listItems(h)) != 2 {
		t.Fatalf("expected two immutable history entries: %s", h.raw)
	}
	ctx := tenant.WithID(context.Background(), f.tenant)
	for _, tc := range []struct {
		code string
		at   time.Time
		want string
	}{{"OM5451", firstAt.Add(-time.Nanosecond), "35"}, {"OM5451", firstAt, "30"}, {"OM5451", time.Time{}, "40"}, {"ST25", time.Time{}, "35"}} {
		v, err := f.mod.svc.StrawFactorAt(ctx, tc.code, tc.at)
		if err != nil || v.String() != tc.want {
			t.Fatalf("factor at %s: %s %v, want %s", tc.at, v, err, tc.want)
		}
	}
	if used, err := f.mod.svc.VarietyInUse(ctx, "OM5451"); err != nil || !used {
		t.Fatal("factor history must prevent hard deletion of its variety")
	}
	// A hidden variety retains its configured row and historical values.
	f.catalogs.put(mdport.CatalogRiceVariety, "OM5451", "OM5451", map[string]string{"crop_code": "LUA"}, false)
	after := admin.adminGet(factorsPath)
	expect(t, after, 200, "hidden row retained")
	found := false
	for _, row := range listItems(after) {
		if row["variety_code"] == "OM5451" {
			found = true
			if row["percentage"] != "40" || row["active"] != false {
				t.Fatal("hidden row changed history")
			}
		}
	}
	if !found {
		t.Fatal("hidden configured variety disappeared")
	}
	adminCtx := authz.WithPrincipal(ctx, authz.Principal{TenantID: f.tenant, ActorRef: admin.actor, Roles: []string{authz.RolePlatformAdmin}, MFA: true})
	f.mod.svc.Catalogs = nil
	receipt, err := f.mod.svc.SaveStrawFactor(adminCtx, "OM5451", "30", idem, 0, true)
	if err != nil || receipt.Seq != 1 {
		t.Fatalf("completed retry must not depend on current catalog availability: %v", err)
	}
}

func TestAH282FactorValidationMatrixAndIsolation(t *testing.T) {
	f := newFixture(t)
	admin := f.platformAdmin()
	for _, tc := range []struct{ code, pct, field string }{{"", "30", "variety_code"}, {"OM5451", "", "percentage"}, {"OM5451", "120", "percentage"}, {"OM5451", "abc", "percentage"}, {"OM5451", "-1", "percentage"}, {"OM5451", "35.00001", "percentage"}, {"OLDV", "35", "variety_code"}, {"ARA", "35", "variety_code"}} {
		r := admin.adminPost(factorsPath, map[string]any{"variety_code": tc.code, "percentage": tc.pct})
		if r.status != 422 || !hasField(r, tc.field) {
			t.Fatalf("validation %s/%s: %s", tc.code, tc.pct, r.raw)
		}
	}
	for _, who := range []caller{f.managerA(), f.memberA(), f.as(f.tenant, uuid.Nil, port.RoleSOPAdmin)} {
		expectCode(t, who.adminGet(factorsPath), 403, "forbidden", "FN11 read denied")
		expectCode(t, who.adminPost(factorsPath, map[string]any{"variety_code": "OM5451", "percentage": "35"}), 403, "forbidden", "FN11 write denied")
	}
	expect(t, admin.adminPost(factorsPath, map[string]any{"variety_code": "OM5451", "percentage": "0"}), 201, "zero allowed")
	expect(t, admin.adminPost(factorsPath, map[string]any{"variety_code": "ST25", "percentage": "100"}), 201, "100 allowed")
	expectCode(t, admin.adminPost(factorsPath, map[string]any{"variety_code": "OM5451", "percentage": "35"}), 409, "cropcycle.straw_factor_taken", "duplicate variety")
	expectCode(t, admin.adminPost(factorsPath+"/OM5451/revisions", map[string]any{"percentage": "40", "expected_revision": 99}), 409, "cropcycle.straw_factor_concurrent", "stale revision")
	other := f.as(ids.New(), uuid.Nil, authz.RolePlatformAdmin)
	foreign := other.adminGet(factorsPath + "/OM5451/history")
	expect(t, foreign, 200, "foreign tenant history")
	if len(listItems(foreign)) != 0 {
		t.Fatal("history leaked across tenants")
	}
	noMFA := admin
	noMFA.token = f.iss.Sign(t, authztest.Token{Subject: admin.actor, TenantID: f.tenant, Roles: []string{authz.RolePlatformAdmin}})
	if r := noMFA.adminGet(factorsPath); r.status != http.StatusForbidden {
		t.Fatalf("MFA required: %d", r.status)
	}
}

func listItems(r resp) []map[string]any {
	out := []map[string]any{}
	for _, row := range list(r.body, "items") {
		out = append(out, row.(map[string]any))
	}
	return out
}

func TestAH282ConcurrentEditsDoNotOverwrite(t *testing.T) {
	f := newFixture(t)
	admin := f.platformAdmin()
	expect(t, admin.adminPost(factorsPath, map[string]any{"variety_code": "OM5451", "percentage": "30"}), 201, "initial factor")
	ctx := tenant.WithID(authz.WithPrincipal(context.Background(), authz.Principal{TenantID: f.tenant, ActorRef: admin.actor, Roles: []string{authz.RolePlatformAdmin}, MFA: true}), f.tenant)
	var group sync.WaitGroup
	results := make(chan error, 2)
	for _, pct := range []string{"40", "50"} {
		group.Go(func() { _, err := f.mod.svc.SaveStrawFactor(ctx, "OM5451", pct, key(), 1, false); results <- err })
	}
	group.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else if !errors.Is(err, app.ErrStrawFactorConcurrent) {
			t.Fatalf("unexpected concurrent failure: %v", err)
		}
	}
	if success != 1 {
		t.Fatalf("exactly one concurrent writer must win, got %d", success)
	}
	h := admin.adminGet(factorsPath + "/OM5451/history")
	expect(t, h, 200, "history after concurrent edits")
	if len(listItems(h)) != 2 {
		t.Fatalf("concurrent edits produced extra history: %s", h.raw)
	}
}
