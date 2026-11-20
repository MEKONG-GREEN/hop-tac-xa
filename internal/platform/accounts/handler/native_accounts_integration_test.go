package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/clock"
	"github.com/MEKONG-GREEN/hop-tac-xa/migrations"
)

func TestNativeFN07CreationAndSecretReplay(t *testing.T) {
	f := newFixtureAt(t, clock.System{}, true)
	admin := f.as(f.tenant, uuid.Nil, htxroles.Admin)
	input := map[string]any{"username": "native-rep", "full_name": "Đại diện HTX", "phone": "0912345678", "cooperative_ref": f.coop30292.String(), "roles": []string{htxroles.Representative, htxroles.Member}}
	body, _ := json.Marshal(input)
	const operation = "native-fn07-create-once"
	r := admin.raw(http.MethodPost, "/v1/accounts/htx-users", operation, "application/json", body)
	if r.status != http.StatusCreated {
		t.Fatalf("create %d: %v", r.status, r.body)
	}
	password := str(r.body, "temporary_password")
	if len(password) != 8 {
		t.Fatal("temporary password length")
	}
	user := uuid.MustParse(str(r.body, "user", "id"))
	if !f.dir.PasswordMatches(user, password) {
		t.Fatal("credential not installed")
	}
	u, _ := f.dir.GetUser(context.Background(), user)
	if len(u.RequiredActions) != 1 || u.RequiredActions[0] != port.ActionUpdatePassword {
		t.Fatal("first-login setup missing")
	}
	var cached []byte
	if err := f.env.AdminPool(t, migrations.PF).QueryRow(context.Background(), `SELECT response_body FROM shared.idempotency_key WHERE tenant_id=$1 AND idem_key=$2`, f.tenant, operation).Scan(&cached); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(cached), password) {
		t.Fatal("credential persisted in replay cache")
	}
	if strings.Contains(string(cached), "0912345678") || strings.Contains(string(cached), "native-rep") || strings.Contains(string(cached), "Đại diện") {
		t.Fatal("IAM profile PII persisted in PF replay cache")
	}
	r = admin.raw(http.MethodPost, "/v1/accounts/htx-users", operation, "application/json", body)
	if r.status != http.StatusCreated || r.body["temporary_password"] != nil {
		t.Fatalf("HTTP replay %d must omit secret", r.status)
	}
	// Durable replay after the generic HTTP cache expires must also omit the secret.
	if _, err := f.env.AdminPool(t, migrations.PF).Exec(context.Background(), `UPDATE shared.idempotency_key SET expires_at=now()-interval '1 hour' WHERE tenant_id=$1 AND idem_key=$2`, f.tenant, operation); err != nil {
		t.Fatal(err)
	}
	r = admin.raw(http.MethodPost, "/v1/accounts/htx-users", operation, "application/json", body)
	if r.status != http.StatusOK || r.body["temporary_password"] != nil {
		t.Fatalf("durable replay %d: %v", r.status, r.body)
	}
	input["username"] = "another-rep"
	body, _ = json.Marshal(input)
	r = admin.raw(http.MethodPost, "/v1/accounts/htx-users", "native-phone-duplicate", "application/json", body)
	if r.status != http.StatusConflict {
		t.Fatalf("duplicate phone %d", r.status)
	}
	for _, role := range []string{htxroles.Representative, htxroles.Member, "account_admin"} {
		who := f.as(f.tenant, f.coop30292, role)
		r = who.do(http.MethodGet, "/v1/accounts/htx-users", nil)
		if r.status != http.StatusForbidden {
			t.Fatalf("%s list %d", role, r.status)
		}
	}
	r = admin.do(http.MethodGet, "/v1/accounts/htx-users", nil)
	if r.status != http.StatusOK || len(r.body["items"].([]any)) != 1 {
		t.Fatalf("native list %d: %v", r.status, r.body)
	}
	r = admin.do(http.MethodPost, "/v1/accounts/users", person(f.coop30292, htxroles.Member))
	if r.status != http.StatusForbidden {
		t.Fatal("legacy creation bypass")
	}
}

