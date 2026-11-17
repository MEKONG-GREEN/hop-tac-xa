package store

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

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
