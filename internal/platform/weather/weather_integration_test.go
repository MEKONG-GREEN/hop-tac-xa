package weather_test

// pf/weather với DB thật (vai đăng nhập app_platform → r_weather), đồng hồ cố định, nhà cung cấp giả và HTX giả.
// FN-TT-01..03 (AH-224, AH-225, AH-226).

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	coopport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/weather"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/weather/app"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/weather/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/weather/provider/fake"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/clock"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
	"github.com/MEKONG-GREEN/hop-tac-xa/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

type coops struct{ sites []coopport.CooperativeSite }

func (c coops) ByUnit(context.Context, []string) ([]coopport.UnitCooperatives, error) {
	return nil, nil
}
func (c coops) Active(context.Context) ([]coopport.CooperativeSite, error) { return c.sites, nil }
func (c coops) Centroids(context.Context, []uuid.UUID) (map[uuid.UUID]coopport.CoopCentroid, error) {
	return nil, nil
}

func dec(s string) *decimal.Decimal { d := decimal.RequireFromString(s); return &d }

type fixture struct {
	t      *testing.T
	env    *pgtest.Env
	clk    *clock.Fixed
	prov   *fake.Provider
	mu     sync.Mutex
	keys   []string
	mod    *weather.Module
	tenant uuid.UUID
	coopA  uuid.UUID // trụ sở ở Cần Thơ
	coopB  uuid.UUID // cách A khoảng 0,8 km — dùng chung điểm (BR-02)
	coopC  uuid.UUID // xa
	coopD  uuid.UUID // chưa có tọa độ
	admin  authz.Principal
	app    *gin.Engine
	admAPI *gin.Engine
}

func newFixture(t *testing.T, at time.Time) *fixture {
	env := pgtest.Get(t)
	f := &fixture{t: t, env: env, clk: clock.NewFixed(at), tenant: ids.New(), coopA: ids.New(), coopB: ids.New(), coopC: ids.New(), coopD: ids.New()}
	f.prov = &fake.Provider{Now: f.clk.Now}
	sites := coops{sites: []coopport.CooperativeSite{
		{Ref: f.coopA, Lat: dec("10.0312"), Lon: dec("105.7801")},
		{Ref: f.coopB, Lat: dec("10.0360"), Lon: dec("105.7840")},
		{Ref: f.coopC, Lat: dec("10.3500"), Lon: dec("105.4300")},
		{Ref: f.coopD},
	}}
	factory := func(provider, key string) (app.Provider, error) {
		f.mu.Lock()
		f.keys = append(f.keys, key)
		f.mu.Unlock()
		return f.prov, nil
	}
	unit := env.Pool(t, migrations.PF, dbadmin.AppPlatform)
	f.mod = weather.New(unit, f.clk, weather.Deps{Coops: sites, Providers: factory, SecretKey: bytes.Repeat([]byte{9}, 32), AllowFake: true})
	f.admin = authz.Principal{ActorRef: ids.New(), TenantID: f.tenant, Roles: []string{htxroles.Admin}, MFA: true}
	f.app, f.admAPI = gin.New(), gin.New()
	f.mod.RegisterApp(f.app.Group("/v1"))
	f.mod.RegisterAdmin(f.admAPI.Group("/v1"))
	return f
}

func (f *fixture) member(coop uuid.UUID, role string) authz.Principal {
	return authz.Principal{ActorRef: ids.New(), TenantID: f.tenant, OrgRef: coop, Roles: []string{role}}
}

type reply struct {
	status int
	raw    string
	body   map[string]any
}

func (f *fixture) call(eng *gin.Engine, p authz.Principal, method, path, idem string, body any) reply {
	f.t.Helper()
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rd)
	if idem != "" {
		req.Header.Set("Idempotency-Key", idem)
	}
	ctx := tenant.WithID(authz.WithPrincipal(context.Background(), p), p.TenantID)
	rec := httptest.NewRecorder()
	eng.ServeHTTP(rec, req.WithContext(ctx))
	r := reply{status: rec.Code, raw: rec.Body.String()}
	_ = json.Unmarshal(rec.Body.Bytes(), &r.body)
	return r
}

