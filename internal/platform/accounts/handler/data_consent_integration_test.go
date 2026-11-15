package handler_test

// AH-23 / REQ-011 / KH-24: đồng ý thu thập ảnh GPS và dữ liệu sản xuất (bằng chứng pf_accounts.data_consent, bước chặn
// route nhận ảnh/GPS ở Middleware, tham số phiên bản của pf/masterdata).

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/keycloak"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/port"
	mdport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/clock"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/httpx"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/params"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
	"github.com/MEKONG-GREEN/hop-tac-xa/migrations"
)

const consentText = "Ứng dụng HTX thu thập ảnh hiện trường có gắn tọa độ GPS và dữ liệu sản xuất."

// consentParams là tham số masterdata giả: key → Text, sửa được giữa chừng; thiếu khóa → ErrParamNotFound.
type consentParams struct {
	mu sync.Mutex
	m  map[string]string
}

func (p *consentParams) At(_ context.Context, key string, _ time.Time) (params.ParamValue, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	v, ok := p.m[key]
	if !ok {
		return params.ParamValue{}, params.ErrParamNotFound
	}
	return params.ParamValue{Key: key, Text: v}, nil
}

func (p *consentParams) set(key, v string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.m[key] = v
}

func (p *consentParams) del(key string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.m, key)
}

type consentFix struct {
	*fixture
	srv    *httptest.Server
	params *consentParams
	now    time.Time
	calls  atomic.Int64 // số lần handler giả chạy
}

func newConsentFixture(t *testing.T) *consentFix {
	base := newFixture(t)
	now := time.Date(2026, 11, 20, 3, 4, 5, 0, time.UTC)
	f := &consentFix{fixture: base, now: now, params: &consentParams{m: map[string]string{
		mdport.ParamAccountsDataConsentVersion: "tam-1",
		mdport.DataConsentTextKey("tam-1"):     consentText,
	}}}
	mod := accounts.New(pgtest.Get(t).Pool(t, migrations.PF, dbadmin.AppPlatform), clock.NewFixed(now),
		accounts.Deps{Directory: keycloak.NewMemory(), Scope: base.party, Orgs: base.party, Params: f.params})
	ops := base.env.Pool(t, migrations.PF, "platform_api")
	eng := httpx.NewEngine(telemetry.NewLogger("test", io.Discard), 1<<20)
	ag := eng.Group("/v1", httpx.Authenticate(base.iss.Verifier()), mod.Middleware(), idempotency.Middleware(ops))
	mod.RegisterApp(ag)
	// Route nhận ảnh/GPS giả của module khác: đếm số lần chạy.
	fake := func(c *gin.Context) { f.calls.Add(1); c.JSON(http.StatusCreated, gin.H{"ok": true}) }
	for _, r := range consentGuardedRoutes(t) {
		method, path, _ := strings.Cut(r, " ")
		ag.Handle(method, strings.TrimPrefix(path, "/v1"), fake)
	}
	ag.GET("/files/:ref", func(c *gin.Context) { f.calls.Add(1); c.JSON(http.StatusOK, gin.H{"ok": true}) })
	f.srv = httptest.NewServer(eng)
	t.Cleanup(f.srv.Close)
	return f
}

// consentGuardedRoutes lấy tập route bị chặn từ domain (ghép cùng đường dẫn mẫu của gin).
func consentGuardedRoutes(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, r := range []string{
		"POST /v1/files", "POST /v1/sync/push",
		"POST /v1/cropcycle/seasons", "POST /v1/cropcycle/seasons/:ref/harvest",
		"POST /v1/cropcycle/farm-logs", "POST /v1/cropcycle/farm-logs/batch", "POST /v1/cropcycle/farm-logs/:id/corrections",
		"POST /v1/plots", "POST /v1/plots/:ref/boundaries",
	} {
		method, path, _ := strings.Cut(r, " ")
		if !domain.IsDataCollectionRoute(method, path) {
			t.Fatalf("route phải thuộc tập chặn: %s", r)
		}
		out = append(out, r)
	}
	return out
}

func (f *consentFix) call(token, method, path string, idem string, body any) resp {
	f.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, f.srv.URL+path, rd)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	if idem != "" {
		req.Header.Set("Idempotency-Key", idem)
	}
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		f.t.Fatal(err)
	}
	defer func() { _ = r.Body.Close() }()
	b, _ := io.ReadAll(r.Body)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return resp{status: r.StatusCode, hdr: r.Header, body: m}
}

func (f *consentFix) token(tid uuid.UUID, sub uuid.UUID, roles ...string) string {
	return f.iss.Sign(f.t, authztest.Token{Subject: sub, TenantID: tid, Roles: roles, OrgRef: f.coop30292, MFA: true})
}

