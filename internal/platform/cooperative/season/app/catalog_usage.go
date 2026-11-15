package app

import (
	"context"
	"strings"

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

// SeasonInUse cho biết mùa vụ có đang được dữ liệu vụ tham chiếu không, cho masterdata/port.CatalogUsage (AH-7):
// có sự việc vụ (kể cả vụ đã hủy) hoặc khai báo sản lượng. So mã không phân biệt hoa thường vì vụ cũ có thể lưu mã thường.
func (s *Service) SeasonInUse(ctx context.Context, code string) (bool, error) {
	var used bool
	err := tenant.FromCtxTx(ctx, s.DB, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pf_coop.season_event WHERE upper(season_code) = upper($1::text))
		    OR EXISTS (SELECT 1 FROM pf_coop.harvest_declaration WHERE upper(season_code) = upper($1::text))`, code).Scan(&used)
	})
	return used, err
}

// SeasonCounts đếm số vụ đang dùng mỗi mùa vụ (cột "Đang dùng" của màn Danh mục mùa vụ, AH-7). Mọi trạng thái vụ,
// kể cả đã hủy, để khớp với cổng xóa của SeasonInUse. Mã không có vụ thì vắng trong kết quả; khóa là mã viết HOA.
func (s *Service) SeasonCounts(ctx context.Context, codes []string) (map[string]int, error) {
	out := make(map[string]int, len(codes))
	err := tenant.FromCtxTx(ctx, s.DB, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT upper(season_code), count(*) FROM pf_coop.season_current
		    WHERE upper(season_code) = ANY($1::text[]) GROUP BY 1`, upperAll(codes))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var code string
			var n int
			if err := rows.Scan(&code, &n); err != nil {
				return err
			}
			out[code] = n
		}
		return rows.Err()
	})
	return out, err
}

func upperAll(codes []string) []string {
	out := make([]string, len(codes))
	for i, c := range codes {
		out[i] = strings.ToUpper(c)
	}
	return out
}
