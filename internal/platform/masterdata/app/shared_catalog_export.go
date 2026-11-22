package app

import (
	"bytes"
	"context"
	"slices"
	"sort"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/masterdata/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/masterdata/store"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tabular"
	"github.com/jackc/pgx/v5"
)

// SharedCatalogs là danh mục dùng chung Vật tư, Máy móc mà Đại diện HTX và Thành viên HTX được mở danh sách và
// xuất Excel chỉ đọc (quyết định VTKM-Q-07, AH-274). Không mở rộng sang các danh mục masterdata khác.
var SharedCatalogs = []string{"material", "machine_type"}

// maxSharedCatalogExport giới hạn số dòng một tệp; danh mục dùng chung hiện chỉ vài chục mục.
const maxSharedCatalogExport = 5000

var sharedCatalogTitles = map[string]string{"material": "Vật tư", "machine_type": "Máy móc"}

// Nhãn cột của thuộc tính; khóa không có nhãn giữ nguyên tên khóa.
var sharedAttrLabels = map[string]string{"group": "Nhóm", "unit": "Đơn vị tính", "farm_stage": "Giai đoạn"}

// ExportSharedCatalog sinh tệp .xlsx các mục đang dùng của một danh mục dùng chung (chỉ đọc).
func (s *Service) ExportSharedCatalog(ctx context.Context, catalog string) ([]byte, error) {
	p, err := authz.Require(ctx, htxroles.Admin, htxroles.Representative, htxroles.Member)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(SharedCatalogs, catalog) {
		return nil, ErrCatalogNotFound
	}
	if err := s.knownCatalog(catalog); err != nil {
		return nil, err
	}
	var items []domain.Item
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		items, e = store.Items(ctx, tx, catalog, false, "", maxSharedCatalogExport+1)
		return e
	})
	if err != nil {
		return nil, err
	}
	if len(items) > maxSharedCatalogExport {
		return nil, problem.Validation.WithDetail("Danh mục quá lớn để xuất một tệp")
	}
	return sharedCatalogXLSX(catalog, items)
}

func sharedCatalogXLSX(catalog string, items []domain.Item) ([]byte, error) {
	keys := map[string]bool{}
	for _, it := range items {
		for k := range it.Attrs {
			keys[k] = true
		}
	}
	attrs := make([]string, 0, len(keys))
	for k := range keys {
		attrs = append(attrs, k)
	}
	sort.Strings(attrs)
	header := []tabular.Cell{{Text: "Mã", Bold: true}, {Text: "Tên", Bold: true}}
	widths := []int{16, 40}
	for _, k := range attrs {
		label := sharedAttrLabels[k]
		if label == "" {
			label = k
		}
		header = append(header, tabular.Cell{Text: label, Bold: true})
		widths = append(widths, 18)
	}
	rows := [][]tabular.Cell{header}
	for _, it := range items {
		row := []tabular.Cell{{Text: it.Code}, {Text: it.Name}}
		for _, k := range attrs {
			row = append(row, tabular.Cell{Text: it.Attrs[k]})
		}
		rows = append(rows, row)
	}
	var buf bytes.Buffer
	sheet := tabular.Sheet{Name: sharedCatalogTitles[catalog], Rows: rows, Widths: widths, FreezeHeader: true}
	if err := tabular.WriteSheetsXLSX(&buf, []tabular.Sheet{sheet}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
