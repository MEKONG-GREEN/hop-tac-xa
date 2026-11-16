package app

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
)

// SeasonInUse cho biết mùa vụ có đang được dữ liệu cơ giới hóa tham chiếu không, cho masterdata/port.CatalogUsage
// (AH-7): diện tích vụ (kể cả sự việc cũ), lần cân đối, dự báo hoặc nhật ký đồng bộ diện tích. Chỉ đọc; không đổi cách
// tính cân đối. Tenant từ context; so mã không phân biệt hoa thường.
func (s *Service) SeasonInUse(ctx context.Context, code string) (bool, error) {
	var used bool
	err := tenant.FromCtxTx(ctx, s.DB, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pf_mechanization.season_area_event WHERE upper(season_code) = upper($1::text))
		    OR EXISTS (SELECT 1 FROM pf_mechanization.balance_run WHERE upper(season_code) = upper($1::text))
		    OR EXISTS (SELECT 1 FROM pf_mechanization.forecast_run WHERE upper(season_code) = upper($1::text))
		    OR EXISTS (SELECT 1 FROM pf_mechanization.area_sync_log WHERE upper(season_code) = upper($1::text))`, code).Scan(&used)
	})
	return used, err
}