func TestNativeFN07AdminNoCooperative(t *testing.T) {
	f := newFixtureAt(t, clock.System{}, true)
	admin := f.as(f.tenant, uuid.Nil, htxroles.Admin)
	input := map[string]any{"username": "native-admin", "full_name": "Admin HTX", "phone": "0912345679", "email": "native-admin@example.vn", "roles": []string{htxroles.Admin}}
	r := admin.do(http.MethodPost, "/v1/accounts/htx-users", input)
	if r.status != http.StatusCreated {
		t.Fatalf("create Admin %d: %v", r.status, r.body)
	}
	id := uuid.MustParse(str(r.body, "user", "id"))
	u, _ := f.dir.GetUser(context.Background(), id)
	if u.OrgRef != uuid.Nil || !u.EmailVerified {
		t.Fatal("Admin organization/email OTP")
	}
	roles, _ := f.dir.RealmRoles(context.Background(), id)
	if len(roles) != 1 || roles[0] != htxroles.Admin {
		t.Fatal("Admin role")
	}
	other := f.as(uuid.New(), uuid.Nil, htxroles.Admin)
	r = other.do(http.MethodGet, "/v1/accounts/htx-users/"+id.String(), nil)
	if r.status != http.StatusNotFound {
		t.Fatalf("cross tenant detail %d", r.status)
	}
}

func TestNativeFN07RetryAfterConfirmedCompensation(t *testing.T) {
	f := newFixtureAt(t, clock.System{}, true)
	admin := f.as(f.tenant, uuid.Nil, htxroles.Admin)
	input := map[string]any{"username": "native-retry", "full_name": "Thử lại", "phone": "0912345680", "cooperative_ref": f.coop30292.String(), "roles": []string{htxroles.Member}}
	body, _ := json.Marshal(input)
	f.dir.Fail("SetTemporaryPassword", port.ErrUnavailable)
	r := admin.raw(http.MethodPost, "/v1/accounts/htx-users", "native-retry-confirmed", "application/json", body)
	if r.status != http.StatusServiceUnavailable {
		t.Fatalf("expected temporary IAM outage, got %d", r.status)
	}
	users, err := f.dir.FindUsers(context.Background(), port.Query{Username: "native-retry"})
	if err != nil || len(users) != 0 {
		t.Fatal("compensation did not delete unused IAM user")
	}
	f.dir.Fail("SetTemporaryPassword", nil)
	r = admin.raw(http.MethodPost, "/v1/accounts/htx-users", "native-retry-confirmed", "application/json", body)
	if r.status != http.StatusCreated {
		t.Fatalf("safe same-key retry %d: %v", r.status, r.body)
	}
}

func TestNativeFN07ConcurrentPhoneClaim(t *testing.T) {
	f := newFixtureAt(t, clock.System{}, true)
	admin := f.as(f.tenant, uuid.Nil, htxroles.Admin)
	var wait sync.WaitGroup
	statuses := make(chan int, 2)
	for _, username := range []string{"native-race-a", "native-race-b"} {
		wait.Go(func() {
			body, _ := json.Marshal(map[string]any{"username": username, "full_name": "Tranh chấp SĐT", "phone": "0912345681", "cooperative_ref": f.coop30292.String(), "roles": []string{htxroles.Member}})
			response := admin.raw(http.MethodPost, "/v1/accounts/htx-users", username+"-create", "application/json", body)
			if response.status == http.StatusServiceUnavailable {
				// The other IAM request owns the global provisioning mutex. Retry
				// after it completes below, preserving this logical operation key.
				statuses <- response.status
				return
			}
			statuses <- response.status
		})
	}
	wait.Wait()
	close(statuses)
	created, conflict := 0, 0
	for status := range statuses {
		switch status {
		case http.StatusCreated:
			created++
		case http.StatusConflict:
			conflict++
		case http.StatusServiceUnavailable:
			conflict++
		default:
			t.Errorf("unexpected %d", status)
		}
	}
	if created != 1 || conflict != 1 {
		t.Fatalf("created=%d conflict=%d", created, conflict)
	}
	users, err := f.dir.FindUsers(context.Background(), port.Query{Attributes: map[string]string{port.AttrPhone: "0912345681"}})
	if err != nil || len(users) != 1 {
		t.Fatal("duplicate IAM identity")
	}
	loser := "native-race-a"
	if users[0].Username == loser {
		loser = "native-race-b"
	}
	body, _ := json.Marshal(map[string]any{"username": loser, "full_name": "Tranh chấp SĐT", "phone": "0912345681", "cooperative_ref": f.coop30292.String(), "roles": []string{htxroles.Member}})
	r := admin.raw(http.MethodPost, "/v1/accounts/htx-users", loser+"-create", "application/json", body)
	if r.status != http.StatusConflict {
		t.Fatalf("phone must remain unique on retry, got %d", r.status)
	}
}

