package store

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func CooperativePrivateReferences(ctx context.Context, tx pgx.Tx, ref uuid.UUID) (bool, error) {
	var used bool
	err := tx.QueryRow(ctx, `SELECT pf_coop.cooperative_has_private_references($1)`, ref).Scan(&used)
	return used, err
}
