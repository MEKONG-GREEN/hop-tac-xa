package store

import (
	"context"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/envelope"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func OwnedImageReceipt(ctx context.Context, tx pgx.Tx, ref, subject, actor uuid.UUID, maxCipherBytes int64) (bool, error) {
	var valid bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pf_files.file
 WHERE file_ref=$1 AND subject_ref=$2 AND uploaded_by=$3
 AND content_type IN ('image/jpeg','image/png','image/webp')
 AND size_bytes>$4 AND size_bytes<=$5 AND captured_at IS NOT NULL)`, ref, subject, actor, envelope.TagSize, maxCipherBytes).Scan(&valid)
	return valid, err
}
