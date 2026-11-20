package app

import (
	"context"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/member/store"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (s *Service) CooperativePrivateReferences(ctx context.Context, ref uuid.UUID) (bool, error) {
	p, err := authz.Require(ctx, authz.RolePlatformAdmin)
	if err != nil {
		return false, err
	}
	ctx = authz.WithPurpose(ctx, "registry.cooperative_delete")
	var used bool
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		used, e = store.CooperativePrivateReferences(ctx, tx, ref)
		return e
	})
	return used, err
}
