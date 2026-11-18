package store

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

// ActiveHouseholdArea totals every open season of the household, including
// seasons that started before a membership transfer. The caller must hold
// LockHousehold before using this total to authorize a write.
func ActiveHouseholdArea(ctx context.Context, tx pgx.Tx, ref uuid.UUID) (decimal.Decimal, error) {
	var raw string
	if err := tx.QueryRow(ctx, `SELECT coalesce(sum(area_ha), 0)::text
	  FROM pf_coop.season_current WHERE household_ref = $1 AND status = 'open'`, ref).Scan(&raw); err != nil {
		return decimal.Zero, err
	}
	return decimal.NewFromString(raw)
}

// ActiveAreas reads the current projection, not the append-only events. Both
// households and seasons belong to pf/cooperative; no foreign module tables are read.
func ActiveAreas(ctx context.Context, tx pgx.Tx, coop uuid.UUID, refs []uuid.UUID) (map[uuid.UUID]decimal.Decimal, error) {
	out := make(map[uuid.UUID]decimal.Decimal, len(refs))
	if len(refs) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, `SELECT household_ref, sum(area_ha)::text
	  FROM pf_coop.season_current
	  WHERE cooperative_ref = $1 AND household_ref = ANY($2::uuid[]) AND status = 'open'
	  GROUP BY household_ref`, coop, uuidStrings(refs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var ref uuid.UUID
		var raw string
		if err := rows.Scan(&ref, &raw); err != nil {
			return nil, err
		}
		area, err := decimal.NewFromString(raw)
		if err != nil {
			return nil, err
		}
		out[ref] = area
	}
	return out, rows.Err()
}
