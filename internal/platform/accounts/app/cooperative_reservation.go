package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/store"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Ghi reservation trước I/O, hoàn tất cùng transaction sổ tài khoản. Khi IAM
// trả lỗi không biết đã tạo chưa, giữ reservation để xóa HTX fail-closed.
func (s *Service) reserveCooperativeIdentity(ctx context.Context, a *actor, org uuid.UUID, key string, input any) (uuid.UUID, error) {
	body, err := json.Marshal(input)
	if err != nil {
		return uuid.Nil, err
	}
	defer clear(body)
	requestHash := sha256.Sum256(body)
	operationKey := sha256.Sum256([]byte(a.p.ActorRef.String() + ":" + key))
	var operation uuid.UUID
	err = tenant.WithTenantTx(ctx, s.DB, a.p.TenantID, func(tx pgx.Tx) error {
		var e error
		operation, e = store.ReserveCooperativeIdentity(ctx, tx, ids.New(), org, hex.EncodeToString(operationKey[:]), hex.EncodeToString(requestHash[:]))
		return e
	})
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.ConstraintName == "cooperative_permanently_deleted" {
		return uuid.Nil, ErrOrgInactive.WithDetail("HTX đã được xóa")
	}
	return operation, err
}

func (s *Service) releaseCooperativeIdentity(ctx context.Context, a *actor, operation, org uuid.UUID) error {
	return tenant.WithTenantTx(ctx, s.DB, a.p.TenantID, func(tx pgx.Tx) error { return store.ReleaseCooperativeIdentity(ctx, tx, operation, org) })
}
