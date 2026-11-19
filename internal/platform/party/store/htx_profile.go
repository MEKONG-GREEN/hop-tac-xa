package store

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func HTXProfileCompleted(ctx context.Context, tx pgx.Tx, command uuid.UUID) (bool, error) {
	var done bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pf_party.htx_profile_completion WHERE command_ref=$1)`, command).Scan(&done)
	return done, err
}

func HTXProfilePending(ctx context.Context, tx pgx.Tx, ref uuid.UUID) (bool, error) {
	var pending bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pf_party.org_event e
WHERE e.org_ref=$1 AND e.action='profile_updated' AND NOT EXISTS
(SELECT 1 FROM pf_party.htx_profile_completion c WHERE c.tenant_id=e.tenant_id AND c.command_ref=e.id))`, ref).Scan(&pending)
	return pending, err
}

func CompleteHTXProfile(ctx context.Context, tx pgx.Tx, ref, command uuid.UUID) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_party.htx_profile_completion(tenant_id,command_ref,org_ref)
VALUES(shared.current_tenant(),$1,$2) ON CONFLICT DO NOTHING`, command, ref)
	return err
}
