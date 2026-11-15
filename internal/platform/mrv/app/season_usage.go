package app

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
)

// SeasonInUse cho biết mùa vụ có đang được dữ liệu MRV tham chiếu không, cho masterdata/port.CatalogUsage (AH-7):
// bằng chứng, liên kết vụ hoặc bản ước tính. Tenant từ context; so mã không phân biệt hoa thường.
func (s *Service) SeasonInUse(ctx context.Context, code string) (bool, error) {
	var used bool
	err := tenant.FromCtxTx(ctx, s.DB, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pf_mrv.evidence WHERE upper(season_code) = upper($1::text))
		    OR EXISTS (SELECT 1 FROM pf_mrv.season_link WHERE upper(season_code) = upper($1::text))
		    OR EXISTS (SELECT 1 FROM pf_mrv.estimate WHERE upper(season_code) = upper($1::text))`, code).Scan(&used)
	})
	return used, err
}
