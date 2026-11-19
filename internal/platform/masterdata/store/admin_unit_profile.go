package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/masterdata/domain"
	partyport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/party/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ledger"
)

// UnitProfileEvent stores only catalog metadata, never personal data. A legal
// code can be reused after reorganization, so its start day is part of the key.
type UnitProfileEvent struct {
	ID, TenantID, ActorRef               uuid.UUID
	Code, Name, Action, IdemKey          string
	UnitValidFrom, EventTime, RecordedAt time.Time
	Active                               bool
	Revision                             int64
	SupersedesID                         *uuid.UUID
	CorrectionReason                     *string
}

func UnitProfileStream(code string, from time.Time) string {
	return "admin_unit_profile:" + code + ":" + day(from)
}

func InsertUnitProfile(ctx context.Context, tx pgx.Tx, e *UnitProfileEvent) error {
	return tx.QueryRow(ctx, `INSERT INTO pf_masterdata.admin_unit_profile_event
 (id,tenant_id,stream_key,code,unit_valid_from,action,name,active,event_time,actor_ref,idem_key,
 supersedes_id,correction_reason,rule_version)
 VALUES($1::uuid,$2::uuid,$3::text,$4::text,$5::date,$6::text,$7::text,$8::boolean,$9::timestamptz,
 $10::uuid,$11::text,$12::uuid,$13::text,'masterdata.admin-unit-profile.v1') RETURNING seq,recorded_at`,
		e.ID, e.TenantID, UnitProfileStream(e.Code, e.UnitValidFrom), e.Code, day(e.UnitValidFrom), e.Action, e.Name, e.Active,
		e.EventTime.UTC(), e.ActorRef, e.IdemKey, e.SupersedesID, e.CorrectionReason).Scan(&e.Revision, &e.RecordedAt)
}

const unitProfileCols = `id,tenant_id,actor_ref,code,name,action,idem_key,unit_valid_from,event_time,recorded_at,active,seq,supersedes_id,correction_reason`

func scanUnitProfile(row pgx.Row) (*UnitProfileEvent, error) {
	var e UnitProfileEvent
	err := row.Scan(&e.ID, &e.TenantID, &e.ActorRef, &e.Code, &e.Name, &e.Action, &e.IdemKey, &e.UnitValidFrom, &e.EventTime, &e.RecordedAt, &e.Active, &e.Revision, &e.SupersedesID, &e.CorrectionReason)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &e, err
}

func UnitProfileByKey(ctx context.Context, tx pgx.Tx, key string) (*UnitProfileEvent, error) {
	return scanUnitProfile(tx.QueryRow(ctx, "SELECT "+unitProfileCols+" FROM pf_masterdata.admin_unit_profile_event WHERE idem_key=$1"+ledger.NewestByIdem(""), key))
}

func LatestUnitProfile(ctx context.Context, tx pgx.Tx, code string, from time.Time) (*UnitProfileEvent, error) {
	return scanUnitProfile(tx.QueryRow(ctx, "SELECT "+unitProfileCols+` FROM pf_masterdata.admin_unit_profile_event
 WHERE stream_key=$1 ORDER BY seq DESC LIMIT 1`, UnitProfileStream(code, from)))
}

