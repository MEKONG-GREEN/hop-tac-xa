package app_test

import (
	"context"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
	"github.com/jackc/pgx/v5"
	"testing"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/vault/identity/client"
	"github.com/MEKONG-GREEN/hop-tac-xa/migrations"
	"github.com/google/uuid"
)

func TestAH99ScopedPhotoKeyDoesNotGrantIdentityOrGenericKeyListing(t *testing.T) {
	s, env := newService(t)
	tid, coop := ids.New(), ids.New()
	created, err := s.Create(as(tid, true, client.RoleRegistrar), client.PurposeRegistryAdmin, sample("0907990011"))
	if err != nil {
		t.Fatal(err)
	}
	s.DB = env.SizedPool(t, migrations.Vault, role, 1)
	p := authz.Principal{TenantID: tid, ActorRef: ids.New(), OrgRef: coop, Roles: []string{htxroles.Member}}
	ctx := authz.WithPrincipal(context.Background(), p)
	calls := 0
	s.HouseholdScopes = householdScopeFunc(func(proof context.Context, ref uuid.UUID) (client.HouseholdIdentityScope, error) {
		calls++
		if ref != created.Ref {
			return client.HouseholdIdentityScope{}, client.ProblemNotFound
		}
		err := tenant.WithTenantTx(proof, s.DB, tid, func(tx pgx.Tx) error { var n int; return tx.QueryRow(proof, "SELECT 1").Scan(&n) })
		return client.HouseholdIdentityScope{Ref: ref, CooperativeRef: coop, ContactAccess: "masked"}, err
	})
	keys, err := s.HouseholdPubKeys(ctx, []uuid.UUID{created.Ref})
	if err != nil || len(keys) != 1 || len(keys[0].PublicKey) != 32 || calls != 2 {
		t.Fatalf("scoped encryption key: %v calls=%d", err, calls)
	}
	if _, err = s.PubKeys(ctx, []string{"82-28783"}); code(err) != "forbidden" {
		t.Fatalf("generic key listing widened: %v", err)
	}
	if _, err = s.Resolve(ctx, client.PurposeContact, created.Ref); code(err) != "forbidden" {
		t.Fatalf("identity resolution widened: %v", err)
	}
	noOrg := p
	noOrg.OrgRef = uuid.Nil
	if _, err = s.HouseholdPubKeys(authz.WithPrincipal(context.Background(), noOrg), []uuid.UUID{created.Ref}); code(err) != problem.NodeScopeRequired.Code {
		t.Fatalf("missing cooperative: %v", err)
	}
	admin := p
	admin.Roles = []string{htxroles.Admin}
	admin.MFA = false
	if _, err = s.HouseholdPubKeys(authz.WithPrincipal(context.Background(), admin), []uuid.UUID{created.Ref}); code(err) != problem.MFARequired.Code {
		t.Fatalf("admin without MFA: %v", err)
	}
	p.OrgRef = ids.New()
	if _, err = s.HouseholdPubKeys(authz.WithPrincipal(context.Background(), p), []uuid.UUID{created.Ref}); code(err) != client.ProblemNotFound.Code {
		t.Fatalf("foreign cooperative key: %v", err)
	}
	p.OrgRef = coop
	p.Roles = []string{htxroles.Member, htxroles.Representative}
	if _, err = s.HouseholdPubKeys(authz.WithPrincipal(context.Background(), p), []uuid.UUID{created.Ref}); code(err) != "forbidden" {
		t.Fatalf("mixed readonly roles: %v", err)
	}
	p.Roles = []string{htxroles.Member}
	calls = 0
	s.HouseholdScopes = householdScopeFunc(func(context.Context, uuid.UUID) (client.HouseholdIdentityScope, error) {
		calls++
		current := coop
		if calls == 2 {
			current = ids.New()
		}
		return client.HouseholdIdentityScope{Ref: created.Ref, CooperativeRef: current, ContactAccess: "masked"}, nil
	})
	if _, err = s.HouseholdPubKeys(authz.WithPrincipal(context.Background(), p), []uuid.UUID{created.Ref}); code(err) != client.ProblemNotFound.Code {
		t.Fatalf("changed owner proof: %v", err)
	}
}

type householdScopeFunc func(context.Context, uuid.UUID) (client.HouseholdIdentityScope, error)

