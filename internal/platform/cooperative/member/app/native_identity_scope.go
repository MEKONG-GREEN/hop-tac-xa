package app

import (
	"context"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/member/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/member/store"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// NativeIdentityScope is owner-verified metadata for the narrow household
// identity API. It contains no identity payload and never calls Vault.
type NativeIdentityScope struct {
	Shared         bool
	Ref            uuid.UUID
	CooperativeRef uuid.UUID
	Active         bool
	ContactAccess  string
	CanWrite       bool
	Revision       int64
}

func nativeHouseholdCaller(ctx context.Context, action htxroles.Action) (authz.Principal, error) {
	p, err := authz.Require(ctx, htxroles.With("FN-04", action)...)
	if err != nil {
		return authz.Principal{}, err
	}
	if !htxroles.Allows(p.Has, "FN-04", action) {
		return authz.Principal{}, problem.Forbidden
	}
	if !p.Has(htxroles.Admin) && p.OrgRef == uuid.Nil {
		return authz.Principal{}, problem.NodeScopeRequired
	}
	return p, nil
}

// NativeHouseholdRegistrationScope checks the target cooperative before a
// native identity is registered. Legacy KN roles do not widen native scope.
func (s *Service) NativeHouseholdRegistrationScope(ctx context.Context, ref uuid.UUID) (NativeIdentityScope, error) {
	p, err := nativeHouseholdCaller(ctx, htxroles.Create)
	if err != nil {
		return NativeIdentityScope{}, err
	}
	if !p.Has(htxroles.Admin) && ref != p.OrgRef {
		return NativeIdentityScope{}, ErrOrgNotFound
	}
	org, err := s.coopOrg(ctx, p.TenantID, ref)
	if err != nil {
		return NativeIdentityScope{}, err
	}
	if org == nil {
		return NativeIdentityScope{}, ErrOrgNotFound
	}
	if !p.Has(htxroles.Admin) {
		var ready bool
		err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
			var e error
			ready, e = store.NativeCooperativeScopeReady(ctx, tx, ref)
			return e
		})
		if err != nil {
			return NativeIdentityScope{}, err
		}
		if !ready {
			return NativeIdentityScope{}, ErrOrgNotFound
		}
	}
	return NativeIdentityScope{Ref: ref, CooperativeRef: ref, Active: org.Active, ContactAccess: "full", CanWrite: org.Active}, nil
}

// NativeHouseholdIdentityScope reads membership under ordinary tenant/node
// RLS, then enforces the native role's exact cooperative before returning any
// proof. No commune lookup, caller-node override or identity I/O is needed.
func (s *Service) NativeHouseholdIdentityScope(ctx context.Context, ref uuid.UUID) (NativeIdentityScope, error) {
	p, err := nativeHouseholdCaller(ctx, htxroles.Read)
	if err != nil {
		return NativeIdentityScope{}, err
	}
	var household *domain.Household
	err = s.nativeReadTx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		household, e = store.HouseholdByRef(ctx, tx, ref)
		return e
	})
	if err != nil {
		return NativeIdentityScope{}, err
	}
	if household == nil {
		return NativeIdentityScope{}, ErrHouseholdNotFound
	}
	shared := !p.Has(htxroles.Admin) && household.CooperativeRef != p.OrgRef
	if shared {
		allowed, err := s.nativeCanReadCooperative(ctx, p, household.CooperativeRef)
		if err != nil {
			return NativeIdentityScope{}, err
		}
		if !allowed {
			return NativeIdentityScope{}, ErrHouseholdNotFound
		}
	}
	if household.Pending {
		return NativeIdentityScope{}, ErrDependency.WithDetail("Hộ đang được cập nhật; thử lại thao tác đang chờ")
	}
	access := "full"
	if shared || htxroles.IsMemberOnly(p.Has) {
		access = "masked"
	}
	return NativeIdentityScope{Ref: household.Ref, CooperativeRef: household.CooperativeRef, Active: household.Active,
		ContactAccess: access, Shared: shared, CanWrite: !shared && household.CooperativeRef != uuid.Nil && htxroles.Allows(p.Has, "FN-04", htxroles.Update), Revision: household.Revision}, nil
}
