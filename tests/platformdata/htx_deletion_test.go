package platformdata_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts"
	ap "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/port"
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
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/vault/identity/client"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/vault/identity/client/clienttest"
	"github.com/MEKONG-GREEN/hop-tac-xa/migrations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type heldIAM struct {
	ap.Directory
	hold            atomic.Bool
	entered, resume chan struct{}
}

func (d *heldIAM) CreateUser(ctx context.Context, in ap.NewUser) (uuid.UUID, error) {
	if d.hold.CompareAndSwap(true, false) {
		close(d.entered)
		select {
		case <-d.resume:
		case <-ctx.Done():
			return uuid.Nil, ctx.Err()
		}
	}
	return d.Directory.CreateUser(ctx, in)
}

type deletionResponse struct {
	status int
	body   map[string]any
}

type deletionVault struct {
	client.Client
	client.HTXContacts
	client.HTXContactEraser
	fail atomic.Bool
}

func (v *deletionVault) EraseHTXContact(ctx context.Context, purpose client.Purpose, ref, command uuid.UUID) error {
	if v.fail.Load() {
		return errors.New("injected erasure outage after PF commit")
	}
	return v.HTXContactEraser.EraseHTXContact(ctx, purpose, ref, command)
}

type deletionAPI struct {
	f                  *fixture
	party              *party.Module
	directory          *accounts.MemoryDirectory
	iam                *heldIAM
	vault              *deletionVault
	issuer             *authztest.FakeIssuer
	admin, application *httptest.Server
	token              string
	t                  *testing.T
}