func (f *fixture) tenants(context.Context) ([]uuid.UUID, error) { return []uuid.UUID{f.tenant}, nil }

func (f *fixture) save(cfg map[string]any) reply {
	f.t.Helper()
	return f.call(f.admAPI, f.admin, http.MethodPost, "/v1/weather/config/revisions", ids.New().String(), cfg)
}

func (f *fixture) run() app.FetchResult {
	f.t.Helper()
	r, err := f.mod.RunFetch(context.Background(), f.tenants)
	if err != nil {
		f.t.Fatalf("RunFetch: %v", err)
	}
	return r
}

func (f *fixture) failingEvents() int {
	f.t.Helper()
	var n int
	if err := f.env.AdminPool(f.t, migrations.PF).QueryRow(context.Background(),
		`SELECT count(*) FROM shared.outbox WHERE tenant_id = $1 AND event_type = 'pf.weather.fetch.failing'`, f.tenant).Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n
}

func path(base string, kv ...string) string {
	var b strings.Builder
	b.WriteString(base)
	for i := 0; i+1 < len(kv); i += 2 {
		if i == 0 {
			b.WriteString("?")
		} else {
			b.WriteString("&")
		}
		b.WriteString(kv[i] + "=" + kv[i+1])
	}
	return b.String()
}

// FN-TT-03: cấu hình (AC-03, AC-04, EX-01), lấy số liệu (AC-01, BR-02), lỗi liên tiếp (AC-05, EX-02), vượt hạn mức (EX-03).
func TestWeather_FNTT03_KetNoiAPI(t *testing.T) {
	f := newFixture(t, time.Date(2026, 11, 23, 9, 0, 0, 0, domain.VN))
	// Chưa cấu hình: mặc định, không gọi nhà cung cấp.
	if r := f.call(f.admAPI, f.admin, http.MethodGet, "/v1/weather/config", "", nil); r.status != 200 || r.body["configured"] != false || r.body["refresh_minutes"] != 60.0 {
		t.Fatalf("cấu hình mặc định: %d %s", r.status, r.raw)
	}
	if r := f.run(); r.Tenants != 1 || r.Due != 0 || f.prov.CallCount() != 0 {
		t.Fatalf("chưa cấu hình không được gọi nhà cung cấp: %+v", r)
	}
	// Vai khác Admin không có chức năng này.
	if r := f.call(f.admAPI, f.member(f.coopA, htxroles.Representative), http.MethodGet, "/v1/weather/config", "", nil); r.status != http.StatusForbidden {
		t.Fatalf("Đại diện HTX đọc cấu hình: %d", r.status)
	}
	// EX-01, AC-04: kiểm tra kết nối lỗi thì không lưu.
	cfg := map[string]any{"provider": "fake", "refresh_minutes": 60, "forecast_days": 7, "api_key": "abcd-efgh-1234"}
	f.prov.SetFail(domain.ErrCodeAuth)
	if r := f.call(f.admAPI, f.admin, http.MethodPost, "/v1/weather/config/test", ids.New().String(), cfg); r.status != 422 || r.body["code"] != "weather.connection_failed" ||
		!strings.Contains(r.raw, "Khóa API không đúng") {
		t.Fatalf("kiểm tra lỗi: %d %s", r.status, r.raw)
	}
	if r := f.save(cfg); r.status != 422 || r.body["code"] != "weather.connection_failed" {
		t.Fatalf("lưu khi kết nối lỗi: %d %s", r.status, r.raw)
	}
	if r := f.call(f.admAPI, f.admin, http.MethodGet, "/v1/weather/config", "", nil); r.body["configured"] != false {
		t.Fatal("không được lưu khi kiểm tra lỗi")
	}
	f.prov.SetFail("")
	if r := f.call(f.admAPI, f.admin, http.MethodPost, "/v1/weather/config/test", ids.New().String(), cfg); r.status != 200 || r.body["ok"] != true {
		t.Fatalf("kiểm tra đạt: %d %s", r.status, r.raw)
	}
	// AC-03: khóa chỉ hiện 4 ký tự cuối; bản lưu là bản mã.
	idem := ids.New().String()
	r := f.call(f.admAPI, f.admin, http.MethodPost, "/v1/weather/config/revisions", idem, cfg)
	if r.status != 201 || r.body["api_key_masked"] != "••••1234" || strings.Contains(r.raw, "abcd-efgh") {
		t.Fatalf("lưu: %d %s", r.status, r.raw)
	}
	var cipher []byte
	if err := f.env.AdminPool(t, migrations.PF).QueryRow(context.Background(),
		`SELECT api_key_cipher FROM pf_weather.provider_config WHERE tenant_id = $1 ORDER BY seq DESC LIMIT 1`, f.tenant).Scan(&cipher); err != nil ||
		bytes.Contains(cipher, []byte("abcd")) || len(cipher) == 0 {
		t.Fatalf("khóa phải lưu mã hóa: %v", err)
	}
	if again := f.call(f.admAPI, f.admin, http.MethodPost, "/v1/weather/config/revisions", idem, cfg); again.status != 200 || again.body["version"] != r.body["version"] {
		t.Fatalf("phát lại: %d %s", again.status, again.raw)
	}
	// Lưu lại không gửi khóa → giữ khóa cũ (giải mã đúng).
	if r := f.save(map[string]any{"provider": "fake", "refresh_minutes": 60, "forecast_days": 7}); r.status != 201 || r.body["api_key_masked"] != "••••1234" {
		t.Fatalf("giữ khóa: %d %s", r.status, r.raw)
	}
	if f.keys[len(f.keys)-1] != "abcd-efgh-1234" {
		t.Fatal("khóa cũ phải được giải mã để kiểm tra kết nối")
	}

	// BR-02: A và B cách dưới 1 km dùng chung một lần gọi; D chưa có tọa độ bị bỏ qua → 2 điểm.
	calls := f.prov.CallCount()
	if r := f.run(); r.Due != 1 || r.Locations != 2 || f.prov.CallCount()-calls != 2 {
		t.Fatalf("lượt đầu: %+v, gọi %d", r, f.prov.CallCount()-calls)
	}
	var pts int
	_ = f.env.AdminPool(t, migrations.PF).QueryRow(context.Background(), `SELECT count(*) FROM pf_weather.location WHERE tenant_id = $1`, f.tenant).Scan(&pts)
	if pts != 2 {
		t.Fatalf("điểm lấy số liệu: %d", pts)
	}
	// Chưa đến lượt (60 phút).
	f.clk.Advance(30 * time.Minute)
	if r := f.run(); r.Due != 0 {
		t.Fatalf("chưa đến lượt: %+v", r)
	}
	// AC-01: đổi tần suất 30 phút có hiệu lực ở lần lấy kế tiếp.
	if r := f.save(map[string]any{"provider": "fake", "refresh_minutes": 30, "forecast_days": 7}); r.status != 201 {
		t.Fatalf("đổi tần suất: %d %s", r.status, r.raw)
	}
	if r := f.run(); r.Due != 1 {
		t.Fatalf("tần suất mới phải có hiệu lực: %+v", r)
	}

	// EX-02, AC-05: lỗi 3 lần liên tiếp → đúng một sự kiện cho Admin; lần thứ 4 không phát thêm.
	f.prov.SetFail(domain.ErrCodeUnavailable)
	for i := 1; i <= 4; i++ {
		f.clk.Advance(30 * time.Minute)
		f.run()
		want := 0
		if i >= 3 {
			want = 1
		}
		if n := f.failingEvents(); n != want {
			t.Fatalf("sau lần lỗi %d: %d sự kiện", i, n)
		}
	}
	// FN-TT-01/AC-03: vẫn hiện số liệu gần nhất kèm giờ cập nhật.
	today := f.call(f.app, f.member(f.coopA, htxroles.Member), http.MethodGet, "/v1/weather/today", "", nil)
	src, _ := today.body["source"].(map[string]any)
	if today.status != 200 || today.body["status"] != "ok" || today.body["current"] == nil || src["stale"] != true || src["updated_at"] == nil {
		t.Fatalf("hôm nay khi API lỗi: %d %s", today.status, today.raw)
	}
	// Thành công mở lại chuỗi lỗi.
	f.prov.SetFail("")
	f.clk.Advance(30 * time.Minute)
	f.run()
	// EX-03: vượt hạn mức → 3 giờ/lần đến 00:00 hôm sau.
	f.prov.SetFail(domain.ErrCodeQuota)
	f.clk.Advance(30 * time.Minute)
	if r := f.run(); r.Failed != 1 {
		t.Fatalf("lỗi hạn mức: %+v", r)
	}
	f.clk.Advance(time.Hour)
	if r := f.run(); r.Due != 0 {
		t.Fatalf("đang giãn tần suất: %+v", r)
	}
	f.clk.Advance(2 * time.Hour)
	if r := f.run(); r.Due != 1 {
		t.Fatalf("hết 3 giờ phải lấy lại: %+v", r)
	}
	if n := f.failingEvents(); n != 1 {
		t.Fatalf("chuỗi lỗi mới chưa tới 3 lần: %d sự kiện", n)
	}
}

