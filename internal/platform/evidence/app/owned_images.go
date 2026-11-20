package app

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/evidence/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/evidence/store"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/envelope"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
)

// This receipt check returns no file metadata, URL or plaintext. It does not
// grant evidence-viewer/Unwrap rights or select GPS/captured_at values.
func (s *Service) VerifyOwnedImages(ctx context.Context, subject uuid.UUID, refs []uuid.UUID, maxPlainBytes int64) error {
	p, ok := authz.FromContext(ctx)
	if !ok || p.ActorRef == uuid.Nil || p.TenantID == uuid.Nil {
		return problem.Unauthenticated
	}
	if subject == uuid.Nil || len(refs) > 20 || maxPlainBytes < 1 || maxPlainBytes > port.MaxFileBytes-envelope.TagSize {
		return problem.Validation.WithField("file_refs", "Thông tin ảnh không hợp lệ")
	}
	if len(refs) == 0 {
		return nil
	}
	if s.DB == nil {
		return ErrDependency
	}
	return tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		for _, ref := range refs {
			valid, err := store.OwnedImageReceipt(ctx, tx, ref, subject, p.ActorRef, maxPlainBytes+envelope.TagSize)
			if err != nil {
				return err
			}
			if !valid {
				return problem.Validation.WithField("file_refs", "Ảnh chưa được tải lên, không thuộc người ghi/hộ hoặc vượt giới hạn")
			}
		}
		return nil
	})
}

var _ port.OwnedImages = (*Service)(nil)
