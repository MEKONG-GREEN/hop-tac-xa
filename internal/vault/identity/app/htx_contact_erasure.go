package app

import (
	"context"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/vault/identity/client"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/vault/identity/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/vault/identity/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (s *Service) ValidateHTXContactErasure(ctx context.Context, purpose client.Purpose, ref uuid.UUID) error {
	if _, err := authz.Require(ctx, authz.RolePlatformAdmin); err != nil {
		return err
	}
	p, err := htxContactCaller(ctx, purpose, ref, true, false)
	if err != nil {
		return err
	}
	return tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error { _, e := store.HTXContactErased(ctx, tx, ref); return e })
}

func (s *Service) EraseHTXContact(ctx context.Context, purpose client.Purpose, ref, command uuid.UUID) error {
	if _, err := authz.Require(ctx, authz.RolePlatformAdmin); err != nil {
		return err
	}
	p, err := htxContactCaller(ctx, purpose, ref, true, false)
	if err != nil {
		return err
	}
	if command == uuid.Nil || command.Version() != 7 {
		return problem.Validation.WithField("command", "Cần UUIDv7 của thao tác xóa")
	}
	return tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		if err := store.LockHTXContactCommand(ctx, tx, command); err != nil {
			return err
		}
		if err := store.LockHTXContact(ctx, tx, ref); err != nil {
			return err
		}
		oldScope, err := store.HTXErasureCommandScope(ctx, tx, command)
		if err != nil {
			return err
		}
		if oldScope != uuid.Nil && oldScope != ref {
			return problem.IdempotencyConflict
		}
		erased, err := store.HTXContactErased(ctx, tx, ref)
		if err != nil {
			return err
		}
		if erased {
			return nil
		}
		if err := store.EraseHTXContact(ctx, tx, ref, command, p.ActorRef); err != nil {
			return err
		}
		return s.logAccess(ctx, tx, p, &ref, true, domain.OpErase, purpose, domain.OutcomeOK)
	})
}

// Mở bản mã ngoài transaction, rồi kiểm lại erasure dưới khóa trước khi trả
// dữ liệu. Audit lỗi nghiệp vụ vẫn commit; không trả PII nếu erasure đã thắng.
func (s *Service) logHTXContactRead(ctx context.Context, p authz.Principal, ref uuid.UUID, op string, purpose client.Purpose) error {
	var erased bool
	err := tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		if err := store.LockHTXContact(ctx, tx, ref); err != nil {
			return err
		}
		var e error
		erased, e = store.HTXContactErased(ctx, tx, ref)
		if e != nil {
			return e
		}
		outcome := domain.OutcomeOK
		if erased {
			outcome = domain.OutcomeErased
		}
		return s.logAccess(ctx, tx, p, &ref, true, op, purpose, outcome)
	})
	if err != nil {
		return err
	}
	if erased {
		return client.ProblemErased
	}
	return nil
}
