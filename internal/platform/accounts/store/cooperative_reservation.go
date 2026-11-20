package store

import (
	"context"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func ReserveCooperativeIdentity(ctx context.Context, tx pgx.Tx, operation, org uuid.UUID, key, digest string) (uuid.UUID, error) {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('accounts.identity-reserve:'||shared.current_tenant()::text||':'||$1,283))`, key); err != nil {
		return uuid.Nil, err
	}
	rows, err := tx.Query(ctx, `SELECT cooperative_ref,request_hash FROM pf_accounts.cooperative_identity_reservation WHERE operation_key=$1 LIMIT 1`, key)
	if err != nil {
		return uuid.Nil, err
	}
	if rows.Next() {
		var previous uuid.UUID
		var body string
		if err := rows.Scan(&previous, &body); err != nil {
			rows.Close()
			return uuid.Nil, err
		}
		if previous != org || body != digest {
			rows.Close()
			return uuid.Nil, problem.IdempotencyConflict
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return uuid.Nil, err
	}
	// Mỗi lần I/O có reservation riêng, kể cả hai attempt cùng Idempotency-Key.
	// Bù một attempt không được mở khóa xóa khi attempt kia còn đang chạy.
	if _, err := tx.Exec(ctx, `INSERT INTO pf_accounts.cooperative_identity_reservation(tenant_id,operation_ref,cooperative_ref,operation_key,request_hash)
VALUES(shared.current_tenant(),$1,$2,$3,$4)`, operation, org, key, digest); err != nil {
		return uuid.Nil, err
	}
	return operation, nil
}

func ReleaseCooperativeIdentity(ctx context.Context, tx pgx.Tx, operation, org uuid.UUID) error {
	_, err := tx.Exec(ctx, `UPDATE pf_accounts.cooperative_identity_reservation SET released_at=now() WHERE operation_ref=$1 AND cooperative_ref=$2 AND released_at IS NULL`, operation, org)
	return err
}
