package app

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/masterdata/store"
)

func (s *Service) RebuildUnitProfiles(ctx context.Context, tenantID uuid.UUID) error {
	return s.tx(ctx, tenantID, func(tx pgx.Tx) error {
		if err := store.LockImport(ctx, tx, tenantID); err != nil {
			return err
		}
		return store.RebuildUnitProfiles(ctx, tx)
	})
}
