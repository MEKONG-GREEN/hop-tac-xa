package handler_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/metrics/app"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/metrics/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/clock"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
)

// seedCoopSeason ghi (bằng tài khoản quản trị — dựng dữ liệu) một vụ thửa mở và một khai thu hoạch của HTX coop ở xã
// commune, vụ HE_THU năm d; seq, prev_hash, row_hash do trigger sổ điền.
func (f *fixture) seedCoopSeason(coop uuid.UUID, commune string, d clock.Date, area, mass string) {
	f.t.Helper()
	ctx := context.Background()
	tx, err := f.owner.Begin(ctx)
	if err != nil {
		f.t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, f.tenant.String()); err != nil {
		f.t.Fatal(err)
	}
	season, plot, day := ids.New(), ids.New(), d.String()
	if _, err := tx.Exec(ctx, `INSERT INTO pf_coop.season_event (id, tenant_id, stream_key, season_ref, action, plot_ref, plot_code,
	    cooperative_ref, commune_code, season_code, season_year, variety_code, sop_template_ref, sop_version_id, sop_version_no,
	    sowing_date, expected_harvest_date, harvest_open_date, area_ha, gps_source, location_status, on_behalf, channel,
	    event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'season:' || $3::uuid::text, $3, 'opened', $4, 'T-01', $5, $6, 'HE_THU', $7, 'OM5451', $8, $9, 1,
	          $10::date, $10::date, $10::date, $11::numeric, 'no_device', 'missing', false, 'web', now(), $12, $13, 'test')`,
		ids.New(), f.tenant, season, plot, coop, commune, d.Year, ids.New(), ids.New(), day, area, ids.New(), ids.New().String()); err != nil {
		f.t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO pf_coop.harvest_declaration (id, tenant_id, stream_key, season_ref, plot_ref,
	    cooperative_ref, commune_code, season_code, season_year, variety_code, harvest_date, mass_kg, area_ha, yield_t_per_ha,
	    anomaly_confirmed, source, gps_source, location_status, on_behalf, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'harvest:' || $3::uuid::text, $3, $4, $5, $6, 'he_thu', $7, 'OM5451', $8::date, $9::numeric, $10::numeric,
	          $9::numeric / 1000 / $10::numeric, false, 'app', 'no_device', 'missing', false, now(), $11, $12, 'test')`,
		ids.New(), f.tenant, season, plot, coop, commune, d.Year, day, mass, area, ids.New(), ids.New().String()); err != nil {
		f.t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		f.t.Fatal(err)
	}
}

// liveSource là nguồn _pub thật tính với mốc hiện tại thay cho Cutoff(D): sổ nguồn chỉ nhận recorded_at = now() còn
// daily_value chỉ nhận recorded_at ≥ Cutoff(D), nên dữ liệu dựng trong kiểm thử không thể nằm trước Cutoff của ngày
// đã ghi sổ. Công thức SQL và phạm vi theo xã giữ nguyên.
type liveSource struct {
	app.PubSource
	today clock.Date
}

func (s liveSource) AsOf(ctx context.Context, k port.Key, d clock.Date, sc port.Scope) (port.Value, error) {
	return s.AsOfAt(ctx, k, d, time.Now(), sc)
}

func (s liveSource) Scopes(ctx context.Context, k port.Key, _ clock.Date, tid uuid.UUID) ([]port.Scope, error) {
	return s.PubSource.Scopes(ctx, k, s.today, tid)
}

// asCoop: người dùng App HTX gắn HTX coop (org_ref = Nút), phạm vi theo xã commune của HTX.
func (f *fixture) asCoop(coop uuid.UUID, commune string, roles ...string) caller {
	actor := ids.New()
	f.scopes.mu.Lock()
	f.scopes.by[actor] = authz.Scope{Units: []string{commune}}
	f.scopes.mu.Unlock()
	return caller{f: f, token: f.iss.Sign(f.t, authztest.Token{Subject: actor, TenantID: f.tenant, OrgRef: coop, Roles: roles})}
}

// FN-06/BR-02 (AH-306): xã 31117 có hai HTX. Sổ chỉ số ghi một dòng cho cả xã (A + B); cán bộ khuyến nông vẫn đọc được
// số theo xã. Đại diện HTX A (và Thành viên HTX) bị từ chối ở mọi dạng gọi — nếu đọc được tổng xã 4 ha thì trừ 1,5 ha
// của A ra 2,5 ha của B; cũng không lộ danh sách vụ đang có dữ liệu trong xã.
func TestFN06_BR02_CommuneMetricsHiddenFromCoopRoles(t *testing.T) {
	f := newFixture(t, clock.NewFixed(time.Now()))
	f.mod.Service().Sources["pf/cropcycle"] = liveSource{f.mod.PubSource("pf/cropcycle"), clock.BusinessDate(time.Now())}
	ctx := context.Background()
	d := yesterday()
	dim := port.SeasonDimension("HE_THU", d.Year)
	const commune = "31117"
	coopA := ids.New()
	f.seedCoopSeason(coopA, commune, d, "1.5", "9000")      // HTX A: 6 t/ha
	f.seedCoopSeason(ids.New(), commune, d, "2.5", "10000") // HTX B: 4 t/ha
	keys := []port.Key{"cc.season_area_ha", "cc.yield_t_per_ha"}
	if r, err := f.mod.Service().RecordDay(ctx, f.tenant, d, keys); err != nil || r.Inserted == 0 {
		t.Fatalf("ghi sổ: %+v %v", r, err)
	}

	period := "&from=" + d.String() + "&to=" + d.String()
	kn := f.as(f.tenant, authz.Scope{Units: []string{"31", commune}}, "kn_province_admin")
	for key, want := range map[port.Key]string{"cc.season_area_ha": "4", "cc.yield_t_per_ha": "4.75"} {
		base := "/v1/metrics/" + string(key) + "/values?unit=" + commune
		res := kn.get(base + "&dimension=" + dim + period)
		expect(t, res, 200, "")
		it := res.items()
		if len(it) != 1 {
			t.Fatalf("%s: khuyến nông đọc số theo xã: %v", key, it)
		}
		if v, _ := it[0]["value"].(string); !decimal.RequireFromString(v).Equal(decimal.RequireFromString(want)) {
			t.Fatalf("%s: xã %s = %s, mong %s", key, commune, v, want)
		}
		res = kn.get(base)
		expect(t, res, 200, "")
		if dims, _ := res.body["dimensions"].([]any); len(dims) != 1 || dims[0] != dim {
			t.Fatalf("%s: dimensions %v", key, res.body)
		}
	}

	callers := map[string]caller{
		"đại diện HTX A":             f.asCoop(coopA, commune, htxroles.Representative),
		"thành viên HTX A":           f.asCoop(coopA, commune, htxroles.Member),
		"đại diện kiêm thành viên A": f.asCoop(coopA, commune, htxroles.Representative, htxroles.Member),
	}
	for name, c := range callers {
		for _, key := range keys {
			base := "/v1/metrics/" + string(key) + "/values"
			for _, q := range []string{"?unit=" + commune + "&dimension=" + dim + period, "?unit=" + commune, ""} {
				res := c.get(base + q)
				expect(t, res, 403, "forbidden")
				if _, ok := res.body["items"]; ok {
					t.Fatalf("%s %s%s: lộ số %v", name, key, q, res.body)
				}
				if _, ok := res.body["dimensions"]; ok {
					t.Fatalf("%s %s%s: lộ danh sách vụ %v", name, key, q, res.body)
				}
			}
		}
	}
}
