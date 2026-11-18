package store

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// HouseholdSOPTemplates returns active, published templates in the caller's
// tenant. Scope and crop/variety are checked again after locking the selection.
func HouseholdSOPTemplates(ctx context.Context, tx pgx.Tx, coop uuid.UUID, crop, variety string) ([]Template, error) {
	rows, err := tx.Query(ctx, "SELECT "+templateCols+` FROM pf_coop.sop_template
	 WHERE deleted_at IS NULL AND status='published' AND published_version IS NOT NULL
	 AND crop=$1 AND (variety_code IS NULL OR variety_code='' OR variety_code=$2)
	 AND (scope='system' OR (scope='htx' AND cooperative_ref=$3)) ORDER BY ref`, crop, variety, coop)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Template
	for rows.Next() {
		t, err := scanTemplate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// HouseholdSeasons deliberately excludes the legacy plot DTO and its required
// plot/forecast fields. The API checks the requested cooperative before here.
func HouseholdSeasons(ctx context.Context, tx pgx.Tx, coop, hh, after uuid.UUID, status string, limit int) ([]Season, error) {
	rows, err := tx.Query(ctx, "SELECT "+seasonCols+` FROM pf_coop.season_current s
	 WHERE s.entry_kind='household' AND s.cooperative_ref=$1 AND s.household_ref=$2
	 AND s.season_ref>$3 AND ($4::text='' OR s.status=$4) ORDER BY s.season_ref LIMIT $5`, coop, hh, after, status, limit)
	if err != nil {
		return nil, err
	}
	return collectSeasons(rows)
}
