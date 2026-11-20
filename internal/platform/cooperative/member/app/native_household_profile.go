package app

import (
	"context"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/member/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/member/store"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type NativeHouseholdProfile struct {
	Household     domain.Household
	Identity      port.HouseholdIdentity
	ContactAccess string
}

// NativeHouseholdProfile serves the existing PF application surface. Identity
// is read only after the owner lookup commits; the Vault callback can reuse a
// one-connection PF pool without recursion into the identity adapter.
func (s *Service) NativeHouseholdProfile(ctx context.Context, ref uuid.UUID) (NativeHouseholdProfile, error) {
	p, err := nativeHouseholdCaller(ctx, htxroles.Read)
	if err != nil {
		return NativeHouseholdProfile{}, err
	}
	scope, err := s.NativeHouseholdIdentityScope(ctx, ref)
	if err != nil {
		return NativeHouseholdProfile{}, err
	}
	reader, ok := s.Vault.(port.NativeHouseholdIdentityReader)
	if !ok {
		return NativeHouseholdProfile{}, ErrDependency.WithDetail("Chưa nối định danh hộ theo quyền HTX")
	}
	var household *domain.Household
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		household, e = store.HouseholdByRef(ctx, tx, ref)
		return e
	})
	if err != nil {
		return NativeHouseholdProfile{}, err
	}
	if household == nil || household.CooperativeRef != scope.CooperativeRef {
		return NativeHouseholdProfile{}, ErrHouseholdNotFound
	}
	identity, err := reader.NativeHouseholdIdentity(ctx, ref)
	if err != nil {
		return NativeHouseholdProfile{}, vaultError(err)
	}
	latest, err := s.NativeHouseholdIdentityScope(ctx, ref)
	if err != nil {
		return NativeHouseholdProfile{}, err
	}
	if latest.CooperativeRef != scope.CooperativeRef || latest.ContactAccess != scope.ContactAccess {
		return NativeHouseholdProfile{}, ErrHouseholdNotFound
	}
	return NativeHouseholdProfile{Household: *household, Identity: identity, ContactAccess: latest.ContactAccess}, nil
}
