package app

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/party/store"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
)

func (r *Registry) CooperativeCountsByUnits(ctx context.Context, codes []string) (map[string]int64, error) {
	if _, err := authz.Require(ctx, authz.RolePlatformAdmin); err != nil {
		return nil, err
	}
	if len(codes) == 0 {
		return map[string]int64{}, nil
	}
	var counts map[string]int64
	err := tenant.FromCtxTx(ctx, r.DB, func(tx pgx.Tx) error {
		var err error
		counts, err = store.CooperativeCountsByUnits(ctx, tx, codes)
		return err
	})
	return counts, err
}
