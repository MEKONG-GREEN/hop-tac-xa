package store

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// CreatedForPurpose kiểm nguồn tạo từ sổ bất biến, không suy từ lần lookup hoặc
// lần tạo bị trùng. Dùng để registry.admin không sửa định danh nhân sự/chủ hộ.
func CreatedForPurpose(ctx context.Context, tx pgx.Tx, ref uuid.UUID, purpose string) (bool, error) {
	var ok bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM vault.access_log
WHERE subject_ref = $1 AND op = 'create' AND outcome = 'ok' AND purpose = $2)`, ref, purpose).Scan(&ok)
	return ok, err
}
