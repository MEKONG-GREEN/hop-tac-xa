package store

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type HTXContactCommand struct {
	CommandRef, CooperativeRef uuid.UUID
	PayloadCT, WrappedKey      []byte
	KEKID                      string
}

func HTXContactCommandByRef(ctx context.Context, tx pgx.Tx, command uuid.UUID) (*HTXContactCommand, error) {
	var out HTXContactCommand
	err := tx.QueryRow(ctx, `SELECT command_ref,cooperative_ref,payload_ct,wrapped_key,kek_id FROM vault.htx_contact_command WHERE command_ref=$1`, command).Scan(&out.CommandRef, &out.CooperativeRef, &out.PayloadCT, &out.WrappedKey, &out.KEKID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &out, err
}

func HTXContactCurrent(ctx context.Context, tx pgx.Tx, ref uuid.UUID) (*HTXContactCommand, error) {
	var out HTXContactCommand
	err := tx.QueryRow(ctx, `SELECT v.command_ref,v.cooperative_ref,v.payload_ct,v.wrapped_key,v.kek_id FROM vault.htx_contact c JOIN vault.htx_contact_command v USING(tenant_id,command_ref) WHERE c.cooperative_ref=$1`, ref).Scan(&out.CommandRef, &out.CooperativeRef, &out.PayloadCT, &out.WrappedKey, &out.KEKID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &out, err
}

func LockHTXContact(ctx context.Context, tx pgx.Tx, ref uuid.UUID) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('vault.htx-contact:'||shared.current_tenant()::text||':'||$1::text,283))`, ref.String())
	return err
}

func LockHTXContactCommand(ctx context.Context, tx pgx.Tx, command uuid.UUID) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('vault.htx-command:'||shared.current_tenant()::text||':'||$1::text,283))`, command.String())
	return err
}

func ApplyHTXContact(ctx context.Context, tx pgx.Tx, row HTXContactCommand) error {
	if _, err := tx.Exec(ctx, `INSERT INTO vault.htx_contact_command(tenant_id,command_ref,cooperative_ref,payload_ct,wrapped_key,kek_id) VALUES(shared.current_tenant(),$1,$2,$3,$4,$5)`, row.CommandRef, row.CooperativeRef, row.PayloadCT, row.WrappedKey, row.KEKID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO vault.htx_contact(tenant_id,cooperative_ref,command_ref) VALUES(shared.current_tenant(),$1,$2) ON CONFLICT(tenant_id,cooperative_ref) DO UPDATE SET command_ref=excluded.command_ref`, row.CooperativeRef, row.CommandRef)
	return err
}