// FN-TT-01 (thẻ hôm nay) và FN-TT-02 (lịch tháng): phạm vi theo vai, âm lịch, buổi, nhãn ngày đã qua.
func TestWeather_FNTT01_FNTT02_HomNayVaLich(t *testing.T) {
	f := newFixture(t, time.Date(2026, 11, 22, 10, 0, 0, 0, domain.VN))
	if r := f.save(map[string]any{"provider": "fake", "refresh_minutes": 60, "forecast_days": 7}); r.status != 201 {
		t.Fatalf("cấu hình: %d %s", r.status, r.raw)
	}
	member := f.member(f.coopA, htxroles.Member)
	// EX-03: chưa có số liệu lần nào → ngày dương, ngày âm và câu báo.
	r := f.call(f.app, member, http.MethodGet, "/v1/weather/today", "", nil)
	lun, _ := r.body["lunar"].(map[string]any)
	if r.status != 200 || r.body["status"] != "no_data" || r.body["message"] != "Chưa có số liệu thời tiết" || r.body["date"] != "2026-11-22" || lun["label"] != "14/10" {
		t.Fatalf("chưa có số liệu: %d %s", r.status, r.raw)
	}
	f.run()                                      // 22/11 10:00 → lưu số liệu ngày 22/11 (trước 23:30)
	f.clk.Advance(13*time.Hour + 45*time.Minute) // 23:45 — lần lấy sau 23:30 không thay số liệu ngày 22/11
	f.prov.Hourless = true
	f.run()
	f.prov.Hourless = false
	f.clk.Advance(10*time.Hour + 15*time.Minute) // 23/11 10:00
	f.run()

	// AC-02 (FN-TT-01): 23/11/2026 là rằm tháng 10 ÂL; số liệu từ bộ đệm.
	r = f.call(f.app, member, http.MethodGet, "/v1/weather/today", "", nil)
	lun, _ = r.body["lunar"].(map[string]any)
	cur, _ := r.body["current"].(map[string]any)
	if r.status != 200 || r.body["status"] != "ok" || lun["label"] != "15/10" || cur["temperature_c"] == nil || cur["wind_direction"] == "" {
		t.Fatalf("hôm nay: %d %s", r.status, r.raw)
	}
	if strings.Contains(strings.ToLower(r.raw), "khuyên") || strings.Contains(strings.ToLower(r.raw), "cảnh báo") {
		t.Fatal("AC-04: không có lời khuyên hay cảnh báo")
	}
	// AC-05: HTX chưa có tọa độ.
	if r := f.call(f.app, f.member(f.coopD, htxroles.Representative), http.MethodGet, "/v1/weather/today", "", nil); r.body["status"] != "no_location" ||
		r.body["message"] != "Chưa có vị trí để xem thời tiết. Liên hệ Admin để cập nhật." {
		t.Fatalf("chưa có tọa độ: %s", r.raw)
	}
	// AC-06: Admin chọn HTX ở bộ lọc; không chọn → báo chọn HTX. Vai khác không xem HTX khác.
	if r := f.call(f.app, f.admin, http.MethodGet, "/v1/weather/today", "", nil); r.status != 422 {
		t.Fatalf("Admin không chọn HTX: %d", r.status)
	}
	if r := f.call(f.app, f.admin, http.MethodGet, path("/v1/weather/today", "cooperative_ref", f.coopC.String()), "", nil); r.status != 200 ||
		r.body["cooperative_ref"] != f.coopC.String() || r.body["status"] != "ok" {
		t.Fatalf("Admin chọn HTX C: %d %s", r.status, r.raw)
	}
	if r := f.call(f.app, member, http.MethodGet, path("/v1/weather/today", "cooperative_ref", f.coopC.String()), "", nil); r.status != 403 {
		t.Fatalf("Thành viên xem HTX khác: %d", r.status)
	}

	// FN-TT-02: lịch tháng 11/2026.
	cal := f.call(f.app, member, http.MethodGet, path("/v1/weather/calendar", "month", "2026-11"), "", nil)
	days, _ := cal.body["days"].([]any)
	if cal.status != 200 || len(days) != 30 || cal.body["forecast_days"] != 7.0 {
		t.Fatalf("lịch: %d %s", cal.status, cal.raw)
	}
	day := func(n int) map[string]any { return days[n-1].(map[string]any) }
	l9, l23 := day(9)["lunar"].(map[string]any), day(23)["lunar"].(map[string]any)
	if l9["label"] != "01/10" || l9["first_day"] != true || l23["full_moon"] != true {
		t.Fatalf("AC-02 âm lịch: %v %v", l9, l23)
	}
	// Ngày đã qua có số liệu lưu: nhãn "Dự báo cuối ngày"; bản lấy lúc 23:45 (không theo giờ) không thay bản 10:00.
	w22, _ := day(22)["weather"].(map[string]any)
	if w22 == nil || w22["kind"] != "final_forecast" || w22["label"] != "Dự báo cuối ngày" || w22["hourly_available"] != true {
		t.Fatalf("22/11: %v", w22)
	}
	// EX-02: ngày đã qua chưa có số liệu → chỉ ngày dương, ngày âm.
	if day(21)["weather"] != nil {
		t.Fatalf("21/11 không có số liệu: %v", day(21)["weather"])
	}
	// Hôm nay đến hết phạm vi dự báo (7 ngày: 23–29/11) có dự báo, 4 buổi; ngoài phạm vi không có (AC-03).
	w23, _ := day(23)["weather"].(map[string]any)
	periods, _ := w23["periods"].([]any)
	if w23["kind"] != "forecast" || len(periods) != 4 || periods[2].(map[string]any)["name"] != "Chiều" {
		t.Fatalf("23/11: %v", w23)
	}
	if day(29)["weather"] == nil || day(30)["weather"] != nil {
		t.Fatalf("phạm vi dự báo: 29/11 %v, 30/11 %v", day(29)["weather"], day(30)["weather"])
	}
	if r := f.call(f.app, member, http.MethodGet, path("/v1/weather/calendar", "month", "11-2026"), "", nil); r.status != 422 {
		t.Fatalf("tháng sai định dạng: %d", r.status)
	}

	// EX-01: nhà cung cấp không có số liệu theo giờ → chi tiết ngày chỉ một khối cả ngày.
	f.prov.Hourless = true
	f.clk.Advance(time.Hour)
	f.run()
	cal = f.call(f.app, member, http.MethodGet, path("/v1/weather/calendar", "month", "2026-11"), "", nil)
	days, _ = cal.body["days"].([]any)
	w24, _ := days[23].(map[string]any)["weather"].(map[string]any)
	if w24["hourly_available"] != false || len(w24["periods"].([]any)) != 0 || w24["temperature_max_c"] == nil {
		t.Fatalf("không theo giờ: %v", w24)
	}
}
