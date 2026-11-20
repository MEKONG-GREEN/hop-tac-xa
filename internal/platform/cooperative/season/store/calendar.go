package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ledger"
)

// CalendarEvent is the complete immutable snapshot of one published revision.
// The application must validate catalogs, recipients and access before writing.
type CalendarEvent struct {
	ID, TenantID, CalendarRef, CooperativeRef uuid.UUID
	Seq                                       int64
	Action                                    string
	Draft                                     domain.CalendarDraft
	ActorRef                                  uuid.UUID
	EventTime, RecordedAt                     time.Time
	SupersedesID                              *uuid.UUID
	CorrectionReason                          *string
	IdemKey                                   string
}

func StreamCalendar(ref uuid.UUID) string { return "season_calendar:" + ref.String() }

// Serialize a retry even for first publication, whose calendar_ref is generated
// by the server and therefore cannot be the initial idempotency lock key.
func LockCalendarOperation(ctx context.Context, tx pgx.Tx, key string) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('pf_coop.calendar_operation:'||current_setting('app.tenant_id',true)||':'||$1::text,0))", key)
	return err
}

func ListCalendars(ctx context.Context, tx pgx.Tx, coop, recipient, after uuid.UUID, limit int, management bool) ([]CalendarEvent, error) {
	rows, err := tx.Query(ctx, "SELECT "+calendarCols+` FROM pf_coop.season_calendar_event
 WHERE id IN (SELECT id FROM pf_coop.season_calendar_current
  WHERE cooperative_ref=$1::uuid AND ($2::boolean OR (status='published' AND $3::uuid=ANY(recipients)))
    AND ($4::uuid IS NULL OR calendar_ref<$4::uuid) ORDER BY calendar_ref DESC LIMIT $5::int)
 ORDER BY calendar_ref DESC`, coop, management, recipient, nilIfZero(after), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CalendarEvent{}
	for rows.Next() {
		e, err := scanCalendar(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// LockCalendar serializes both first publication and later edits, including
// durable idempotency lookup when no current projection exists yet.
func LockCalendar(ctx context.Context, tx pgx.Tx, ref uuid.UUID) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('pf_coop.season_calendar:'||current_setting('app.tenant_id',true)||':'||$1::text,0))", ref.String())
	return err
}

func InsertCalendarEvent(ctx context.Context, tx pgx.Tx, e *CalendarEvent) error {
	return tx.QueryRow(ctx, `INSERT INTO pf_coop.season_calendar_event
 (id,tenant_id,stream_key,calendar_ref,cooperative_ref,action,crop_code,season_code,season_year,sow_from,sow_to,recipients,
 actor_ref,event_time,supersedes_id,correction_reason,idem_key,rule_version)
 VALUES($1::uuid,$2::uuid,$3::text,$4::uuid,$5::uuid,$6::text,$7::text,$8::text,$9::int,$10::date,$11::date,$12::uuid[],
 $13::uuid,$14::timestamptz,$15::uuid,$16::text,$17::text,$18::text) RETURNING seq,recorded_at`,
		e.ID, e.TenantID, StreamCalendar(e.CalendarRef), e.CalendarRef, e.CooperativeRef, e.Action, e.Draft.CropCode, e.Draft.SeasonCode,
		e.Draft.SeasonYear, day(e.Draft.SowFrom), day(e.Draft.SowTo), uuids(e.Draft.Recipients), e.ActorRef, e.EventTime.UTC(),
		e.SupersedesID, e.CorrectionReason, e.IdemKey, domain.RuleVersion).Scan(&e.Seq, &e.RecordedAt)
}

const calendarCols = `id,tenant_id,calendar_ref,cooperative_ref,seq,action,crop_code,season_code,season_year,sow_from,sow_to,
 recipients::text[],actor_ref,event_time,recorded_at,supersedes_id,correction_reason,idem_key`

func scanCalendar(row pgx.Row) (CalendarEvent, error) {
	var e CalendarEvent
	var recipients []string
	err := row.Scan(&e.ID, &e.TenantID, &e.CalendarRef, &e.CooperativeRef, &e.Seq, &e.Action, &e.Draft.CropCode, &e.Draft.SeasonCode,
		&e.Draft.SeasonYear, &e.Draft.SowFrom, &e.Draft.SowTo, &recipients, &e.ActorRef, &e.EventTime, &e.RecordedAt, &e.SupersedesID, &e.CorrectionReason, &e.IdemKey)
	if err != nil {
		return e, err
	}
	e.Draft.Recipients, err = parseUUIDs(recipients)
	return e, err
}

func CalendarByIdem(ctx context.Context, tx pgx.Tx, key string) (*CalendarEvent, error) {
	e, err := scanCalendar(tx.QueryRow(ctx, "SELECT "+calendarCols+" FROM pf_coop.season_calendar_event WHERE idem_key=$1::text"+ledger.NewestByIdem(""), key))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &e, err
}

// CalendarCurrent reads the source revision referenced by the projection. A
// cancelled revision remains readable for management, but member lists exclude it.
func CalendarCurrent(ctx context.Context, tx pgx.Tx, ref uuid.UUID) (*CalendarEvent, error) {
	e, err := scanCalendar(tx.QueryRow(ctx, "SELECT "+calendarCols+` FROM pf_coop.season_calendar_event
 WHERE calendar_ref=$1::uuid AND id=(SELECT id FROM pf_coop.season_calendar_current WHERE calendar_ref=$1::uuid)`, ref))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &e, err
}

// SaveCalendar is shared by write and rebuild paths. Out-of-order replay cannot
// overwrite a newer revision; the cooperative identity cannot change on edit.
func SaveCalendar(ctx context.Context, tx pgx.Tx, e CalendarEvent) error {
	status := "published"
	if e.Action == "cancelled" {
		status = "cancelled"
	}
	tag, err := tx.Exec(ctx, `INSERT INTO pf_coop.season_calendar_current AS c
 (tenant_id,calendar_ref,cooperative_ref,id,version,status,crop_code,season_code,season_year,sow_from,sow_to,recipients,actor_ref,event_time,recorded_at)
 VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::bigint,$6::text,$7::text,$8::text,$9::int,$10::date,$11::date,$12::uuid[],$13::uuid,$14::timestamptz,$15::timestamptz)
 ON CONFLICT(tenant_id,calendar_ref) DO UPDATE SET id=excluded.id,version=excluded.version,status=excluded.status,
 crop_code=excluded.crop_code,season_code=excluded.season_code,season_year=excluded.season_year,sow_from=excluded.sow_from,sow_to=excluded.sow_to,
 recipients=excluded.recipients,actor_ref=excluded.actor_ref,event_time=excluded.event_time,recorded_at=excluded.recorded_at
 WHERE c.version<excluded.version AND c.cooperative_ref=excluded.cooperative_ref`, e.TenantID, e.CalendarRef, e.CooperativeRef, e.ID, e.Seq, status,
		e.Draft.CropCode, e.Draft.SeasonCode, e.Draft.SeasonYear, day(e.Draft.SowFrom), day(e.Draft.SowTo), uuids(e.Draft.Recipients), e.ActorRef, e.EventTime, e.RecordedAt)
	if err != nil || tag.RowsAffected() != 0 {
		return err
	}
	var sameCooperative bool
	if err := tx.QueryRow(ctx, `SELECT cooperative_ref=$2::uuid FROM pf_coop.season_calendar_current WHERE calendar_ref=$1::uuid`, e.CalendarRef, e.CooperativeRef).Scan(&sameCooperative); err != nil {
		return err
	}
	if !sameCooperative {
		return errors.New("calendar: cooperative identity cannot change")
	}
	return nil
}

// RebuildCalendars only replaces this tenant's calendar projection. It does not
// enqueue notifications or modify seasons opened using calendar guidance.
func RebuildCalendars(ctx context.Context, tx pgx.Tx) (int, error) {
	rows, err := tx.Query(ctx, "SELECT "+calendarCols+" FROM pf_coop.season_calendar_event ORDER BY calendar_ref,seq")
	if err != nil {
		return 0, err
	}
	events := []CalendarEvent{}
	for rows.Next() {
		e, err := scanCalendar(rows)
		if err != nil {
			rows.Close()
			return 0, err
		}
		events = append(events, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	if _, err = tx.Exec(ctx, "DELETE FROM pf_coop.season_calendar_current"); err != nil {
		return 0, err
	}
	refs := map[uuid.UUID]bool{}
	for _, e := range events {
		if err = SaveCalendar(ctx, tx, e); err != nil {
			return 0, err
		}
		refs[e.CalendarRef] = true
	}
	return len(refs), nil
}
