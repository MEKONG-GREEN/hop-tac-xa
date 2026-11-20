package store

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func HasHouseholdProduction(ctx context.Context, tx pgx.Tx, ref uuid.UUID) (bool, error) {
	var used bool
	err := tx.QueryRow(ctx, `SELECT pf_coop.household_has_production_history($1)`, ref).Scan(&used)
	return used, err
}
