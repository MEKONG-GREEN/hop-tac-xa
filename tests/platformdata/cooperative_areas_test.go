package platformdata_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/masterdata"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/party"
	pp "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/party/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/clock"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/httpx"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/hop-tac-xa/migrations"
)

type countedAreas struct {
	source pp.CooperativeAreaSource
	calls  atomic.Int64
}

func (c *countedAreas) CooperativeAreas(ctx context.Context, refs []uuid.UUID) (map[uuid.UUID]pp.CooperativeAreas, error) {
	c.calls.Add(1)
	return c.source.CooperativeAreas(ctx, refs)
}

// Cùng một pool chỉ một connection: đọc số liệu hộ khi còn giữ transaction của
// party sẽ deadlock. Không dùng fake nguồn, không sửa golden hợp đồng đọc cũ.
func TestAH283CooperativeAreaWiring(t *testing.T) {
	f := newFixture(t)
	for i, tid := range []uuid.UUID{f.tid, f.tid, f.other} {
		household := ids.New()
		f.exec(`INSERT INTO pf_coop.household(tenant_id,household_ref,commune_code,cultivated_area_ha,livestock_area_ha,active,created_by)
VALUES($1,$2,'89011',1.5,0.5,$3,$4)`, tid, household, i != 1, ids.New())
		f.exec(`INSERT INTO pf_coop.household_membership(tenant_id,id,household_ref,org_ref,since_date)
VALUES($1,$2,$3,$4,'2026-01-01')`, tid, ids.New(), household, ref(103))
	}
	config := pgtest.Get(t).Pool(t, migrations.PF, dbadmin.AppPlatform).Config()
	config.MaxConns, config.MinConns = 1, 0
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	md := masterdata.New(pool, clock.System{})
	pty := party.New(pool, clock.System{}, party.Deps{AdminUnits: md.AdminUnits()})
	coop := cooperative.New(pool, clock.System{}, cooperative.Deps{Orgs: pty.Orgs(), Dir: pty.OrgDirectory(), Scope: pty.ScopeResolver(), Consents: pty.ConsentStates()})
	pty.UseCoopProfiles(coop.CoopProfiles())
	areas := &countedAreas{source: coop.CooperativeAreas()}
	pty.UseCooperativeAreas(areas)
	issuer := authztest.New(t)
	engine := httpx.NewEngine(telemetry.NewLogger("test", io.Discard), 1<<20)
	pty.RegisterApp(engine.Group("/v1", httpx.Authenticate(issuer.Verifier())))
	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)
	get := func(org uuid.UUID, role string, target uuid.UUID) (int, map[string]any) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/v1/cooperatives/"+target.String(), nil)
		if err != nil {
			t.Fatal(err)
		}
		token := issuer.Sign(t, authztest.Token{Subject: ids.New(), TenantID: f.tid, OrgRef: org, Roles: []string{role}})
		req.Header.Set("Authorization", "Bearer "+token)
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = response.Body.Close() }()
		var body map[string]any
		if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		return response.StatusCode, body
	}
	status, body := get(uuid.Nil, authz.RolePlatformAdmin, ref(103))
	if status != http.StatusOK {
		t.Fatalf("đọc hồ sơ: %d %v", status, body)
	}
	summary, ok := body["household_areas"].(map[string]any)
	if !ok || summary["cultivated_ha"] != "3" || summary["livestock_ha"] != "1" || summary["households"] != float64(2) {
		t.Fatalf("tổng hộ HTX, gồm hộ ngừng và loại tenant khác: %v", body)
	}
	before := areas.calls.Load()
	if status, _ := get(ref(107), pp.RoleCoopManager, ref(103)); status != http.StatusNotFound {
		t.Fatalf("Đại diện xem HTX khác: %d", status)
	}
	if areas.calls.Load() != before {
		t.Fatal("đã đọc nguồn diện tích trước khi kiểm quyền HTX")
	}
}
