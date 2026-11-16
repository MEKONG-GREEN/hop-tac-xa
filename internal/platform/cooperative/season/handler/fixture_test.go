package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	port "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/app"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/handler"
	geoport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/geo/port"
	metricsport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/metrics/port"
	partyport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/party/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/party/port/porttest"
	syncport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/clock"
	shareddb "github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/db"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/httpx"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/inbox"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
	"github.com/MEKONG-GREEN/hop-tac-xa/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

// startTime: 29/09/2026 10:00 giờ Việt Nam.
var startTime = time.Date(2026, 9, 29, 3, 0, 0, 0, time.UTC)

const today = "2026-09-29"

type fixture struct {
	t        *testing.T
	iss      *authztest.FakeIssuer
	app      *httptest.Server
	admin    *httptest.Server
	mod      seasonMod
	db       tenant.Beginner // pool app_platform, giao dịch dưới r_cooperative
	pool     *pgxpool.Pool
	clk      *clock.Fixed
	plots    *fakePlots
	orgs     *fakeOrgs
	params   *fakeParams
	farmers  *fakeFarmers
	catalogs *fakeCatalogs
	tenant   uuid.UUID
	mkg      uuid.UUID         // tenant MKG riêng (ADR-0020) — tenant phong bì van, tenant tài khoản mkg_sync
	share    *porttest.Sharing // cấp quyền chia sẻ giả tenant → mkg (pf/party)
	coopA    uuid.UUID         // xã 31117
	coopB    uuid.UUID         // xã 31120
	knA      uuid.UUID         // tổ KN xã 31117
	h1, h2   uuid.UUID         // nông hộ của HTX A
	h3, h4   uuid.UUID         // h3: HTX A; h4: HTX B
	p1, p2   uuid.UUID         // thửa HTX A (h1, h2); p2 lệch GPS 120 m
	p3, p4   uuid.UUID         // p3: HTX A (h3); p4: HTX B (h4)
	p5       uuid.UUID         // thửa thứ hai của h1
	sopAdmin caller
	approver caller
}

func newFixture(t *testing.T) *fixture {
	env := pgtest.Get(t)
	iss := authztest.New(t)
	pool := env.Pool(t, migrations.PF, dbadmin.AppPlatform)
	ops := env.Pool(t, migrations.PF, "platform_api")
	f := &fixture{t: t, iss: iss, pool: pool, clk: clock.NewFixed(startTime), tenant: ids.New(), mkg: ids.New(), share: porttest.New(),
		coopA: ids.New(), coopB: ids.New(), knA: ids.New(), h1: ids.New(), h2: ids.New(), h3: ids.New(), h4: ids.New(),
		p1: ids.New(), p2: ids.New(), p3: ids.New(), p4: ids.New(), p5: ids.New()}
	f.seedSourceOrgs()
	sc := &fakeScope{scopes: map[uuid.UUID]authz.Scope{f.knA: {Units: []string{"31117"}}}}
	f.plots = &fakePlots{plots: map[uuid.UUID]port.Plot{}, dist: map[uuid.UUID]decimal.Decimal{}}
	area := decimal.RequireFromString("1.5000")
	for i, p := range []struct {
		ref, coop, hh uuid.UUID
		commune       string
		dist          int64
	}{{f.p1, f.coopA, f.h1, "31117", 10}, {f.p2, f.coopA, f.h2, "31117", 120}, {f.p3, f.coopA, f.h3, "31117", 0},
		{f.p4, f.coopB, f.h4, "31120", 5}, {f.p5, f.coopA, f.h1, "31117", 30}} {
		f.plots.put(port.Plot{Ref: p.ref, Code: "TH000000" + string(rune('1'+i)), CommuneCode: p.commune, CooperativeRef: p.coop,
			HouseholdRef: p.hh, Status: port.PlotActive, Version: 1, AreaHa: area}, p.dist)
	}
	f.orgs = &fakeOrgs{orgs: map[uuid.UUID]partyport.Org{
		f.coopA: {Ref: f.coopA, Kind: partyport.OrgCooperative, Code: "HTXA", AdminUnitCode: "31117", Active: true},
		f.coopB: {Ref: f.coopB, Kind: partyport.OrgCooperative, Code: "HTXB", AdminUnitCode: "31120", Active: true},
	}}
	f.params = &fakeParams{vals: map[string]decimal.Decimal{}}
	f.farmers = &fakeFarmers{links: map[uuid.UUID][]uuid.UUID{}}
	f.catalogs = newFakeCatalogs()
	f.db = shareddb.ForModule(pool, shareddb.Module("pf/cooperative"))
	f.mod = seasonMod{svc: &app.Service{DB: f.db, Clock: f.clk, Scope: sc, Orgs: f.orgs, Plots: f.plots, Params: f.params,
		Catalogs: f.catalogs, Farmers: f.farmers}}
	f.share.Grant(f.tenant, f.mkg, partyport.ShareKinds...) // ADR-0020: tenant chương trình → tenant MKG, mọi loại
	f.mod.UseSharing(f.share, f.share)
	log := telemetry.NewLogger("test", io.Discard)
	appEng := httpx.NewEngine(log, 4<<20)
	f.mod.RegisterApp(appEng.Group("/v1", httpx.Authenticate(iss.Verifier()), idempotency.Middleware(ops)))
	adminEng := httpx.NewEngine(log, 4<<20)
	f.mod.RegisterAdmin(adminEng.Group("/v1", httpx.Authenticate(iss.Verifier()), httpx.RequireMFA(), idempotency.Middleware(ops)))
	f.app, f.admin = httptest.NewServer(appEng), httptest.NewServer(adminEng)
	t.Cleanup(f.app.Close)
	t.Cleanup(f.admin.Close)
	f.sopAdmin = f.as(f.tenant, uuid.Nil, port.RoleSOPAdmin)
	f.approver = f.as(f.tenant, uuid.Nil, authz.RoleParamApprover)
	return f
}

