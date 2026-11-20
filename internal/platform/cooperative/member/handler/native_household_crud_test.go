package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/port/farmerid"
	seasonapp "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/app"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/db"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/httpx"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/vault/identity/client"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/vault/identity/client/clienttest"
	"github.com/MEKONG-GREEN/hop-tac-xa/migrations"
	"github.com/google/uuid"
)

func nativeVaultFixture(t *testing.T) *fixture {
	t.Helper()
	f := newFixture(t)
	f.svc.DB = db.ForModule(f.env.SizedPool(t, migrations.PF, dbadmin.AppPlatform, 1), db.Module("pf/cooperative"))
	scopes, err := client.NewHouseholdScopeHTTP(f.app.URL, f.app.Client())
	if err != nil {
		t.Fatal(err)
	}
	v := clienttest.NewPersistentHTTPWithHouseholdScopes(t, f.env.SizedPool(t, migrations.Vault, "vault_app", 1), httpx.Authenticate(f.iss.Verifier()), scopes)
	f.svc.Vault = farmerid.Households(v)
	f.svc.ProductionHistory = seasonapp.HouseholdHistory{DB: f.svc.DB}
	return f
}
func nativeBody(coop uuid.UUID, phone, nid, area string) map[string]any {
	return map[string]any{"cooperative_ref": coop.String(), "cultivated_area_ha": area, "livestock_area_ha": "0.5", "active": true,
		"identity": map[string]any{"full_name": "Chủ hộ AH285", "phone": phone, "national_id": nid, "address": "Ấp thử nghiệm"}}
}
func nativePost(c caller, path, key string, body any) resp {
	raw, _ := json.Marshal(body)
	return c.call(c.f.app.URL, http.MethodPost, path, key, "application/json", raw)
}

func TestAH285NativeCRUDAndReplayWithRealVault(t *testing.T) {
	f := nativeVaultFixture(t)
	tree := f.seedTree(f.root())
	admin := f.as(f.tenant, uuid.Nil, htxroles.Admin)
	rep := f.as(f.tenant, tree.c1, htxroles.Representative)
	body := nativeBody(tree.c2, "0907285031", "079123456731", "2")
	created := nativePost(admin, "/v1/household-profiles", "create-admin", body)
	if created.status != 201 {
		t.Fatalf("native Admin create: %d %v", created.status, created.body)
	}
	ref := created.body["household_ref"].(string)
	path := "/v1/households/" + ref + "/profile"
	initial := admin.appDo(http.MethodGet, path, nil)
	code := initial.body["code"]
	if initial.status != 200 || code == nil || initial.body["revision"] != float64(1) {
		t.Fatalf("generated metadata: %d %v", initial.status, initial.body)
	}
	for _, tc := range []struct {
		who    caller
		status int
	}{{f.as(f.tenant, tree.c2, htxroles.Member), 403}, {rep, 404}, {f.as(ids.New(), uuid.Nil, htxroles.Admin), 404}} {
		r := nativePost(tc.who, "/v1/household-profiles", ids.New().String(), body)
		if r.status != tc.status {
			t.Fatalf("create scope: %d %v", r.status, r.body)
		}
	}
	duplicate := nativePost(admin, "/v1/household-profiles", "duplicate-id", nativeBody(tree.c1, "0907285032", "079123456731", "1"))
	if duplicate.status != 409 || duplicate.body["code"] != "vault.household_national_id_taken" {
		t.Fatalf("CCCD uniqueness: %d %v", duplicate.status, duplicate.body)
	}
	body["expected_revision"] = 1
	body["cultivated_area_ha"] = "3"
	body["active"] = false
	body["identity"].(map[string]any)["phone"] = "0907285033"
	updated := nativePost(admin, path+"/revisions", "update-admin", body)
	if updated.status != 200 {
		t.Fatalf("Admin edit: %d %v", updated.status, updated.body)
	}
	current := admin.appDo(http.MethodGet, path, nil)
	if current.body["code"] != code || current.body["revision"] != float64(2) {
		t.Fatal("code changed or revision did not advance")
	}
	own := f.as(f.tenant, tree.c2, htxroles.Member)
	masked := own.appDo(http.MethodGet, path, nil)
	if str(masked.body, "identity", "phone") != "0907***033" || str(masked.body, "identity", "national_id") != "079******731" {
		t.Fatal("Member API released raw identity")
	}
	filtered := admin.appDo(http.MethodGet, "/v1/cooperatives/"+tree.c2.String()+"/households?status=inactive", nil)
	if filtered.status != 200 || len(items(filtered)) != 1 {
		t.Fatalf("inactive filter: %d %v", filtered.status, filtered.body)
	}
	body["expected_revision"] = 2
	body["active"] = true
	body["cooperative_ref"] = tree.c1.String()
	moved := nativePost(admin, path+"/revisions", "cannot-move", body)
	if moved.status < 400 {
		t.Fatal("native HTX was changed")
	}
	replay := nativePost(admin, "/v1/household-profiles", "create-admin", nativeBody(tree.c2, "0907285031", "079123456731", "2"))
	if replay.status != 201 {
		t.Fatalf("old create retry: %d %v", replay.status, replay.body)
	}
	after := admin.appDo(http.MethodGet, path, nil)
	if str(after.body, "identity", "phone") != "0907285033" {
		t.Fatal("old create retry overwrote latest identity")
	}
	deleted := nativePost(admin, path+"/deletions", "delete-admin", map[string]any{"expected_revision": 2})
	if deleted.status != 200 {
		t.Fatalf("Admin delete: %d %v", deleted.status, deleted.body)
	}
	if r := admin.appDo(http.MethodGet, path, nil); r.status != 404 {
		t.Fatal("deleted household remained visible")
	}
	if r := nativePost(admin, path+"/deletions", "delete-admin", map[string]any{"expected_revision": 2}); r.status != 200 {
		t.Fatalf("delete retry: %d", r.status)
	}
	if r := nativePost(rep, "/v1/household-profiles", "rep-create", nativeBody(tree.c1, "0907285034", "079123456734", "1")); r.status != 201 {
		t.Fatalf("Rep create: %d %v", r.status, r.body)
	}
}

