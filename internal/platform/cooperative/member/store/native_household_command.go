package store

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"time"
)

type NativeHouseholdCommand struct {
	Ref, HouseholdRef, ActorRef uuid.UUID
	CooperativeRef              *uuid.UUID
	Operation, IdemKey          string
	ExpectedRevision            int64
	Request                     []byte
	CompletedAt                 *time.Time
	FailedCode                  *string
}

const nativeCommandCols = `command_ref,household_ref,cooperative_ref,actor_ref,operation,idem_key,expected_revision,request,completed_at,failed_code`

func scanNativeCommand(row pgx.Row) (*NativeHouseholdCommand, error) {
	var c NativeHouseholdCommand
	err := row.Scan(&c.Ref, &c.HouseholdRef, &c.CooperativeRef, &c.ActorRef, &c.Operation, &c.IdemKey, &c.ExpectedRevision, &c.Request, &c.CompletedAt, &c.FailedCode)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &c, err
}
func NativeCommandByKey(ctx context.Context, tx pgx.Tx, actor uuid.UUID, key string) (*NativeHouseholdCommand, error) {
	return scanNativeCommand(tx.QueryRow(ctx, `SELECT `+nativeCommandCols+` FROM pf_coop.native_household_command WHERE actor_ref=$1 AND idem_key=$2`, actor, key))
}
func NativeCommandByRef(ctx context.Context, tx pgx.Tx, ref uuid.UUID) (*NativeHouseholdCommand, error) {
	return scanNativeCommand(tx.QueryRow(ctx, `SELECT `+nativeCommandCols+` FROM pf_coop.native_household_command WHERE command_ref=$1`, ref))
}
func InsertNativeCommand(ctx context.Context, tx pgx.Tx, tenant uuid.UUID, c NativeHouseholdCommand) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_coop.native_household_command(tenant_id,command_ref,household_ref,cooperative_ref,actor_ref,operation,idem_key,expected_revision,request)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb)`, tenant, c.Ref, c.HouseholdRef, c.CooperativeRef, c.ActorRef, c.Operation, c.IdemKey, c.ExpectedRevision, string(c.Request))
	return err
}
func FinishNativeCommand(ctx context.Context, tx pgx.Tx, ref uuid.UUID) error {
	_, err := tx.Exec(ctx, `UPDATE pf_coop.native_household_command SET completed_at=now() WHERE command_ref=$1`, ref)
	return err
}
func FailNativeCommand(ctx context.Context, tx pgx.Tx, ref uuid.UUID, code string) error {
	_, err := tx.Exec(ctx, `UPDATE pf_coop.native_household_command SET failed_code=$2,completed_at=now(),cooperative_ref=NULL WHERE command_ref=$1 AND completed_at IS NULL`, ref, code)
	return err
}
func UpdateNativeHousehold(ctx context.Context, tx pgx.Tx, ref uuid.UUID, commune, area string, livestock *string, active, deleted bool, revision int64) error {
	_, err := tx.Exec(ctx, `UPDATE pf_coop.household SET commune_code=$2,cultivated_area_ha=$3::numeric,livestock_area_ha=$4::numeric,
 active=$5,native_deleted=$6,native_revision=$7,updated_at=now() WHERE household_ref=$1`, ref, commune, area, livestock, active, deleted, revision)
	return err
}