func (f *consentFix) coop() (string, uuid.UUID) {
	sub := ids.New()
	return f.token(f.tenant, sub, "coop_manager"), sub
}

func (f *consentFix) consentRows(tid, user uuid.UUID) []string {
	f.t.Helper()
	var out []string
	err := tenant.WithTenantTx(context.Background(), f.pool, tid, func(tx pgx.Tx) error {
		rows, err := tx.Query(context.Background(),
			`SELECT user_ref::text || '|' || text_version || '|' || actor_ref::text || '|' || to_char(event_time AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS')
			   FROM pf_accounts.data_consent WHERE user_ref = $1 ORDER BY seq`, user)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var s string
			if err := rows.Scan(&s); err != nil {
				return err
			}
			out = append(out, s)
		}
		return rows.Err()
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return out
}

func (f *consentFix) accept(token, version string) resp {
	f.t.Helper()
	return f.call(token, http.MethodPost, "/v1/me/data-consent", key(), map[string]any{"version": version})
}

func routeURL(r string) (string, string) {
	method, path, _ := strings.Cut(r, " ")
	for _, p := range []string{":ref", ":id"} {
		path = strings.ReplaceAll(path, p, ids.New().String())
	}
	return method, path
}

func TestDataConsent_REQ011_AC1_NewCoopAccountMustConsent(t *testing.T) {
	f := newConsentFixture(t)
	tok, _ := f.coop()
	r := f.call(tok, http.MethodGet, "/v1/me/data-consent", "", nil)
	if r.status != http.StatusOK || r.body["required"] != true || str(r.body, "version") != "tam-1" || str(r.body, "text") != consentText {
		t.Fatalf("GET: %d %v", r.status, r.body)
	}
	if r.body["consented_at"] != nil {
		t.Fatalf("chưa đồng ý thì consented_at rỗng: %v", r.body)
	}
}

func TestDataConsent_REQ011_AC2_EvidenceAppendOnly(t *testing.T) {
	f := newConsentFixture(t)
	tok, sub := f.coop()
	r := f.accept(tok, "tam-1")
	if r.status != http.StatusCreated || str(r.body, "version") != "tam-1" || str(r.body, "consented_at") != "2026-11-20T03:04:05Z" {
		t.Fatalf("POST: %d %v", r.status, r.body)
	}
	want := sub.String() + "|tam-1|" + sub.String() + "|2026-11-20T03:04:05"
	if got := f.consentRows(f.tenant, sub); !slices.Equal(got, []string{want}) {
		t.Fatalf("sổ: %v, muốn %v", got, want)
	}
	// Sổ chỉ ghi thêm: vai accounts không sửa, không xóa được.
	for _, q := range []string{`UPDATE pf_accounts.data_consent SET text_version = 'x'`, `DELETE FROM pf_accounts.data_consent`} {
		err := tenant.WithTenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
			_, e := tx.Exec(context.Background(), q)
			return e
		})
		if err == nil {
			t.Fatalf("%s phải bị từ chối", q)
		}
	}
	if got := f.consentRows(f.tenant, sub); len(got) != 1 {
		t.Fatalf("bằng chứng phải còn nguyên: %v", got)
	}
	// Không có cột họ tên, số điện thoại (quy tắc 7).
	var cols []string
	err := tenant.WithTenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(context.Background(), `SELECT column_name FROM information_schema.columns
		  WHERE table_schema = 'pf_accounts' AND table_name = 'data_consent'`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var c string
			if err := rows.Scan(&c); err != nil {
				return err
			}
			cols = append(cols, c)
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cols {
		for _, bad := range []string{"name", "phone", "email"} {
			if strings.Contains(c, bad) {
				t.Fatalf("cột %q chứa PII", c)
			}
		}
	}
	// Thiếu khóa idempotency.
	if r := f.call(tok, http.MethodPost, "/v1/me/data-consent", "", map[string]any{"version": "tam-1"}); r.status != http.StatusBadRequest {
		t.Fatalf("thiếu Idempotency-Key: %d %v", r.status, r.body)
	}
	// Version sai dạng.
	if r := f.accept(tok, "sai dạng!"); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("version sai dạng: %d %v", r.status, r.body)
	}
}