type identityCommitOutage struct {
	port.HouseholdIdentities
	writer port.NativeHouseholdIdentityWriter
	fail   bool
}

type identityReadHook struct {
	port.HouseholdIdentities
	reader port.NativeHouseholdIdentityReader
	after  func()
}

func (r identityReadHook) NativeHouseholdIdentity(ctx context.Context, ref uuid.UUID) (port.HouseholdIdentity, error) {
	id, err := r.reader.NativeHouseholdIdentity(ctx, ref)
	if err == nil {
		r.after()
	}
	return id, err
}

func TestAH285NativeProfileRejectsConcurrentRevisionChange(t *testing.T) {
	f := nativeVaultFixture(t)
	tree := f.seedTree(f.root())
	rep := f.as(f.tenant, tree.c1, htxroles.Representative)
	created := nativePost(rep, "/v1/household-profiles", "read-race-create", nativeBody(tree.c1, "0907285091", "079123456791", "2"))
	if created.status != 201 {
		t.Fatalf("create: %d %v", created.status, created.body)
	}
	ref := created.body["household_ref"].(string)
	original := f.svc.Vault
	f.svc.Vault = identityReadHook{HouseholdIdentities: original, reader: original.(port.NativeHouseholdIdentityReader), after: func() {
		if _, err := f.env.AdminPool(t, migrations.PF).Exec(context.Background(), `UPDATE pf_coop.household SET cultivated_area_ha=3 WHERE household_ref=$1`, ref); err != nil {
			t.Fatal(err)
		}
	}}
	if r := rep.appDo(http.MethodGet, "/v1/households/"+ref+"/profile", nil); r.status != 503 || r.body["identity"] != nil {
		t.Fatalf("concurrent profile returned a mixed revision: %d", r.status)
	}
}

func (w *identityCommitOutage) WriteNativeHouseholdIdentity(ctx context.Context, command uuid.UUID, id port.HouseholdIdentity) error {
	err := w.writer.WriteNativeHouseholdIdentity(ctx, command, id)
	if err != nil {
		return err
	}
	if w.fail {
		w.fail = false
		return problem.New(503, "test.outage", "simulated lost Vault response")
	}
	return nil
}
func TestAH285NativeCreateRecoversAfterVaultCommit(t *testing.T) {
	f := nativeVaultFixture(t)
	tree := f.seedTree(f.root())
	rep := f.as(f.tenant, tree.c1, htxroles.Representative)
	original := f.svc.Vault
	f.svc.Vault = &identityCommitOutage{HouseholdIdentities: original, writer: original.(port.NativeHouseholdIdentityWriter), fail: true}
	body := nativeBody(tree.c1, "0907285041", "079123456741", "2")
	if r := nativePost(rep, "/v1/household-profiles", "lost-response", body); r.status != 503 {
		t.Fatalf("lost response: %d %v", r.status, r.body)
	}
	r := nativePost(rep, "/v1/household-profiles", "lost-response", body)
	if r.status != 201 {
		t.Fatalf("resume: %d %v", r.status, r.body)
	}
	var count int
	if err := f.env.AdminPool(t, migrations.Vault).QueryRow(context.Background(), `SELECT count(*) FROM vault.native_household_command WHERE tenant_id=$1`, f.tenant).Scan(&count); err != nil || count != 1 {
		t.Fatalf("Vault command duplicated: %d %v", count, err)
	}
}

