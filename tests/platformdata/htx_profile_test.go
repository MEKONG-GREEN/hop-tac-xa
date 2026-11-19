package platformdata_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/masterdata"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/party"
	pp "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/party/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/clock"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/db"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/httpx"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/vault/identity/client/clienttest"
	"github.com/MEKONG-GREEN/hop-tac-xa/migrations"
)

type retryProfileOwner struct {
	pp.CoopProfiles
	editor   pp.HTXProfileEditor
	failNext atomic.Bool
}

func (p *retryProfileOwner) ApplyHTXProfile(ctx context.Context, patch pp.HTXProfilePatch) error {
	if p.failNext.Swap(false) {
		return errors.New("injected owner outage")
	}
	return p.editor.ApplyHTXProfile(ctx, patch)
}

// Owner thật ở hai DB, mỗi pool chỉ 1 connection. Ép lỗi sau khi Vault đã
// sửa rồi retry, sửa lần mới, mất receipt và retry lần cũ: không được ghi đè.
func TestAH283HTXProfileOwnersAndRecovery(t *testing.T) {
	f := newFixture(t)
	env := pgtest.Get(t)
	one := func(cluster migrations.Cluster, role string) *pgxpool.Pool {
		t.Helper()
		config := env.Pool(t, cluster, role).Config()
		config.MaxConns, config.MinConns = 1, 0
		pool, err := pgxpool.NewWithConfig(context.Background(), config)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(pool.Close)
		return pool
	}
	pf := one(migrations.PF, dbadmin.AppPlatform)
	issuer := authztest.New(t)
	vault := clienttest.NewPersistentHTTP(t, one(migrations.Vault, "vault_app"), httpx.Authenticate(issuer.Verifier()))
	md := masterdata.New(pf, clock.System{})
	pty := party.New(pf, clock.System{}, party.Deps{AdminUnits: md.AdminUnits(), Vault: vault})
	coop := cooperative.New(pf, clock.System{}, cooperative.Deps{Orgs: pty.Orgs(), Dir: pty.OrgDirectory(), Scope: pty.ScopeResolver(), Consents: pty.ConsentStates()})
	profiles := coop.CoopProfiles()
	owner := &retryProfileOwner{CoopProfiles: profiles, editor: profiles.(pp.HTXProfileEditor)}
	pty.UseCoopProfiles(owner)
	pty.UseCooperativeAreas(coop.CooperativeAreas())
	adminToken := issuer.Sign(t, authztest.Token{Subject: ids.New(), TenantID: f.tid, MFA: true, Roles: []string{authz.RolePlatformAdmin}})
	log := telemetry.NewLogger("test", io.Discard)
	application, admin := httpx.NewEngine(log, 1<<20), httpx.NewEngine(log, 1<<20)
	errorsSeen := make(chan error, 16)
	captureErrors := func(c *gin.Context) {
		c.Next()
		for _, item := range c.Errors {
			select {
			case errorsSeen <- item.Err:
			default:
			}
		}
	}
	application.Use(captureErrors)
	admin.Use(captureErrors)
	ops := db.ForModule(pf, db.Ops)
	pty.RegisterApp(application.Group("/v1", httpx.Authenticate(issuer.Verifier()), idempotency.Middleware(ops)))
	pty.RegisterAdmin(admin.Group("/v1", httpx.Authenticate(issuer.Verifier()), httpx.RequireMFA(), idempotency.Middleware(ops)))
	appServer, adminServer := httptest.NewServer(application), httptest.NewServer(admin)
	t.Cleanup(appServer.Close)
	t.Cleanup(adminServer.Close)
	call := func(server, token, method, path, key string, input map[string]any) (int, map[string]any) {
		t.Helper()
		body, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, method, server+path, bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		if key != "" {
			req.Header.Set("Idempotency-Key", key)
		}
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = response.Body.Close() }()
		var result map[string]any
		if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
			t.Fatal(err)
		}
		return response.StatusCode, result
	}
	// Liên kết định danh người cũ không được dùng làm SĐT liên hệ HTX hoặc
	// buộc vai nghiệp vụ phải nhận thêm quyền Vault rộng.
	legacyPerson := ids.New()
	legacyContext, _ := gin.CreateTestContext(httptest.NewRecorder())
	legacyContext.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	legacyContext.Request.Header.Set("Authorization", "Bearer "+adminToken)
	httpx.Authenticate(issuer.Verifier())(legacyContext)
	if legacyContext.IsAborted() {
		t.Fatal("legacy setup authentication failed")
	}
	legacyRef, err := pty.CooperativeRegistrar().RegisterCooperative(legacyContext.Request.Context(), pp.CooperativeRegistration{
		Name: "HTX hồ sơ cũ", CommuneCode: "89011", Address: "Trụ sở hồ sơ cũ", RepresentativeRef: legacyPerson, IdemKey: "ah283-legacy-register"})
	if err != nil {
		t.Fatal(err)
	}
	legacyPath := "/v1/cooperatives/" + legacyRef.String() + "/profile"
	legacyRepresentative := issuer.Sign(t, authztest.Token{Subject: ids.New(), TenantID: f.tid, OrgRef: legacyRef, Roles: []string{pp.RoleCoopManager}})
	legacyStatus, legacyProfile := call(appServer.URL, legacyRepresentative, http.MethodGet, legacyPath, "", nil)
	if legacyStatus != http.StatusOK || legacyProfile["contact_configured"] != false || legacyProfile["can_edit"] != true || legacyProfile["phone"] != "" || legacyProfile["representative_ref"] != legacyPerson.String() {
		t.Fatalf("hồ sơ cũ không suy SĐT/không mất liên kết định danh cũ: %d %v", legacyStatus, legacyProfile)
	}
	legacyInput := map[string]any{"name": "HTX hồ sơ cũ", "commune_code": "89011", "head_office": "Trụ sở hồ sơ cũ", "phone": "0907248370", "representative_name": "Đại diện liên hệ HTX", "expected_revision": 1}
	if status, body := call(appServer.URL, legacyRepresentative, http.MethodPost, legacyPath+"/revisions", "ah283-legacy-own-edit", legacyInput); status != http.StatusOK {
		t.Fatalf("Đại diện bổ sung liên hệ hồ sơ cũ: %d %v", status, body)
	}
	legacyStatus, legacyProfile = call(appServer.URL, legacyRepresentative, http.MethodGet, legacyPath, "", nil)
	if legacyStatus != http.StatusOK || legacyProfile["contact_configured"] != true || legacyProfile["phone"] != legacyInput["phone"] || legacyProfile["representative_ref"] != nil {
		t.Fatalf("liên hệ mới không còn gắn nhầm định danh người cũ: %d %v", legacyStatus, legacyProfile)
	}
	input := map[string]any{"name": "HTX hồ sơ xuyên module", "commune_code": "89011", "head_office": "Trụ sở đầu", "phone": "0907248350", "representative_name": ""}
	status, result := call(adminServer.URL, adminToken, http.MethodPost, "/v1/cooperative-profiles", "ah283-create-real", input)
	if status != http.StatusCreated {
		select {
		case cause := <-errorsSeen:
			for cause != nil {
				t.Logf("owner cause: %v", cause)
				cause = errors.Unwrap(cause)
			}
		default:
		}
		t.Fatalf("tạo qua owner thật: %d %v", status, result)
	}
	org := uuid.MustParse(result["ref"].(string))
	profilePath := "/v1/cooperatives/" + org.String() + "/profile"
	input["expected_revision"] = 1
	input["name"], input["head_office"], input["representative_name"] = "HTX đã sửa lần một", "Trụ sở lần một", "Đại diện lần một"
	owner.failNext.Store(true)
	status, result = call(appServer.URL, adminToken, http.MethodPost, profilePath+"/revisions", "ah283-update-real-one", input)
	if status != http.StatusServiceUnavailable {
		t.Fatalf("lỗi owner phải cho retry: %d %v", status, result)
	}
	if status, result = call(appServer.URL, adminToken, http.MethodGet, profilePath, "", nil); status != http.StatusConflict || result["code"] != "party.cooperative_profile_pending" {
		t.Fatalf("không trả hồ sơ nửa chừng: %d %v", status, result)
	}
	if status, result = call(appServer.URL, adminToken, http.MethodPost, profilePath+"/revisions", "ah283-new-operation-too-early", input); status != http.StatusConflict || result["code"] != "party.cooperative_profile_pending" {
		t.Fatalf("chặn thao tác mới khi owner chưa xong: %d %v", status, result)
	}
	status, result = call(appServer.URL, adminToken, http.MethodPost, profilePath+"/revisions", "ah283-update-real-one", input)
	if status != http.StatusOK || result["revision"] != float64(2) {
		t.Fatalf("hoàn tất ý định trước: %d %v", status, result)
	}
	second := map[string]any{"name": "HTX mới nhất", "commune_code": "89011", "head_office": "Trụ sở mới nhất", "phone": "0907248350", "representative_name": "Đại diện mới nhất", "expected_revision": 2}
	status, result = call(appServer.URL, adminToken, http.MethodPost, profilePath+"/revisions", "ah283-update-real-two", second)
	if status != http.StatusOK || result["revision"] != float64(3) {
		t.Fatalf("sửa tiếp: %d %v", status, result)
	}
	// Chỉ xóa receipt của dữ liệu fixture để mô phỏng hết hạn/mất projection.
	f.exec(`DELETE FROM shared.idempotency_key WHERE tenant_id=$1 AND idem_key='ah283-update-real-one'`, f.tid)
	f.exec(`DELETE FROM pf_party.htx_profile_completion WHERE tenant_id=$1 AND command_ref IN
(SELECT id FROM pf_party.org_event WHERE tenant_id=$1 AND org_ref=$2 AND seq=2)`, f.tid, org)
	status, result = call(appServer.URL, adminToken, http.MethodPost, profilePath+"/revisions", "ah283-update-real-one", input)
	if status != http.StatusOK {
		t.Fatalf("khôi phục receipt từ owner bền vững: %d %v", status, result)
	}
	status, result = call(appServer.URL, adminToken, http.MethodGet, profilePath, "", nil)
	if status != http.StatusOK || result["name"] != second["name"] || result["address"] != second["head_office"] || result["representative_name"] != second["representative_name"] {
		t.Fatalf("retry cũ không được ghi đè hồ sơ mới: %d %v", status, result)
	}
	member := issuer.Sign(t, authztest.Token{Subject: ids.New(), TenantID: f.tid, OrgRef: org, Roles: []string{pp.RoleHTXMember}})
	status, result = call(appServer.URL, member, http.MethodGet, profilePath, "", nil)
	if status != http.StatusOK || result["contact_masked"] != true || result["phone"] == input["phone"] || strings.Contains(result["phone"].(string), "0907248350") {
		t.Fatalf("Member không được nhận SĐT gốc: %d %v", status, result)
	}
	if status, _ := call(appServer.URL, member, http.MethodPost, profilePath+"/revisions", "ah283-member-denied", second); status != http.StatusForbidden {
		t.Fatalf("Member không được sửa: %d", status)
	}
	representative := issuer.Sign(t, authztest.Token{Subject: ids.New(), TenantID: f.tid, OrgRef: org,
		Roles: []string{pp.RoleCoopManager}})
	second["expected_revision"], second["name"] = 3, "HTX do Đại diện sửa"
	if status, result = call(appServer.URL, representative, http.MethodPost, profilePath+"/revisions", "ah283-own-representative", second); status != http.StatusOK {
		t.Fatalf("Đại diện được sửa đúng HTX mình qua owner thật: %d %v", status, result)
	}
	foreignRepresentative := issuer.Sign(t, authztest.Token{Subject: ids.New(), TenantID: f.tid, OrgRef: ref(107), MFA: true,
		Roles: []string{pp.RoleCoopManager}})
	if status, _ := call(appServer.URL, foreignRepresentative, http.MethodPost, profilePath+"/revisions", "ah283-other-representative", second); status != http.StatusNotFound {
		t.Fatalf("Đại diện không sửa HTX khác: %d", status)
	}
}
