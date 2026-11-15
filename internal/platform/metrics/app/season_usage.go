package app

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
)

// SeasonInUse cho biết mùa vụ có đang được số liệu thống kê tham chiếu không, cho masterdata/port.CatalogUsage (AH-7):
// có giá trị chỉ số theo ngày với chiều vụ "MÃ-NĂM" (port.SeasonDimension, mã viết HOA). Đọc sổ ở primary, không đọc
// bản sao: quyết định xóa không được dựa trên dữ liệu trễ.
func (s *Service) SeasonInUse(ctx context.Context, code string) (bool, error) {
	var used bool
	err := tenant.FromCtxTx(ctx, s.DB, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pf_metrics.daily_value
		    WHERE left(dimension, length($1::text) + 1) = upper($1::text) || '-'
		      AND substr(dimension, length($1::text) + 2) ~ '^[0-9]{4}$')`, code).Scan(&used)
	})
	return used, err
}