func TestAH285NativeValidationBeforeIdentityMutation(t *testing.T) {
	f := nativeVaultFixture(t)
	tree := f.seedTree(f.root())
	admin := f.as(f.tenant, uuid.Nil, htxroles.Admin)
	for _, mutate := range []func(map[string]any){
		func(b map[string]any) { b["cooperative_ref"] = uuid.Nil.String() }, func(b map[string]any) { b["cultivated_area_ha"] = "0" }, func(b map[string]any) { b["livestock_area_ha"] = "-1" },
		func(b map[string]any) { b["identity"].(map[string]any)["national_id"] = "07912345678" }, func(b map[string]any) { b["identity"].(map[string]any)["phone"] = "1907285031" },
	} {
		body := nativeBody(tree.c1, "0907285051", "079123456751", "2")
		mutate(body)
		if r := nativePost(admin, "/v1/household-profiles", ids.New().String(), body); r.status != 422 {
			t.Fatalf("invalid create: %d %v", r.status, r.body)
		}
	}
	var count int
	if err := f.env.AdminPool(t, migrations.Vault).QueryRow(context.Background(), `SELECT count(*) FROM vault.subject WHERE tenant_id=$1`, f.tenant).Scan(&count); err != nil || count != 0 {
		t.Fatal("invalid form wrote an identity")
	}
}

func TestAH285NativeUpdateRecoversWithoutExposingPartialIdentity(t *testing.T) {
	f := nativeVaultFixture(t)
	tree := f.seedTree(f.root())
	rep := f.as(f.tenant, tree.c1, htxroles.Representative)
	body := nativeBody(tree.c1, "0907285071", "079123456771", "2")
	created := nativePost(rep, "/v1/household-profiles", "update-recovery-create", body)
	if created.status != 201 {
		t.Fatalf("create: %d %v", created.status, created.body)
	}
	path := "/v1/households/" + created.body["household_ref"].(string) + "/profile"
	original := f.svc.Vault
	f.svc.Vault = &identityCommitOutage{HouseholdIdentities: original, writer: original.(port.NativeHouseholdIdentityWriter), fail: true}
	body["expected_revision"] = 1
	body["identity"].(map[string]any)["phone"] = "0907285072"
	if r := nativePost(rep, path+"/revisions", "update-lost-response", body); r.status != 503 {
		t.Fatalf("lost response: %d %v", r.status, r.body)
	}
	if r := rep.appDo(http.MethodGet, path, nil); r.status != 503 || r.body["identity"] != nil {
		t.Fatalf("partially committed identity became visible: %d", r.status)
	}
	if r := nativePost(rep, path+"/revisions", "update-lost-response", body); r.status != 200 {
		t.Fatalf("resume update: %d %v", r.status, r.body)
	}
	f.svc.Vault = original
	current := rep.appDo(http.MethodGet, path, nil)
	if current.status != 200 || current.body["revision"] != float64(2) || str(current.body, "identity", "phone") != "0907285072" {
		t.Fatal("resumed update did not converge")
	}
}

func TestAH285NativeDeletionIncludesHistoryFromPreviousCooperative(t *testing.T) {
	f := nativeVaultFixture(t)
	tree := f.seedTree(f.root())
	rep := f.as(f.tenant, tree.c1, htxroles.Representative)
	created := nativePost(rep, "/v1/household-profiles", "old-coop-history-create", nativeBody(tree.c1, "0907285081", "079123456781", "2"))
	if created.status != 201 {
		t.Fatalf("create: %d %v", created.status, created.body)
	}
	ref := created.body["household_ref"].(string)
	seedNativeProduction(t, f, uuid.MustParse(ref), tree.c2, "1", "completed")
	r := nativePost(rep, "/v1/households/"+ref+"/profile/deletions", "old-coop-history-delete", map[string]any{"expected_revision": 1})
	if r.status != 409 || r.body["code"] != "party.household_has_production" {
		t.Fatalf("history owned by previous cooperative must block deletion: %d %v", r.status, r.body)
	}
}

