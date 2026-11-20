package app

import (
	"context"
	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
	"github.com/google/uuid"
)

func (s *Service) HouseholdPhotoKey(ctx context.Context, ref uuid.UUID) ([]byte, error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return nil, problem.Unauthenticated
	}
	if !htxroles.Allows(p.Has, "KHMV-FN-05", htxroles.Create) {
		return nil, problem.Forbidden
	}
	before, err := s.NativeHouseholdIdentityScope(ctx, ref)
	if err != nil {
		return nil, err
	}
	keys, ok := s.Vault.(port.NativeHouseholdPhotoKeys)
	if !ok {
		return nil, ErrDependency
	}
	key, err := keys.HouseholdPhotoKey(ctx, ref)
	if err != nil {
		return nil, vaultError(err)
	}
	after, err := s.NativeHouseholdIdentityScope(ctx, ref)
	if err != nil {
		return nil, err
	}
	if before.CooperativeRef != after.CooperativeRef || before.Revision != after.Revision || len(key) != 32 {
		return nil, ErrDependency
	}
	return key, nil
}