func TestDataConsent_REQ011_AC3_GuardRejectsPhotoGPS(t *testing.T) {
	f := newConsentFixture(t)
	tok, _ := f.coop()
	for _, route := range consentGuardedRoutes(t) {
		method, path := routeURL(route)
		r := f.call(tok, method, path, key(), map[string]any{"gps": map[string]any{"lat": 10.1, "lng": 105.2}})
		if r.status != http.StatusForbidden || str(r.body, "code") != "accounts.data_consent_required" {
			t.Fatalf("%s: %d %v", route, r.status, r.body)
		}
	}
	if n := f.calls.Load(); n != 0 {
		t.Fatalf("handler nhận ảnh/GPS không được chạy khi chưa đồng ý: %d lần", n)
	}
	// Đọc ảnh không bị chặn.
	if r := f.call(tok, http.MethodGet, "/v1/files/"+ids.New().String(), "", nil); r.status != http.StatusOK {
		t.Fatalf("GET file: %d %v", r.status, r.body)
	}
	if r := f.accept(tok, "tam-1"); r.status != http.StatusCreated {
		t.Fatalf("đồng ý: %d %v", r.status, r.body)
	}
	for _, route := range consentGuardedRoutes(t) {
		method, path := routeURL(route)
		if r := f.call(tok, method, path, key(), map[string]any{}); r.status != http.StatusCreated {
			t.Fatalf("%s sau khi đồng ý: %d %v", route, r.status, r.body)
		}
	}
}

func TestDataConsent_ACN1_NoPromptAfterConsent(t *testing.T) {
	f := newConsentFixture(t)
	tok, sub := f.coop()
	if r := f.accept(tok, "tam-1"); r.status != http.StatusCreated {
		t.Fatalf("đồng ý: %d %v", r.status, r.body)
	}
	r := f.call(tok, http.MethodGet, "/v1/me/data-consent", "", nil)
	if r.body["required"] != false || str(r.body, "consented_at") != "2026-11-20T03:04:05Z" {
		t.Fatalf("GET sau đồng ý: %v", r.body)
	}
	// Đồng ý lại bằng khóa mới: 200 với bản cũ, không ghi thêm dòng.
	r2 := f.accept(tok, "tam-1")
	if r2.status != http.StatusOK || str(r2.body, "version") != "tam-1" {
		t.Fatalf("POST lại: %d %v", r2.status, r2.body)
	}
	if got := f.consentRows(f.tenant, sub); len(got) != 1 {
		t.Fatalf("chỉ một dòng: %v", got)
	}
}

func TestDataConsent_ACN2_VersionChangeRepromptsKeepsEvidence(t *testing.T) {
	f := newConsentFixture(t)
	tok, sub := f.coop()
	if r := f.accept(tok, "tam-1"); r.status != http.StatusCreated {
		t.Fatalf("đồng ý: %d %v", r.status, r.body)
	}
	f.params.set(mdport.DataConsentTextKey("tam-2"), "Nội dung mới")
	f.params.set(mdport.ParamAccountsDataConsentVersion, "tam-2")
	r := f.call(tok, http.MethodGet, "/v1/me/data-consent", "", nil)
	if r.body["required"] != true || str(r.body, "version") != "tam-2" || str(r.body, "text") != "Nội dung mới" {
		t.Fatalf("đổi phiên bản phải hỏi lại: %v", r.body)
	}
	if r := f.call(tok, http.MethodPost, "/v1/plots", key(), map[string]any{}); r.status != http.StatusForbidden {
		t.Fatalf("guard phải chặn lại: %d %v", r.status, r.body)
	}
	// Bản cũ vẫn là bản cũ: không đồng ý được phiên bản không còn hiện hành.
	if r := f.accept(tok, "tam-1"); r.status != http.StatusConflict || str(r.body, "code") != "accounts.data_consent_version_stale" {
		t.Fatalf("version cũ: %d %v", r.status, r.body)
	}
	if r := f.accept(tok, "tam-2"); r.status != http.StatusCreated {
		t.Fatalf("đồng ý bản mới: %d %v", r.status, r.body)
	}
	got := f.consentRows(f.tenant, sub)
	if len(got) != 2 || !strings.Contains(got[0], "|tam-1|") || !strings.Contains(got[1], "|tam-2|") {
		t.Fatalf("bằng chứng cũ phải còn nguyên: %v", got)
	}
}

