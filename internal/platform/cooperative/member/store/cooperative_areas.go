package store

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

// CooperativeAreas đọc một snapshot duy nhất từ membership đang mở. NULL không
// bị sum() âm thầm bỏ qua: trả số hộ thiếu để app không công bố tổng sai.
type CooperativeAreas struct {
	Cultivated, Livestock                           decimal.Decimal
	Households, MissingCultivated, MissingLivestock int64
}

func AreasByCooperatives(ctx context.Context, tx pgx.Tx, refs []uuid.UUID) (map[uuid.UUID]CooperativeAreas, error) {
	out := make(map[uuid.UUID]CooperativeAreas, len(refs))
	rows, err := tx.Query(ctx, `SELECT m.org_ref, count(*),
  count(*) FILTER (WHERE h.cultivated_area_ha IS NULL),
  count(*) FILTER (WHERE h.livestock_area_ha IS NULL),
  coalesce(sum(h.cultivated_area_ha), 0)::text,
  coalesce(sum(h.livestock_area_ha), 0)::text
FROM pf_coop.household_membership m
JOIN pf_coop.household h ON h.tenant_id = m.tenant_id AND h.household_ref = m.household_ref
WHERE m.until_date IS NULL AND m.org_ref = ANY($1::text[]::uuid[])
GROUP BY m.org_ref`, uuidStrings(refs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var ref uuid.UUID
		var a CooperativeAreas
		if err := rows.Scan(&ref, &a.Households, &a.MissingCultivated, &a.MissingLivestock, &a.Cultivated, &a.Livestock); err != nil {
			return nil, err
		}
		out[ref] = a
	}
	return out, rows.Err()
}
