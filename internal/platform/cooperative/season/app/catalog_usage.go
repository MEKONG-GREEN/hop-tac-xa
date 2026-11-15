package app

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
)

// VarietyInUse cho biết giống có đang được dữ liệu vụ tham chiếu không, cho masterdata/port.CatalogUsage (AH-2, BRD FN-01
// BR-02, EX-03): có sự việc vụ (kể cả vụ đã hủy), khai báo sản lượng hoặc gắn quy trình SOP (kể cả lần gắn đã được thay).
func (s *Service) VarietyInUse(ctx context.Context, code string) (bool, error) {
	var used bool
	err := tenant.FromCtxTx(ctx, s.DB, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pf_coop.season_event WHERE variety_code = $1::text)
		    OR EXISTS (SELECT 1 FROM pf_coop.harvest_declaration WHERE variety_code = $1::text)
		    OR EXISTS (SELECT 1 FROM pf_coop.variety_sop_binding WHERE variety_code = $1::text)`, code).Scan(&used)
	})
	return used, err
}