func TestNativeFN07RejectsAnotherLedgerActionKeyBeforeIAM(t *testing.T) {
	f := newFixtureAt(t, clock.System{}, true)
	admin := f.as(f.tenant, uuid.Nil, htxroles.Admin)
	input := map[string]any{"username": "native-key-first", "full_name": "Khóa tài khoản", "phone": "0912345682", "cooperative_ref": f.coop30292.String(), "roles": []string{htxroles.Member}}
	r := admin.do(http.MethodPost, "/v1/accounts/htx-users", input)
	if r.status != 201 {
		t.Fatalf("initial %d", r.status)
	}
	user := str(r.body, "user", "id")
	const used = "native-key-used-by-lock"
	body, _ := json.Marshal(map[string]any{"reason": goodReason})
	r = admin.raw(http.MethodPost, "/v1/accounts/users/"+user+"/lock", used, "application/json", body)
	if r.status != 200 {
		t.Fatalf("lock %d", r.status)
	}
	if _, err := f.env.AdminPool(t, migrations.PF).Exec(context.Background(), `UPDATE shared.idempotency_key SET expires_at=now()-interval '1 hour' WHERE tenant_id=$1 AND idem_key=$2`, f.tenant, used); err != nil {
		t.Fatal(err)
	}
	input["username"] = "native-key-second"
	input["phone"] = "0912345683"
	body, _ = json.Marshal(input)
	r = admin.raw(http.MethodPost, "/v1/accounts/htx-users", used, "application/json", body)
	if r.status != 422 {
		t.Fatalf("expected durable key conflict, got %d: %v", r.status, r.body)
	}
	users, err := f.dir.FindUsers(context.Background(), port.Query{Username: "native-key-second"})
	if err != nil || len(users) != 0 {
		t.Fatal("reused action key reached IAM create")
	}
}

func TestNativeFN07IAMValidationDoesNotHoldProvisioningMutex(t *testing.T) {
	f := newFixtureAt(t, clock.System{}, true)
	admin := f.as(f.tenant, uuid.Nil, htxroles.Admin)
	input := map[string]any{"username": "native-iam-rejected", "full_name": "Hồ sơ bị từ chối", "phone": "0912345684", "cooperative_ref": f.coop30292.String(), "roles": []string{htxroles.Member}}
	f.dir.Fail("CreateUser", port.ErrInvalidProfile)
	r := admin.do(http.MethodPost, "/v1/accounts/htx-users", input)
	if r.status != 422 {
		t.Fatalf("IAM validation %d", r.status)
	}
	f.dir.Fail("CreateUser", nil)
	input["username"] = "native-after-rejected"
	r = admin.do(http.MethodPost, "/v1/accounts/htx-users", input)
	if r.status != 201 {
		t.Fatalf("provisioning mutex retained after rejection: %d", r.status)
	}
}