func newDeletionAPI(t *testing.T) *deletionAPI {
	t.Helper()
	f := newFixture(t)
	env := pgtest.Get(t)
	one := func(cluster migrations.Cluster, role string) *pgxpool.Pool {
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
	vaultClient := clienttest.NewPersistentHTTP(t, one(migrations.Vault, "vault_app"), httpx.Authenticate(issuer.Verifier()))
	contacts, ok := client.AsHTXContacts(vaultClient)
	if !ok {
		t.Fatal("persistent Vault lacks contacts")
	}
	eraser, ok := client.AsHTXContactEraser(vaultClient)
	if !ok {
		t.Fatal("persistent Vault lacks erasure")
	}
	vault := &deletionVault{Client: vaultClient, HTXContacts: contacts, HTXContactEraser: eraser}
	md := masterdata.New(pf, clock.System{})
	pty := party.New(pf, clock.System{}, party.Deps{AdminUnits: md.AdminUnits(), Vault: vault})
	coop := cooperative.New(pf, clock.System{}, cooperative.Deps{Orgs: pty.Orgs(), Dir: pty.OrgDirectory(), Scope: pty.ScopeResolver(), Consents: pty.ConsentStates()})
	pty.UseCoopProfiles(coop.CoopProfiles())
	pty.UseCooperativeAreas(coop.CooperativeAreas())
	dir := accounts.NewMemoryDirectory()
	iam := &heldIAM{Directory: dir, entered: make(chan struct{}), resume: make(chan struct{})}
	acc := accounts.New(pf, clock.System{}, accounts.Deps{Directory: iam, Orgs: pty.Orgs(), Scope: pty.ScopeResolver(), Roles: ap.NativeRoleCatalog()})
	pty.UseCooperativeAccountUsers(acc.CooperativeAccountUsers())
	log := telemetry.NewLogger("test", io.Discard)
	appEngine, adminEngine := httpx.NewEngine(log, 1<<20), httpx.NewEngine(log, 1<<20)
	ops := db.ForModule(pf, db.Ops)
	pty.RegisterApp(appEngine.Group("/v1", httpx.Authenticate(issuer.Verifier()), idempotency.Middleware(ops)))
	adminGroup := adminEngine.Group("/v1", httpx.Authenticate(issuer.Verifier()), httpx.RequireMFA(), idempotency.Middleware(ops))
	pty.RegisterAdmin(adminGroup)
	acc.RegisterAdmin(adminGroup)
	a := &deletionAPI{f: f, party: pty, directory: dir, iam: iam, vault: vault, issuer: issuer, t: t, admin: httptest.NewServer(adminEngine), application: httptest.NewServer(appEngine)}
	t.Cleanup(a.admin.Close)
	t.Cleanup(a.application.Close)
	a.token = issuer.Sign(t, authztest.Token{Subject: ids.New(), TenantID: f.tid, MFA: true, Roles: []string{authz.RolePlatformAdmin}})
	return a
}

func (a *deletionAPI) call(server, token, method, path, key string, input map[string]any) deletionResponse {
	a.t.Helper()
	body, err := json.Marshal(input)
	if err != nil {
		a.t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, server+path, bytes.NewReader(body))
	if err != nil {
		a.t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set(idempotency.Header, key)
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		return deletionResponse{body: map[string]any{"error": err.Error()}}
	}
	defer func() { _ = response.Body.Close() }()
	out := deletionResponse{status: response.StatusCode}
	if err := json.NewDecoder(response.Body).Decode(&out.body); err != nil {
		out.body = map[string]any{"error": err.Error()}
	}
	return out
}
func (a *deletionAPI) create(name, key string) (uuid.UUID, map[string]any) {
	a.t.Helper()
	input := map[string]any{"name": name, "commune_code": "89011", "head_office": "Trụ sở thử nghiệm", "phone": "0907248380", "representative_name": ""}
	r := a.call(a.admin.URL, a.token, http.MethodPost, "/v1/cooperative-profiles", key, input)
	if r.status != http.StatusCreated {
		a.t.Fatalf("create: %d %v", r.status, r.body)
	}
	return uuid.MustParse(r.body["ref"].(string)), input
}
func (a *deletionAPI) preview(org uuid.UUID) deletionResponse {
	return a.call(a.admin.URL, a.token, http.MethodGet, "/v1/cooperatives/"+org.String()+"/deletion", "", nil)
}
func (a *deletionAPI) remove(org uuid.UUID, key string, revision int) deletionResponse {
	return a.call(a.admin.URL, a.token, http.MethodPost, "/v1/cooperatives/"+org.String()+"/deletions", key, map[string]any{"expected_revision": revision})
}

func TestAH283DeleteUnusedCooperativeNoResurrection(t *testing.T) {
	a := newDeletionAPI(t)
	org, input := a.create("HTX tạo nhầm chưa dùng", "ah283-delete-created")
	if r := a.preview(org); r.status != http.StatusOK || r.body["can_delete"] != true {
		t.Fatalf("preview unused: %d %v", r.status, r.body)
	}
	for _, role := range []string{pp.RoleCoopManager, pp.RoleHTXMember} {
		token := a.issuer.Sign(t, authztest.Token{Subject: ids.New(), TenantID: a.f.tid, OrgRef: org, MFA: true, Roles: []string{role}})
		r := a.call(a.admin.URL, token, http.MethodPost, "/v1/cooperatives/"+org.String()+"/deletions", ids.New().String(), map[string]any{"expected_revision": 1})
		if r.status != http.StatusForbidden {
			t.Fatalf("%s delete: %d %v", role, r.status, r.body)
		}
	}
	if r := a.remove(org, "ah283-delete-unused", 1); r.status != http.StatusOK || r.body["deleted"] != true {
		t.Fatalf("delete unused: %d %v", r.status, r.body)
	}
	nodes, err := a.party.KRefOrgs().OrgNodes(context.Background(), a.f.tid)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, node := range nodes {
		if node.Ref == org {
			found = true
			if node.Active || node.Seq != 2 {
				t.Fatalf("rebuild tombstone: %+v", node)
			}
		}
	}
	if !found {
		t.Fatal("rebuild omitted deleted cooperative sequence barrier")
	}
	var orgs, profiles, deleted int
	var projectedActive bool
	if err := a.f.admin.QueryRow(context.Background(), `SELECT
(SELECT count(*) FROM pf_party.org WHERE tenant_id=$1 AND ref=$2),
(SELECT count(*) FROM pf_coop.cooperative WHERE tenant_id=$1 AND org_ref=$2),
(SELECT count(*) FROM pf_party.org_event WHERE tenant_id=$1 AND org_ref=$2 AND action='deleted'),
(SELECT active FROM k_ref.org WHERE tenant_id=$1 AND ref=$2)`, a.f.tid, org).Scan(&orgs, &profiles, &deleted, &projectedActive); err != nil {
		t.Fatal(err)
	}
	if orgs != 0 || profiles != 0 || deleted != 1 || projectedActive {
		t.Fatalf("projection/audit: org=%d profile=%d deleted=%d active=%v", orgs, profiles, deleted, projectedActive)
	}
	a.f.exec(`DELETE FROM shared.idempotency_key WHERE tenant_id=$1`, a.f.tid)
	if r := a.remove(org, "ah283-delete-unused", 1); r.status != http.StatusOK {
		t.Fatalf("durable delete replay: %d %v", r.status, r.body)
	}
	r := a.call(a.admin.URL, a.token, http.MethodPost, "/v1/cooperative-profiles", "ah283-delete-created", input)
	if r.status != http.StatusNotFound {
		t.Fatalf("old create must not resurrect: %d %v", r.status, r.body)
	}
	if r := a.call(a.application.URL, a.token, http.MethodGet, "/v1/cooperatives/"+org.String()+"/profile", "", nil); r.status != http.StatusNotFound {
		t.Fatalf("deleted profile: %d %v", r.status, r.body)
	}
}

func TestAH283DeleteCountsInactiveHouseholdsAndHistoricalAccounts(t *testing.T) {
	a := newDeletionAPI(t)
	org, _ := a.create("HTX ba hộ hai tài khoản", "ah283-delete-used")
	other, _ := a.create("HTX nhận chuyển tài khoản", "ah283-other-accounts")
	for i := range 3 {
		household := ids.New()
		a.f.exec(`INSERT INTO pf_coop.household(tenant_id,household_ref,commune_code,cultivated_area_ha,active,created_by) VALUES($1,$2,'89011',1.5,$3,$4)`, a.f.tid, household, i != 1, ids.New())
		var until any
		if i == 2 {
			until = "2026-03-01"
		}
		a.f.exec(`INSERT INTO pf_coop.household_membership(tenant_id,id,household_ref,org_ref,since_date,until_date) VALUES($1,$2,$3,$4,'2026-01-01',$5::date)`, a.f.tid, ids.New(), household, org, until)
	}
	a.directory.Seed(ap.NewUser{Username: "ah283-locked", TenantID: a.f.tid, OrgRef: org}, false, pp.RoleCoopManager)
	created := a.call(a.admin.URL, a.token, http.MethodPost, "/v1/accounts/users", "ah283-history-account", map[string]any{"email": "ah283-history@example.test", "full_name": "Tài Khoản Thử", "org_ref": org.String(), "roles": []string{pp.RoleCoopManager}})
	if created.status != http.StatusCreated {
		t.Fatalf("create account: %d %v", created.status, created.body)
	}
	user := created.body["user"].(map[string]any)["id"].(string)
	moved := a.call(a.admin.URL, a.token, http.MethodPost, "/v1/accounts/users/"+user+"/assignment", "ah283-move-account", map[string]any{"org_ref": other.String()})
	if moved.status != http.StatusOK {
		t.Fatalf("move account: %d %v", moved.status, moved.body)
	}
	check := a.preview(org)
	if check.status != http.StatusOK || fmt.Sprint(check.body["households"]) != "3" || fmt.Sprint(check.body["accounts"]) != "2" || check.body["can_delete"] != false {
		t.Fatalf("all history counted: %d %v", check.status, check.body)
	}
	if r := a.remove(org, "ah283-used-denied", 1); r.status != http.StatusConflict || !strings.Contains(fmt.Sprint(r.body["detail"]), "3 hộ, 2 tài khoản") {
		t.Fatalf("used deletion: %d %v", r.status, r.body)
	}
}

func TestAH283DeleteFencesAccountProvisioningBeforeIAM(t *testing.T) {
	a := newDeletionAPI(t)
	org, _ := a.create("HTX tài khoản đang tạo", "ah283-provisioning")
	release := sync.OnceFunc(func() { close(a.iam.resume) })
	defer release()
	a.iam.hold.Store(true)
	result := make(chan deletionResponse, 1)
	go func() {
		result <- a.call(a.admin.URL, a.token, http.MethodPost, "/v1/accounts/users", "ah283-held-iam", map[string]any{"email": "ah283-held@example.test", "full_name": "Tài Khoản Chờ", "org_ref": org.String(), "roles": []string{pp.RoleCoopManager}})
	}()
	select {
	case <-a.iam.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("IAM was not reached")
	}
	if check := a.preview(org); check.status != http.StatusOK || fmt.Sprint(check.body["accounts"]) != "0" || fmt.Sprint(check.body["pending_accounts"]) != "1" || check.body["can_delete"] != false {
		t.Fatalf("reservation before IAM: %d %v", check.status, check.body)
	}
	if r := a.remove(org, "ah283-held-delete", 1); r.status != http.StatusConflict {
		t.Fatalf("delete during IAM: %d %v", r.status, r.body)
	}
	release()
	if r := <-result; r.status != http.StatusCreated {
		t.Fatalf("account completes: %d %v", r.status, r.body)
	}
	if check := a.preview(org); check.status != http.StatusOK || fmt.Sprint(check.body["accounts"]) != "1" || fmt.Sprint(check.body["pending_accounts"]) != "0" || check.body["can_delete"] != false {
		t.Fatalf("account replaces reservation atomically: %d %v", check.status, check.body)
	}
}

func TestAH283DeleteChecksOtherOwnerBusinessReferences(t *testing.T) {
	a := newDeletionAPI(t)
	org, _ := a.create("HTX có dữ liệu KN liên kết", "ah283-kn-linked")
	producer := ids.New()
	a.f.exec(`INSERT INTO pf_extension.kn_producer(id,tenant_id,stream_key,producer_ref,kind,event,commune_code,name,person_ref,htx_ref,event_time,actor_ref,idem_key,rule_version)
VALUES($1,$2,'kn_producer:'||$3::text,$3::uuid,'cooperative','created','89011','Hồ sơ KN liên kết',$4,$5,now(),$6,$7,'kn.producer/v1')`, ids.New(), a.f.tid, producer, ids.New(), org, ids.New(), ids.New().String())
	if check := a.preview(org); check.status != http.StatusOK || check.body["has_business"] != true || check.body["can_delete"] != false {
		t.Fatalf("business source: %d %v", check.status, check.body)
	}
	if r := a.remove(org, "ah283-business-denied", 1); r.status != http.StatusConflict {
		t.Fatalf("business deletion denied: %d %v", r.status, r.body)
	}
}

func TestAH283DeleteResumesErasureAfterPFCommit(t *testing.T) {
	a := newDeletionAPI(t)
	org, _ := a.create("HTX xóa gặp gián đoạn Vault", "ah283-erasure-retry-create")
	a.vault.fail.Store(true)
	if r := a.remove(org, "ah283-erasure-retry", 1); r.status != http.StatusServiceUnavailable {
		t.Fatalf("outage: %d %v", r.status, r.body)
	}
	var live, deleted int
	if err := a.f.admin.QueryRow(context.Background(), `SELECT
 (SELECT count(*) FROM pf_party.org WHERE tenant_id=$1 AND ref=$2),
 (SELECT count(*) FROM pf_party.org_event WHERE tenant_id=$1 AND org_ref=$2 AND action='deleted')`, a.f.tid, org).Scan(&live, &deleted); err != nil {
		t.Fatal(err)
	}
	if live != 0 || deleted != 1 {
		t.Fatalf("PF commit: live=%d deleted=%d", live, deleted)
	}
	a.vault.fail.Store(false)
	if r := a.remove(org, "ah283-erasure-retry", 1); r.status != http.StatusOK {
		t.Fatalf("resume without live org: %d %v", r.status, r.body)
	}
	var pending int
	if err := a.f.admin.QueryRow(context.Background(), `SELECT count(*) FROM pf_party.org_event e LEFT JOIN pf_party.htx_deletion_completion c ON c.tenant_id=e.tenant_id AND c.command_ref=e.id WHERE e.tenant_id=$1 AND e.org_ref=$2 AND e.action='deleted' AND c.command_ref IS NULL`, a.f.tid, org).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if pending != 0 {
		t.Fatal("erasure completion missing")
	}
}

func TestAH283DeleteDatabaseGuardsCannotBeBypassed(t *testing.T) {
	a := newDeletionAPI(t)
	org, _ := a.create("HTX kiểm tra chốt DB", "ah283-db-fence")
	ctx := authz.WithPrincipal(context.Background(), authz.Principal{TenantID: a.f.tid, ActorRef: ids.New(), Roles: []string{authz.RolePlatformAdmin}, MFA: true})
	partyDB := db.ForModule(pgtest.Get(t).Pool(t, migrations.PF, dbadmin.AppPlatform), db.Module("pf/party"))
	for _, purpose := range []string{"", "registry.cooperative_delete"} {
		callCtx := authz.WithPurpose(ctx, purpose)
		err := tenant.WithTenantTx(callCtx, partyDB, a.f.tid, func(tx pgx.Tx) error {
			_, err := tx.Exec(callCtx, `DELETE FROM pf_party.org WHERE tenant_id=$1 AND ref=$2`, a.f.tid, org)
			return err
		})
		var pg *pgconn.PgError
		if !errors.As(err, &pg) || (pg.Code != "42501" && pg.Code != "23514") {
			t.Fatalf("delete without owner event: %v", err)
		}
	}
	if r := a.remove(org, "ah283-db-authorized-delete", 1); r.status != http.StatusOK {
		t.Fatalf("authorized delete: %d %v", r.status, r.body)
	}
	_, err := a.f.admin.Exec(context.Background(), `INSERT INTO pf_extension.kn_producer(id,tenant_id,stream_key,producer_ref,kind,event,commune_code,name,person_ref,htx_ref,event_time,actor_ref,idem_key,rule_version)
 VALUES($1,$2,'kn_producer:'||$3::text,$3::uuid,'cooperative','created','89011','Liên kết đến HTX đã xóa',$4,$5,now(),$6,$7,'kn.producer/v1')`, ids.New(), a.f.tid, ids.New(), ids.New(), org, ids.New(), ids.New().String())
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.ConstraintName != "cooperative_permanently_deleted" {
		t.Fatalf("late producer must reject deleted target: %v", err)
	}
}