func (fn householdScopeFunc) HouseholdIdentityScope(ctx context.Context, ref uuid.UUID) (client.HouseholdIdentityScope, error) {
	return fn(ctx, ref)
}
func (fn householdScopeFunc) HouseholdRegistrationScope(context.Context, uuid.UUID) (client.HouseholdIdentityScope, error) {
	return client.HouseholdIdentityScope{}, problem.Forbidden
}

func TestAH285ReadHouseholdIdentityMasksAtVaultAndKeepsGenericPermissions(t *testing.T) {
	s, env := newService(t)
	s.DB = env.SizedPool(t, migrations.Vault, role, 1)
	tid, coop := ids.New(), ids.New()
	in := sample("0907285011")
	in.NationalID = "079123456789"
	created, err := s.Create(as(tid, true, client.RoleRegistrar), client.PurposeRegistryAdmin, in)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name            string
		roles           []string
		phone, national string
	}{
		{"Member", []string{htxroles.Member}, "0907***011", "079******789"},
		{"Representative", []string{htxroles.Representative}, "0907285011", "079123456789"},
		{"Admin", []string{htxroles.Admin}, "0907285011", "079123456789"},
		{"Mixed native roles", []string{htxroles.Member, htxroles.Representative}, "0907285011", "079123456789"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := authz.Principal{TenantID: tid, ActorRef: ids.New(), OrgRef: coop, Roles: tc.roles, MFA: true}
			ctx := authz.WithRawToken(authz.WithPrincipal(context.Background(), p), "original-household-user")
			calls := 0
			s.HouseholdScopes = householdScopeFunc(func(proofCtx context.Context, ref uuid.UUID) (client.HouseholdIdentityScope, error) {
				calls++
				proofP, _ := authz.FromContext(proofCtx)
				token, _ := authz.RawToken(proofCtx)
				if token != "original-household-user" || proofP.ActorRef != p.ActorRef || ref != created.Ref {
					t.Fatal("caller was replaced")
				}
				// Even an overly broad owner response cannot unmask a Member.
				return client.HouseholdIdentityScope{Ref: ref, CooperativeRef: coop, ContactAccess: "full"}, nil
			})
			out, err := s.ReadHouseholdIdentity(ctx, client.PurposeContact, created.Ref)
			if err != nil {
				t.Fatal(err)
			}
			if calls != 2 || out.Phone != tc.phone || out.NationalID != tc.national || out.FullName != in.FullName {
				t.Fatal("incorrect scoped identity projection")
			}
			if _, err := s.Resolve(ctx, client.PurposeContact, created.Ref); code(err) != "forbidden" {
				t.Fatalf("generic Resolve widened: %v", err)
			}
		})
	}
}