func TestDataConsent_ACN4_AdminNotAsked(t *testing.T) {
	f := newConsentFixture(t)
	adm := f.token(f.tenant, ids.New(), port.RoleAccountAdmin)
	r := f.call(adm, http.MethodGet, "/v1/me/data-consent", "", nil)
	if r.status != http.StatusOK || r.body["required"] != false || r.body["text"] != nil {
		t.Fatalf("Admin GET: %d %v", r.status, r.body)
	}
	if r := f.accept(adm, "tam-1"); r.status != http.StatusForbidden {
		t.Fatalf("Admin POST: %d %v", r.status, r.body)
	}
	if r := f.call(adm, http.MethodPost, "/v1/plots", key(), map[string]any{}); r.status != http.StatusCreated {
		t.Fatalf("Admin đi qua route ảnh/GPS: %d %v", r.status, r.body)
	}
	// Tham số trống: Admin vẫn không bị ảnh hưởng.
	f.params.del(mdport.ParamAccountsDataConsentVersion)
	if r := f.call(adm, http.MethodGet, "/v1/me/data-consent", "", nil); r.status != http.StatusOK || r.body["required"] != false {
		t.Fatalf("Admin khi thiếu tham số: %d %v", r.status, r.body)
	}
	if r := f.call(adm, http.MethodPost, "/v1/plots", key(), map[string]any{}); r.status != http.StatusCreated {
		t.Fatalf("Admin khi thiếu tham số: %d %v", r.status, r.body)
	}
}

func TestDataConsent_Unconfigured_FailClosed(t *testing.T) {
	f := newConsentFixture(t)
	tok, _ := f.coop()
	check := func(label string) {
		t.Helper()
		if r := f.call(tok, http.MethodGet, "/v1/me/data-consent", "", nil); r.status != http.StatusServiceUnavailable ||
			str(r.body, "code") != "accounts.data_consent_unconfigured" {
			t.Fatalf("%s GET: %d %v", label, r.status, r.body)
		}
		if r := f.call(tok, http.MethodPost, "/v1/files", key(), map[string]any{}); r.status != http.StatusServiceUnavailable ||
			str(r.body, "code") != "accounts.data_consent_unconfigured" {
			t.Fatalf("%s guard: %d %v", label, r.status, r.body)
		}
		if r := f.accept(tok, "tam-1"); r.status != http.StatusServiceUnavailable {
			t.Fatalf("%s POST: %d %v", label, r.status, r.body)
		}
	}
	f.params.del(mdport.ParamAccountsDataConsentVersion)
	check("thiếu version")
	f.params.set(mdport.ParamAccountsDataConsentVersion, "tam-1")
	f.params.del(mdport.DataConsentTextKey("tam-1"))
	check("thiếu nội dung")
	f.params.set(mdport.DataConsentTextKey("tam-1"), "   ")
	check("nội dung rỗng")
	f.params.set(mdport.DataConsentTextKey("tam-1"), consentText)
	f.params.set(mdport.ParamAccountsDataConsentVersion, "sai dạng!")
	check("mã sai dạng")
	if n := f.calls.Load(); n != 0 {
		t.Fatalf("handler không được chạy: %d", n)
	}
}

func TestDataConsent_TenantIsolation(t *testing.T) {
	f := newConsentFixture(t)
	sub := ids.New()
	a := f.token(f.tenant, sub, "coop_manager")
	otherTenant := ids.New()
	b := f.token(otherTenant, sub, "coop_manager")
	if r := f.accept(a, "tam-1"); r.status != http.StatusCreated {
		t.Fatalf("đồng ý ở tenant A: %d %v", r.status, r.body)
	}
	if r := f.call(b, http.MethodGet, "/v1/me/data-consent", "", nil); r.body["required"] != true {
		t.Fatalf("tenant B vẫn phải hỏi: %v", r.body)
	}
	if r := f.call(b, http.MethodPost, "/v1/files", key(), map[string]any{}); r.status != http.StatusForbidden {
		t.Fatalf("tenant B vẫn bị chặn: %d %v", r.status, r.body)
	}
}

func TestDataConsent_IdempotentReplay(t *testing.T) {
	f := newConsentFixture(t)
	tok, sub := f.coop()
	k := key()
	r1 := f.call(tok, http.MethodPost, "/v1/me/data-consent", k, map[string]any{"version": "tam-1"})
	r2 := f.call(tok, http.MethodPost, "/v1/me/data-consent", k, map[string]any{"version": "tam-1"})
	if r1.status != http.StatusCreated || r2.status != r1.status || str(r1.body, "id") == "" || str(r1.body, "id") != str(r2.body, "id") {
		t.Fatalf("phát lại: %d %v / %d %v", r1.status, r1.body, r2.status, r2.body)
	}
	if got := f.consentRows(f.tenant, sub); len(got) != 1 {
		t.Fatalf("một dòng: %v", got)
	}
	// Khóa đã dùng cho người khác → từ chối.
	other, _ := f.coop()
	if r := f.call(other, http.MethodPost, "/v1/me/data-consent", k, map[string]any{"version": "tam-1"}); r.status < 400 {
		t.Fatalf("khóa của người khác phải bị từ chối: %d %v", r.status, r.body)
	}
}
