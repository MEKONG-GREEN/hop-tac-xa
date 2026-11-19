package store

import (
	"context"

	"github.com/jackc/pgx/v5"
)

func CooperativeCountsByUnits(ctx context.Context, tx pgx.Tx, codes []string) (map[string]int64, error) {
	rows, err := tx.Query(ctx, `SELECT admin_unit_code,count(*) FROM pf_party.org
 WHERE kind='cooperative' AND admin_unit_code=ANY($1::text[]) GROUP BY admin_unit_code`, codes)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := map[string]int64{}
	for rows.Next() {
		var code string
		var n int64
		if err := rows.Scan(&code, &n); err != nil {
			return nil, err
		}
		counts[code] = n
	}
	return counts, rows.Err()
}
