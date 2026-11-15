package store

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Truy vấn riêng của loại cây và giống (AH-2). Mục đã xóa không tham gia kiểm trùng tên và không được đếm.

// LockCatalogScope tuần tự hóa việc kiểm trùng tên và đổi trạng thái trong cùng một phạm vi (loại cây: "crop";
// giống: "crop:<mã loại cây>"), để hai request song song không cùng lọt qua kiểm tra.
func LockCatalogScope(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, scope string) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('pf_masterdata.catalog_scope:' || $1::text || ':' || $2::text, 0))",
		tenantID.String(), scope)
	return err
}

func names(rows pgx.Rows, err error) ([]string, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// CropTypeNames lấy tên các loại cây (mục crop không có parent_code, kể cả đã ẩn) trừ mục mã exceptCode.
func CropTypeNames(ctx context.Context, tx pgx.Tx, exceptCode string) ([]string, error) {
	return names(tx.Query(ctx, `SELECT name FROM pf_masterdata.catalog_item
	    WHERE catalog = 'crop' AND code <> $1 AND deleted_at IS NULL AND NOT (attrs ? 'parent_code')`, exceptCode))
}

// VarietyNames lấy tên các giống (kể cả đã ẩn) của một loại cây, trừ mục mã exceptCode.
func VarietyNames(ctx context.Context, tx pgx.Tx, cropCode, exceptCode string) ([]string, error) {
	return names(tx.Query(ctx, `SELECT name FROM pf_masterdata.catalog_item
	    WHERE catalog = 'rice_variety' AND code <> $1 AND deleted_at IS NULL AND attrs->>'crop_code' = $2`, exceptCode, cropCode))
}

// CountVarieties đếm giống của một loại cây; visibleOnly: chỉ giống đang hiện.
func CountVarieties(ctx context.Context, tx pgx.Tx, cropCode string, visibleOnly bool) (int, error) {
	var n int
	err := tx.QueryRow(ctx, `SELECT count(*) FROM pf_masterdata.catalog_item
	    WHERE catalog = 'rice_variety' AND deleted_at IS NULL AND attrs->>'crop_code' = $1 AND ($2::boolean = false OR active)`,
		cropCode, visibleOnly).Scan(&n)
	return n, err
}

// PestsAffectingCrop cho biết có mục sâu bệnh (chưa xóa) nêu cây trồng này ở "cây trồng bị ảnh hưởng".
func PestsAffectingCrop(ctx context.Context, tx pgx.Tx, cropCode string) (bool, error) {
	var ok bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pf_masterdata.catalog_item
	    WHERE catalog = 'pest' AND deleted_at IS NULL AND attrs ? 'affected_crops'
	      AND string_to_array(attrs->>'affected_crops', ',') @> ARRAY[$1::text])`, cropCode).Scan(&ok)
	return ok, err
}