type caller struct {
	f     *fixture
	tid   uuid.UUID
	org   uuid.UUID
	token string
	actor uuid.UUID
}

func (f *fixture) as(tid, org uuid.UUID, roles ...string) caller {
	actor := ids.New()
	tok := f.iss.Sign(f.t, authztest.Token{Subject: actor, TenantID: tid, Roles: roles, OrgRef: org, MFA: true})
	return caller{f: f, tid: tid, org: org, token: tok, actor: actor}
}

func (f *fixture) platformAdmin() caller { return f.as(f.tenant, uuid.Nil, authz.RolePlatformAdmin) }
func (f *fixture) managerA() caller      { return f.as(f.tenant, f.coopA, port.RoleCoopManager) }
func (f *fixture) managerB() caller      { return f.as(f.tenant, f.coopB, port.RoleCoopManager) }
func (f *fixture) officerA() caller      { return f.as(f.tenant, f.knA, partyport.RoleKNOfficer) }

// farmer tạo tài khoản nông dân liên kết với các nông hộ.
func (f *fixture) farmer(hh ...uuid.UUID) caller {
	c := f.as(f.tenant, f.coopA, port.RoleFarmer)
	f.farmers.link(c.actor, hh...)
	return c
}

type resp struct {
	status int
	raw    []byte
	body   map[string]any
}

func key() string { return "k-" + ids.New().String() }

func (c caller) do(base, method, path, idem, ctype string, body []byte) resp {
	c.f.t.Helper()
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, _ := http.NewRequest(method, base+path, rd)
	req.Header.Set("Authorization", "Bearer "+c.token)
	if idem != "" {
		req.Header.Set("Idempotency-Key", idem)
	}
	req.Header.Set("Content-Type", ctype)
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		c.f.t.Fatal(err)
	}
	defer func() { _ = r.Body.Close() }()
	raw, _ := io.ReadAll(r.Body)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return resp{status: r.StatusCode, raw: raw, body: m}
}

func (c caller) post(path string, body any) resp {
	c.f.t.Helper()
	return c.postKey(path, key(), body)
}

func (c caller) postKey(path, idem string, body any) resp {
	c.f.t.Helper()
	b, _ := json.Marshal(body)
	return c.do(c.f.app.URL, http.MethodPost, path, idem, "application/json", b)
}

func (c caller) get(path string) resp {
	c.f.t.Helper()
	return c.do(c.f.app.URL, http.MethodGet, path, "", "application/json", nil)
}

func (c caller) adminPost(path string, body any) resp {
	c.f.t.Helper()
	b, _ := json.Marshal(body)
	return c.do(c.f.admin.URL, http.MethodPost, path, key(), "application/json", b)
}

// adminPostKey như adminPost nhưng dùng đúng Idempotency-Key cho trước (kiểm gọi lặp cùng khóa).
func (c caller) adminPostKey(path string, body any, idemKey string) resp {
	c.f.t.Helper()
	b, _ := json.Marshal(body)
	return c.do(c.f.admin.URL, http.MethodPost, path, idemKey, "application/json", b)
}