func TestAH285ReadHouseholdIdentityFailsClosedAndRechecksMembership(t *testing.T) {
	s, _ := newService(t)
	tid, coop := ids.New(), ids.New()
	created, err := s.Create(as(tid, true, client.RoleRegistrar), client.PurposeRegistryAdmin, sample("0907285012"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := authz.WithPrincipal(context.Background(), authz.Principal{TenantID: tid, ActorRef: ids.New(), OrgRef: coop, Roles: []string{htxroles.Representative}})
	if _, err := s.ReadHouseholdIdentity(ctx, client.PurposeContact, created.Ref); code(err) != "vault.household_scope_unavailable" {
		t.Fatalf("missing verifier: %v", err)
	}
	calls := 0
	s.HouseholdScopes = householdScopeFunc(func(context.Context, uuid.UUID) (client.HouseholdIdentityScope, error) {
		calls++
		if calls > 1 {
			return client.HouseholdIdentityScope{}, client.ProblemNotFound
		}
		return client.HouseholdIdentityScope{Ref: created.Ref, CooperativeRef: coop, ContactAccess: "full"}, nil
	})
	out, err := s.ReadHouseholdIdentity(ctx, client.PurposeContact, created.Ref)
	if code(err) != client.CodeNotFound || out.Phone != "" || out.NationalID != "" || calls != 2 {
		t.Fatalf("revoked proof released identity: %v", err)
	}
	s.HouseholdScopes = householdScopeFunc(func(context.Context, uuid.UUID) (client.HouseholdIdentityScope, error) {
		return client.HouseholdIdentityScope{Ref: created.Ref, CooperativeRef: ids.New(), ContactAccess: "full"}, nil
	})
	if _, err := s.ReadHouseholdIdentity(ctx, client.PurposeContact, created.Ref); code(err) != client.CodeNotFound {
		t.Fatalf("foreign cooperative: %v", err)
	}
	admin := as(tid, false, htxroles.Admin)
	if _, err := s.ReadHouseholdIdentity(admin, client.PurposeContact, created.Ref); code(err) != "mfa_required" {
		t.Fatalf("Admin without MFA: %v", err)
	}
	person := sample("0907285013")
	person.Kind = client.SubjectPerson
	other, err := s.Create(as(tid, true, client.RoleRegistrar), client.PurposeRegistryAdmin, person)
	if err != nil {
		t.Fatal(err)
	}
	s.HouseholdScopes = householdScopeFunc(func(_ context.Context, ref uuid.UUID) (client.HouseholdIdentityScope, error) {
		return client.HouseholdIdentityScope{Ref: ref, CooperativeRef: coop, ContactAccess: "full"}, nil
	})
	if _, err := s.ReadHouseholdIdentity(ctx, client.PurposeContact, other.Ref); code(err) != client.CodeNotFound {
		t.Fatalf("person leaked through household API: %v", err)
	}
}

// AH-286 (FN-04/AC-04, QTPQ-Q-61 AC-N1, AC-N2): Đại diện HTX đọc hộ của HTX đã chia sẻ cho mình chỉ nhận SĐT 4 đầu/3
// cuối, CCCD 3 đầu/3 cuối, kể cả khi bên chủ trả "full"; hộ của HTX mình vẫn đầy đủ. Bằng chứng chủ không đúng dạng
// chia sẻ chỉ xem (thiếu cờ, cho ghi, không che) hoặc vai khác Đại diện → không tìm thấy.
func TestAH286SharedHouseholdIdentityAlwaysMasked(t *testing.T) {
	s, env := newService(t)
	s.DB = env.SizedPool(t, migrations.Vault, role, 1)
	tid, own, owner := ids.New(), ids.New(), ids.New()
	in := sample("0913456222")
	in.NationalID = "079123456123"
	created, err := s.Create(as(tid, true, client.RoleRegistrar), client.PurposeRegistryAdmin, in)
	if err != nil {
		t.Fatal(err)
	}
	read := func(roles []string, scope client.HouseholdIdentityScope) (client.Identity, error) {
		t.Helper()
		p := authz.Principal{TenantID: tid, ActorRef: ids.New(), OrgRef: own, Roles: roles, MFA: true}
		ctx := authz.WithRawToken(authz.WithPrincipal(context.Background(), p), "original-household-user")
		s.HouseholdScopes = householdScopeFunc(func(_ context.Context, ref uuid.UUID) (client.HouseholdIdentityScope, error) {
			scope.Ref = ref
			return scope, nil
		})
		return s.ReadHouseholdIdentity(ctx, client.PurposeContact, created.Ref)
	}
	rep := []string{htxroles.Representative}
	shared := client.HouseholdIdentityScope{CooperativeRef: owner, ContactAccess: "masked", Shared: true}
	out, err := read(rep, shared)
	if err != nil {
		t.Fatal(err)
	}
	if out.Phone != "0913***222" || out.NationalID != "079******123" || out.FullName != in.FullName {
		t.Fatalf("shared household must be masked: %q %q", out.Phone, out.NationalID)
	}
	out, err = read(rep, client.HouseholdIdentityScope{CooperativeRef: own, ContactAccess: "full"})
	if err != nil || out.Phone != "0913456222" || out.NationalID != "079123456123" {
		t.Fatalf("own household stays full: %v %q %q", err, out.Phone, out.NationalID)
	}
	for _, tc := range []struct {
		name  string
		roles []string
		scope client.HouseholdIdentityScope
	}{
		{"owner forgot shared flag", rep, client.HouseholdIdentityScope{CooperativeRef: owner, ContactAccess: "masked"}},
		{"owner returned full", rep, client.HouseholdIdentityScope{CooperativeRef: owner, ContactAccess: "full", Shared: true}},
		{"owner granted write", rep, client.HouseholdIdentityScope{CooperativeRef: owner, ContactAccess: "masked", Shared: true, CanWrite: true}},
		{"Member never benefits", []string{htxroles.Member}, shared},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := read(tc.roles, tc.scope)
			if code(err) != client.CodeNotFound || out.Phone != "" || out.NationalID != "" {
				t.Fatalf("got %v %q %q", err, out.Phone, out.NationalID)
			}
		})
	}
}
