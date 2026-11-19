package masterdata_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/masterdata"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/party"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/clock"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/httpx"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/hop-tac-xa/migrations"
)

func TestAH271_HTXCountsUsePartyOutsideCatalogTransaction(t *testing.T) {
	f := newFixture(t)
	tid := f.seeded()
	otherTenant := f.seeded()
	admin := f.caller(tid, authz.RolePlatformAdmin)
	_, provinces := admin.do(f.app, http.MethodGet, "/v1/masterdata/admin-units?limit=1", "", nil)
	province := provinces["items"].([]any)[0].(map[string]any)["code"].(string)
	_, communes := admin.do(f.app, http.MethodGet, "/v1/masterdata/admin-units?parent="+province+"&limit=1", "", nil)
	commune := communes["items"].([]any)[0].(map[string]any)["code"].(string)
	for i, tenantID := range []any{tid, tid, otherTenant} {
		ref := ids.New()
		_, err := f.env.AdminPool(t, migrations.PF).Exec(context.Background(), `INSERT INTO pf_party.org
 (tenant_id,ref,kind,code,name,admin_unit_code,active,last_event_id,last_seq)
 VALUES($1::uuid,$2::uuid,'cooperative',$3::text,'HTX kiểm đếm',$4::text,$5::boolean,$6::uuid,1)`,
			tenantID, ref, "HTX-"+ref.String(), commune, i == 0, ids.New())
		if err != nil {
			t.Fatal(err)
		}
	}
	// Both modules use the same pool with one connection. Calling party while
	// holding masterdata's transaction would time out instead of returning 2.
	config := f.env.Pool(t, migrations.PF, dbadmin.AppPlatform).Config()
	config.MaxConns = 1
	config.MinConns = 0
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	md := masterdata.New(pool, clock.System{})
	pty := party.New(pool, clock.System{}, party.Deps{AdminUnits: md.AdminUnits()})
	md.UseUnitUsage(pty.UnitCounts())
	engine := httpx.NewEngine(telemetry.NewLogger("test", io.Discard), 1<<20)
	md.RegisterAdmin(engine.Group("/v1", httpx.Authenticate(f.iss.Verifier()), httpx.RequireMFA()))
	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+managedUnitsPath+"?parent="+province, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+admin.token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var body struct {
		Items []struct {
			Code  string `json:"code"`
			Count *int64 `json:"cooperative_count"`
		} `json:"items"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 {
		t.Fatalf("counts: %d", response.StatusCode)
	}
	for _, row := range body.Items {
		if row.Code == commune {
			if row.Count == nil || *row.Count != 2 {
				t.Fatalf("count must include inactive HTX and exclude another tenant: %+v", row)
			}
			return
		}
	}
	t.Fatal("commune missing from catalog")
}
