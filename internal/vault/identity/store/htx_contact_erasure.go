package store

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func HTXContactErased(ctx context.Context, tx pgx.Tx, ref uuid.UUID) (bool, error) {
	var erased bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM vault.htx_contact_erasure WHERE cooperative_ref=$1)`, ref).Scan(&erased)
	return erased, err
}

func HTXErasureCommandScope(ctx context.Context, tx pgx.Tx, command uuid.UUID) (uuid.UUID, error) {
	var ref uuid.UUID
	err := tx.QueryRow(ctx, `SELECT coalesce((SELECT cooperative_ref FROM vault.htx_contact_erasure WHERE command_ref=$1),'00000000-0000-0000-0000-000000000000'::uuid)`, command).Scan(&ref)
	return ref, err
}

func EraseHTXContact(ctx context.Context, tx pgx.Tx, ref, command, actor uuid.UUID) error {
	if _, err := tx.Exec(ctx, `INSERT INTO vault.htx_contact_erasure(tenant_id,cooperative_ref,command_ref,erased_by) VALUES(shared.current_tenant(),$1,$2,$3)`, ref, command, actor); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM vault.htx_contact WHERE cooperative_ref=$1`, ref); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `DELETE FROM vault.htx_contact_command WHERE cooperative_ref=$1`, ref)
	return err
}
