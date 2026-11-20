package store

import (
	"bytes"
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
)

type NativeCreation struct {
	ID, UserRef uuid.UUID
	State       string
}

// ClaimNativeCreation serializes a logical request before any IAM operation.
func ClaimNativeCreation(ctx context.Context, tx pgx.Tx, id, actor uuid.UUID, operation, digest []byte, ledgerKey string) (NativeCreation, bool, error) {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended(encode($1::bytea,'hex'),275))`, operation); err != nil {
		return NativeCreation{}, false, err
	}
	var c NativeCreation
	var previous []byte
	var user *uuid.UUID
	err := tx.QueryRow(ctx, `SELECT id,user_ref,state,request_digest FROM pf_accounts.native_creation WHERE operation_key=$1 FOR UPDATE`, operation).Scan(&c.ID, &user, &c.State, &previous)
	if err == nil {
		if !bytes.Equal(digest, previous) {
			return c, false, problem.IdempotencyConflict
		}
		if user != nil {
			c.UserRef = *user
		}
		if c.State == "compensated" {
			previousEvent, err := EventByIdemKey(ctx, tx, ledgerKey)
			if err != nil {
				return c, false, err
			}
			if previousEvent != nil {
				return c, false, problem.IdempotencyConflict
			}
			_, err = tx.Exec(ctx, `UPDATE pf_accounts.native_creation SET state='pending',created_at=now() WHERE id=$1 AND state='compensated'`, c.ID)
			c.State = "pending"
			return c, err == nil, err
		}
		return c, false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return c, false, err
	}
	previousEvent, err := EventByIdemKey(ctx, tx, ledgerKey)
	if err != nil {
		return c, false, err
	}
	if previousEvent != nil {
		return c, false, problem.IdempotencyConflict
	}
	_, err = tx.Exec(ctx, `INSERT INTO pf_accounts.native_creation(tenant_id,id,operation_key,request_digest,actor_ref) VALUES(shared.current_tenant(),$1,$2,$3,$4)`, id, operation, digest, actor)
	return NativeCreation{ID: id, State: "pending"}, err == nil, err
}

func IsNativeCreationBusy(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505" && pg.ConstraintName == "native_creation_inflight"
}

func FinishNativeCreation(ctx context.Context, tx pgx.Tx, id, user uuid.UUID, compensated bool) error {
	state := "complete"
	var ref *uuid.UUID
	if compensated {
		state = "compensated"
	} else {
		ref = &user
	}
	tag, err := tx.Exec(ctx, `UPDATE pf_accounts.native_creation SET state=$2,user_ref=$3 WHERE id=$1 AND state='pending'`, id, state, ref)
	if err == nil && tag.RowsAffected() != 1 {
		return problem.Conflict
	}
	return err
}
