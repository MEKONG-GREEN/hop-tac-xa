package store

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func NativeHouseholdCommand(ctx context.Context, tx pgx.Tx, command uuid.UUID) (uuid.UUID, []byte, error) {
	var ref uuid.UUID
	var digest []byte
	err := tx.QueryRow(ctx, `SELECT household_ref,digest FROM vault.native_household_command WHERE command_ref=$1`, command).Scan(&ref, &digest)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, nil, nil
	}
	return ref, digest, err
}
func NativeHouseholdRevision(ctx context.Context, tx pgx.Tx, ref uuid.UUID) (int64, error) {
	var revision int64
	err := tx.QueryRow(ctx, `SELECT revision FROM vault.native_household_head WHERE household_ref=$1`, ref).Scan(&revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return 1, nil
	}
	return revision, err
}
func CompleteNativeHouseholdCommand(ctx context.Context, tx pgx.Tx, tenant, command, ref uuid.UUID, revision int64, digest []byte) error {
	if _, err := tx.Exec(ctx, `INSERT INTO vault.native_household_command(tenant_id,command_ref,household_ref,digest) VALUES($1,$2,$3,$4)`, tenant, command, ref, digest); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO vault.native_household_head(tenant_id,household_ref,revision) VALUES($1,$2,$3)
 ON CONFLICT(tenant_id,household_ref) DO UPDATE SET revision=EXCLUDED.revision`, tenant, ref, revision)
	return err
}
func UpdateNativeHouseholdIdentity(ctx context.Context, tx pgx.Tx, ref uuid.UUID, id Identity, areaCode string) error {
	if _, err := tx.Exec(ctx, `UPDATE vault.subject SET area_code=$2 WHERE ref=$1`, ref, areaCode); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE vault.identity SET full_name_ct=$2,phone_ct=$3,national_id_ct=$4,address_ct=$5,updated_at=now() WHERE subject_ref=$1`, ref, id.FullNameCT, id.PhoneCT, id.NationalIDCT, id.AddressCT)
	return err
}
