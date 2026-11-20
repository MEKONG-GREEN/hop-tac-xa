package app_test

import (
	"context"
	"testing"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/vault/identity/client"
	"github.com/MEKONG-GREEN/hop-tac-xa/migrations"
	"github.com/google/uuid"
)

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