// adminPostRaw gửi thân thô (vd tệp .xlsx) tới Admin API với Idempotency-Key cho trước ("" = không gửi header).
func (c caller) adminPostRaw(path, ctype string, body []byte, idemKey string) resp {
	c.f.t.Helper()
	return c.do(c.f.admin.URL, http.MethodPost, path, idemKey, ctype, body)
}

func (c caller) adminGet(path string) resp {
	c.f.t.Helper()
	return c.do(c.f.admin.URL, http.MethodGet, path, "", "application/json", nil)
}

func get(m any, path ...string) any {
	cur := m
	for _, p := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = mm[p]
	}
	return cur
}

func str(m any, path ...string) string {
	s, _ := get(m, path...).(string)
	return s
}

func num(m any, path ...string) float64 {
	n, _ := get(m, path...).(float64)
	return n
}

func list(m any, path ...string) []any {
	l, _ := get(m, path...).([]any)
	return l
}

func code(r resp) string { return str(r.body, "code") }

func expect(t *testing.T, r resp, status int, what string) {
	t.Helper()
	if r.status != status {
		t.Fatalf("%s: HTTP %d, muốn %d: %s", what, r.status, status, r.raw)
	}
}

func expectCode(t *testing.T, r resp, status int, c, what string) {
	t.Helper()
	if r.status != status || code(r) != c {
		t.Fatalf("%s: HTTP %d %s, muốn %d %s: %s", what, r.status, code(r), status, c, r.raw)
	}
}

// dateRange dựng cửa sổ Theo ngày so với NXG.
func dateRange(from, to int) map[string]any {
	return map[string]any{"kind": "date_range", "anchor": "nxg", "from": from, "to": to}
}

// sopSteps là các việc của quy trình mẫu: làm đất, gieo sạ (giai đoạn 1), bón phân, phun thuốc (PHI 14)
// (giai đoạn 2), thu hoạch (giai đoạn 3, ngày 95–100 so với mốc gốc).
func sopSteps() []map[string]any {
	return []map[string]any{
		{"seq": 1, "stage_seq": 1, "task_seq": 1, "name": "Làm đất", "activity_code": "lam_dat", "stage_code": "lam_dat", "window": dateRange(-3, -1), "mandatory": true},
		{"seq": 2, "stage_seq": 1, "task_seq": 2, "name": "Gieo sạ", "activity_code": "gieo_sa", "window": dateRange(0, 1), "mandatory": true, "evidence_kinds": []string{"anh_hien_truong"}},
		{"seq": 3, "stage_seq": 2, "task_seq": 1, "name": "Bón thúc", "activity_code": "bon_phan", "window": dateRange(10, 13), "mandatory": true,
			"materials": []map[string]any{{"material_code": "URE", "dose_per_ha": "50", "unit": "kg"}}},
		{"seq": 4, "stage_seq": 2, "task_seq": 2, "name": "Phun thuốc", "activity_code": "phun_thuoc", "window": dateRange(40, 43), "mandatory": false, "phi_days": 14},
		{"seq": 5, "stage_seq": 3, "task_seq": 1, "name": "Thu hoạch", "activity_code": "thu_hoach", "window": dateRange(95, 100), "mandatory": true},
	}
}

func sopStages() []map[string]any {
	return []map[string]any{
		{"seq": 1, "name": "Chuẩn bị", "from_day": -3, "to_day": 1},
		{"seq": 2, "name": "Chăm sóc", "from_day": 2, "to_day": 90},
		{"seq": 3, "name": "Thu hoạch", "from_day": 91, "to_day": 100},
	}
}

// sopBody là thân tạo SOP lúa (loại cây LUA) với nội dung mẫu; extra ghi đè hoặc thêm trường.
func sopBody(name string, extra map[string]any) map[string]any {
	body := map[string]any{"name": name, "crop": "LUA", "anchor_name": "Ngày xuống giống", "stages": sopStages(), "steps": sopSteps()}
	for k, v := range extra {
		body[k] = v
	}
	return body
}

