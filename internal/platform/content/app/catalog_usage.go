package app

import (
	"context"
	"slices"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/content/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
)

// CatalogTagInUse cho biết mục danh mục có đang là nhãn của tài liệu (mọi phiên bản) không, cho
// masterdata/port.CatalogUsage (AH-2). Danh mục không dùng làm nhãn thì không có tham chiếu.
func (s *Service) CatalogTagInUse(ctx context.Context, catalog, code string) (bool, error) {
	if !slices.Contains(domain.TagCatalogs, catalog) {
		return false, nil
	}
	var used bool
	err := tenant.FromCtxTx(ctx, s.DB, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pf_content.document_version WHERE tags @> ARRAY[$1::text])`,
			catalog+":"+code).Scan(&used)
	})
	return used, err
}