func seedNativeProduction(t *testing.T, f *fixture, hh, coop uuid.UUID, area, status string) uuid.UUID {
	t.Helper()
	season := ids.New()
	_, err := f.env.AdminPool(t, migrations.PF).Exec(context.Background(), `INSERT INTO pf_coop.season_current
 (tenant_id,season_ref,plot_ref,plot_code,household_ref,cooperative_ref,commune_code,season_code,season_year,variety_code,
 sop_template_ref,sop_version_id,sop_version_no,sowing_date,expected_harvest_date,harvest_open_date,area_ha,status,current_event_id,
 version,location_status,gps_source,on_behalf,opened_by,opened_at,harvest_id)
 VALUES($1,$2,$3,'AH285',$4,$5,'30001','DX',2026,'OM18',$6,$7,1,'2026-01-01','2026-04-01','2026-03-20',$8::numeric,$9,$10,
 1,'missing','denied',false,$11,now(),CASE WHEN $9='completed' THEN $12::uuid ELSE NULL END)`,
		f.tenant, season, ids.New(), hh, coop, ids.New(), ids.New(), area, status, ids.New(), ids.New(), ids.New())
	if err != nil {
		t.Fatal(err)
	}
	return season
}

type nativeHistoryRace struct{ after func() }

func (h nativeHistoryRace) HasHouseholdProduction(context.Context, uuid.UUID) (bool, error) {
	h.after()
	return false, nil
}

func TestAH285NativeProductionGuardAndInactiveState(t *testing.T) {
	f := nativeVaultFixture(t)
	tree := f.seedTree(f.root())
	rep := f.as(f.tenant, tree.c1, htxroles.Representative)
	body := nativeBody(tree.c1, "0907285061", "079123456761", "2")
	created := nativePost(rep, "/v1/household-profiles", "production-create", body)
	if created.status != 201 {
		t.Fatalf("create: %d %v", created.status, created.body)
	}
	ref := created.body["household_ref"].(string)
	path := "/v1/households/" + ref + "/profile"
	season := seedNativeProduction(t, f, uuid.MustParse(ref), tree.c1, "1.5", "open")
	body["expected_revision"] = 1
	body["cultivated_area_ha"] = "1"
	if r := nativePost(rep, path+"/revisions", "cannot-shrink", body); r.status != 422 {
		t.Fatalf("area guard: %d %v", r.status, r.body)
	}
	body["cultivated_area_ha"] = "2"
	body["active"] = false
	if r := nativePost(rep, path+"/revisions", "inactive", body); r.status != 200 {
		t.Fatalf("inactive with running season: %d %v", r.status, r.body)
	}
	if _, err := f.env.AdminPool(t, migrations.PF).Exec(context.Background(), `UPDATE pf_coop.season_current SET status='completed',harvest_id=$2 WHERE season_ref=$1`, season, ids.New()); err != nil {
		t.Fatal(err)
	}
	r := nativePost(rep, path+"/deletions", "cannot-delete-history", map[string]any{"expected_revision": 2})
	if r.status != 409 || r.body["code"] != "party.household_has_production" {
		t.Fatalf("closed season must block deletion: %d %v", r.status, r.body)
	}
	// An owner answer can become stale before the PF commit. The DB fence must
	// reject the delete and release this failed delete's pending command.
	second := nativePost(rep, "/v1/household-profiles", "race-create", nativeBody(tree.c1, "0907285062", "079123456762", "2"))
	secondRef := uuid.MustParse(second.body["household_ref"].(string))
	secondPath := "/v1/households/" + secondRef.String() + "/profile"
	f.svc.ProductionHistory = nativeHistoryRace{after: func() { seedNativeProduction(t, f, secondRef, tree.c1, "1", "completed") }}
	raced := nativePost(rep, secondPath+"/deletions", "race-delete", map[string]any{"expected_revision": 1})
	if raced.status != 409 || raced.body["code"] != "party.household_has_production" {
		t.Fatalf("DB recheck: %d %v", raced.status, raced.body)
	}
	update := nativeBody(tree.c1, "0907285062", "079123456762", "2")
	update["expected_revision"] = 1
	update["active"] = false
	if r := nativePost(rep, secondPath+"/revisions", "after-failed-delete", update); r.status != 200 {
		t.Fatalf("failed delete left an unrecoverable lease: %d %v", r.status, r.body)
	}
}