func ProjectUnitProfile(ctx context.Context, tx pgx.Tx, e UnitProfileEvent) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_masterdata.admin_unit_profile_current
 (tenant_id,code,unit_valid_from,id,revision,name,active,actor_ref,event_time)
 VALUES($1::uuid,$2::text,$3::date,$4::uuid,$5::bigint,$6::text,$7::boolean,$8::uuid,$9::timestamptz)
 ON CONFLICT(tenant_id,code,unit_valid_from) DO UPDATE SET
 id=EXCLUDED.id,revision=EXCLUDED.revision,name=EXCLUDED.name,active=EXCLUDED.active,
 actor_ref=EXCLUDED.actor_ref,event_time=EXCLUDED.event_time
 WHERE admin_unit_profile_current.revision<EXCLUDED.revision`,
		e.TenantID, e.Code, day(e.UnitValidFrom), e.ID, e.Revision, e.Name, e.Active, e.ActorRef, e.EventTime.UTC())
	return err
}

// ManagedUnit includes the original legal version and its operational metadata.
// Active is the unit's own switch; Selectable also considers the parent switch.
type ManagedUnit struct {
	Unit               domain.Unit
	Active, Selectable bool
	Revision           int64
	EventID            *uuid.UUID
}

// ManagedUnits reads legal versions at a business day and metadata as of that
// day's end in Vietnam. It does not alter legal validity or node scope. Names
// are filtered in Go so Vietnamese comparisons do not depend on LC_CTYPE.
// parent=nil reads both levels; &"" reads provinces; otherwise direct communes.
func ManagedUnits(ctx context.Context, tx pgx.Tx, at time.Time, parent *string) ([]ManagedUnit, error) {
	return managedUnits(ctx, tx, at, parent, "")
}

func ManagedUnitAt(ctx context.Context, tx pgx.Tx, at time.Time, code string) (*ManagedUnit, error) {
	units, err := managedUnits(ctx, tx, at, nil, code)
	if err != nil || len(units) == 0 {
		return nil, err
	}
	return &units[0], nil
}

func managedUnits(ctx context.Context, tx pgx.Tx, at time.Time, parent *string, code string) ([]ManagedUnit, error) {
	rows, err := tx.Query(ctx, `SELECT u.code,u.valid_from,u.valid_to,COALESCE(m.name,u.name),u.level,
 COALESCE(u.parent_code,''),u.successor_codes,COALESCE(m.active,true),
 COALESCE(m.active,true) AND COALESCE(p.active,true),COALESCE(m.seq,0),m.id
 FROM pf_masterdata.admin_unit u
 LEFT JOIN LATERAL (
  SELECT e.name,e.active,e.seq,e.id FROM pf_masterdata.admin_unit_profile_event e
  WHERE e.tenant_id=u.tenant_id AND e.code=u.code AND e.unit_valid_from=u.valid_from
   AND e.event_time < (($1::date+1)::timestamp AT TIME ZONE 'Asia/Ho_Chi_Minh')
  ORDER BY e.event_time DESC,e.seq DESC LIMIT 1
 ) m ON true
 LEFT JOIN LATERAL (
  SELECT e.active FROM pf_masterdata.admin_unit b
  JOIN pf_masterdata.admin_unit_profile_event e ON e.tenant_id=b.tenant_id AND e.code=b.code AND e.unit_valid_from=b.valid_from
  WHERE b.tenant_id=u.tenant_id AND b.code=u.parent_code
   AND b.valid_from<=$1::date AND (b.valid_to IS NULL OR b.valid_to>$1::date)
   AND e.event_time < (($1::date+1)::timestamp AT TIME ZONE 'Asia/Ho_Chi_Minh')
  ORDER BY e.event_time DESC,e.seq DESC LIMIT 1
 ) p ON true
 WHERE u.valid_from<=$1::date AND (u.valid_to IS NULL OR u.valid_to>$1::date)
  AND u.level IN ('province','commune')
  AND ($3::text='' OR u.code=$3::text)
  AND ($2::text IS NULL OR ($2='' AND u.level='province') OR (u.parent_code=$2 AND u.level='commune'))
 ORDER BY u.code`, day(at), parent, code)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ManagedUnit{}
	for rows.Next() {
		var m ManagedUnit
		var level string
		if err := rows.Scan(&m.Unit.Code, &m.Unit.ValidFrom, &m.Unit.ValidTo, &m.Unit.Name, &level, &m.Unit.ParentCode, &m.Unit.Successors, &m.Active, &m.Selectable, &m.Revision, &m.EventID); err != nil {
			return nil, err
		}
		m.Unit.Level = partyport.Level(level)
		out = append(out, m)
	}
	return out, rows.Err()
}

// RebuildUnitProfiles rebuilds only operational metadata. The geographic
// version/succession source remains immutable and is not deleted or rewritten.
func RebuildUnitProfiles(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, `DELETE FROM pf_masterdata.admin_unit_profile_current`); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO pf_masterdata.admin_unit_profile_current
 (tenant_id,code,unit_valid_from,id,revision,name,active,actor_ref,event_time)
 SELECT DISTINCT ON (tenant_id,code,unit_valid_from)
 tenant_id,code,unit_valid_from,id,seq,name,active,actor_ref,event_time
 FROM pf_masterdata.admin_unit_profile_event ORDER BY tenant_id,code,unit_valid_from,seq DESC`)
	return err
}