// publishedSOP tạo, phát hành mẫu SOP hệ thống và (nếu variety ≠ "") gắn với giống.
func (f *fixture) publishedSOP(name, variety string) (tmpl, version string) {
	f.t.Helper()
	r := f.sopAdmin.adminPost("/v1/cropcycle/sops", sopBody(name, map[string]any{"code": "SOP-" + ids.New().String()[30:]}))
	expect(f.t, r, http.StatusCreated, "tạo SOP")
	tmpl, version = str(r.body, "ref"), str(list(r.body, "versions")[0], "id")
	expect(f.t, f.sopAdmin.adminPost("/v1/cropcycle/sop-versions/"+version+"/publish", nil), http.StatusOK, "phát hành SOP")
	if variety != "" {
		expect(f.t, f.sopAdmin.adminPost("/v1/cropcycle/variety-bindings", map[string]any{"variety_code": variety, "template_ref": tmpl}), http.StatusOK, "gắn giống")
	}
	return tmpl, version
}

// open mở vụ cho thửa bằng caller (giống OM5451, ngày sạ hôm nay, GPS thiết bị).
func (c caller) open(plot uuid.UUID, extra map[string]any) resp {
	c.f.t.Helper()
	body := map[string]any{"plot_ref": plot.String(), "season_code": "DX", "season_year": 2026, "variety_code": "OM5451",
		"sowing_date": c.f.clk.Now().In(clock.Zone()).Format(time.DateOnly), "gps": map[string]any{"lon": "105.6", "lat": "10.05", "accuracy_m": "4"}}
	for k, v := range extra {
		body[k] = v
	}
	return c.post("/v1/cropcycle/seasons", body)
}

func (c caller) mustOpen(plot uuid.UUID) string {
	c.f.t.Helper()
	r := c.open(plot, nil)
	expect(c.f.t, r, http.StatusCreated, "mở vụ")
	return str(r.body, "season", "season_ref")
}

// log ghi nhật ký cho thửa.
func (c caller) log(plot uuid.UUID, extra map[string]any) resp {
	c.f.t.Helper()
	body := map[string]any{"plot_ref": plot.String(), "activity_code": "bon_phan", "sop_step_seq": 3,
		"gps": map[string]any{"lon": "105.6", "lat": "10.05"}, "machine_codes": []string{"MPT"},
		"inputs": []map[string]any{{"name": "NPK 20-20-15", "qty": "50", "unit": "kg"}}, "note": "Bón thúc đợt 1"}
	for k, v := range extra {
		body[k] = v
	}
	return c.post("/v1/cropcycle/farm-logs", body)
}

func (f *fixture) outbox(eventType string) []map[string]any {
	f.t.Helper()
	var out []map[string]any
	err := tenant.WithTenantTx(context.Background(), f.db, f.tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(context.Background(), `SELECT payload FROM shared.outbox WHERE event_type = $1::text ORDER BY id`, eventType)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var raw []byte
			if err := rows.Scan(&raw); err != nil {
				return err
			}
			var m map[string]any
			if err := json.Unmarshal(raw, &m); err != nil {
				return err
			}
			out = append(out, m)
		}
		return rows.Err()
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return out
}

// advanceDays dời đồng hồ nghiệp vụ n ngày.
func (f *fixture) advanceDays(n int) { f.clk.Advance(time.Duration(n) * 24 * time.Hour) }

// seasonMod gói Service vụ mùa như các lối vào của module pf/cooperative mà kiểm thử dùng (W3.3: vụ chuyển từ
// pf/cropcycle — kiểm thử dựng thẳng Service với Plots giả thay vì cả module).
type seasonMod struct{ svc *app.Service }

func (m seasonMod) RegisterApp(rg *gin.RouterGroup)   { handler.Handler{Svc: m.svc}.RegisterApp(rg) }
func (m seasonMod) RegisterAdmin(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.RegisterAdmin(rg) }
func (m seasonMod) ReportData() app.ReportData        { return app.ReportData{Svc: m.svc} }
func (m seasonMod) Stats() *app.Service               { return m.svc }
func (m seasonMod) SupplyStats() *app.Service         { return m.svc }
func (m seasonMod) MetricsSource() metricsport.Source { return app.MetricsSource{Svc: m.svc} }
func (m seasonMod) SyncSource() syncport.Source       { return app.SyncSource{Svc: m.svc} }
func (m seasonMod) SyncSink() syncport.Sink           { return app.SyncSink{Svc: m.svc} }
func (m seasonMod) GpsLogHandler() inbox.Handler      { return m.svc.HandleGpsLog }
func (m seasonMod) UseGpsLog(g geoport.GpsLog)        { m.svc.GpsLog = g }
func (m seasonMod) UseSharing(pa partyport.PartnerAccess, _ partyport.ValveGrants) {
	m.svc.Sharing = pa
}
