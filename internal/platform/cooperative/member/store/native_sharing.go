package store

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/member/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const sharingColumns = `ref,requester_cooperative_ref,owner_cooperative_ref,state,revision,requested_at,processed_at,actor_ref`

func scanSharing(row pgx.Row) (*domain.NativeSharing, error) {
	var r domain.NativeSharing
	err := row.Scan(&r.Ref, &r.Requester, &r.Owner, &r.State, &r.Revision, &r.RequestedAt, &r.ProcessedAt, &r.ActorRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &r, err
}
func SharingByRef(ctx context.Context, tx pgx.Tx, ref uuid.UUID, lock bool) (*domain.NativeSharing, error) {
	suffix := ""
	if lock {
		suffix = " FOR UPDATE"
	}
	return scanSharing(tx.QueryRow(ctx, `SELECT `+sharingColumns+` FROM pf_coop.native_sharing WHERE ref=$1`+suffix, ref))
}
func SharingList(ctx context.Context, tx pgx.Tx, coop uuid.UUID) ([]domain.NativeSharing, error) {
	rows, err := tx.Query(ctx, `SELECT `+sharingColumns+` FROM pf_coop.native_sharing WHERE requester_cooperative_ref=$1 OR owner_cooperative_ref=$1 ORDER BY requested_at DESC,ref DESC`, coop)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.NativeSharing{}
	for rows.Next() {
		r, e := scanSharing(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}
func InsertSharing(ctx context.Context, tx pgx.Tx, tid uuid.UUID, r domain.NativeSharing) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_coop.native_sharing(tenant_id,`+sharingColumns+`) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, tid, r.Ref, r.Requester, r.Owner, r.State, r.Revision, r.RequestedAt, r.ProcessedAt, r.ActorRef)
	return err
}
func UpdateSharing(ctx context.Context, tx pgx.Tx, r domain.NativeSharing) error {
	_, err := tx.Exec(ctx, `UPDATE pf_coop.native_sharing SET state=$2,revision=$3,processed_at=$4,actor_ref=$5 WHERE ref=$1`, r.Ref, r.State, r.Revision, r.ProcessedAt, r.ActorRef)
	return err
}
func InsertSharingEvent(ctx context.Context, tx pgx.Tx, tid uuid.UUID, r domain.NativeSharing, action, key string) error {
	when := r.RequestedAt
	if r.ProcessedAt != nil {
		when = *r.ProcessedAt
	}
	_, err := tx.Exec(ctx, `INSERT INTO pf_coop.native_sharing_event(id,tenant_id,stream_key,request_ref,requester_cooperative_ref,owner_cooperative_ref,action,state,event_time,actor_ref,idem_key,rule_version)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,'FN-09.v1')`, ids.New(), tid, "native_sharing:"+r.Ref.String(), r.Ref, r.Requester, r.Owner, action, r.State, when, r.ActorRef, key)
	return err
}

type SharingOperation struct {
	RequestRef       uuid.UUID
	Action           string
	Revision         int64
	Requester, Owner uuid.UUID
	Response         json.RawMessage
}

func SharingReplay(ctx context.Context, tx pgx.Tx, actor uuid.UUID, key string) (*SharingOperation, error) {
	// Serializes identical actor/key operations before either calls the ledger.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended(shared.current_tenant()::text||$1||$2,278))`, actor.String(), key); err != nil {
		return nil, err
	}
	var r SharingOperation
	err := tx.QueryRow(ctx, `SELECT request_ref,action,expected_revision,requester_cooperative_ref,owner_cooperative_ref,response FROM pf_coop.native_sharing_operation WHERE actor_ref=$1 AND idem_key=$2`, actor, key).Scan(&r.RequestRef, &r.Action, &r.Revision, &r.Requester, &r.Owner, &r.Response)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &r, err
}
func SaveSharingReplay(ctx context.Context, tx pgx.Tx, tid, actor uuid.UUID, key string, r SharingOperation) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_coop.native_sharing_operation(tenant_id,actor_ref,idem_key,request_ref,action,expected_revision,requester_cooperative_ref,owner_cooperative_ref,response)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb)`, tid, actor, key, r.RequestRef, r.Action, r.Revision, r.Requester, r.Owner, string(r.Response))
	return err
}
