package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ledger"
)

type StrawFactorEvent struct {
	ID, TenantID, ActorRef  uuid.UUID
	VarietyCode             string
	Seq                     int64
	Percentage              decimal.Decimal
	Previous                *decimal.Decimal
	EffectiveAt, RecordedAt time.Time
	IdemKey                 string
}

const factorColumns = `id,tenant_id,actor_ref,variety_code,seq,percentage::text,previous_percentage::text,effective_at,recorded_at,idem_key`

func scanStrawFactor(row pgx.Row) (*StrawFactorEvent, error) {
	var e StrawFactorEvent
	var percentage string
	var previous *string
	err := row.Scan(&e.ID, &e.TenantID, &e.ActorRef, &e.VarietyCode, &e.Seq, &percentage, &previous, &e.EffectiveAt, &e.RecordedAt, &e.IdemKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	e.Percentage, err = decimal.NewFromString(percentage)
	if err != nil {
		return nil, err
	}
	if previous != nil {
		v, err := decimal.NewFromString(*previous)
		if err != nil {
			return nil, err
		}
		e.Previous = &v
	}
	return &e, nil
}

func LockStrawFactorOperation(ctx context.Context, tx pgx.Tx, key, code string) error {
	for _, lock := range []string{"idem:" + key, "variety:" + code} {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('pf_coop.straw_factor:'||shared.current_tenant()::text||':'||$1,282))`, lock); err != nil {
			return err
		}
	}
	return nil
}

func StrawFactorByIdem(ctx context.Context, tx pgx.Tx, key string) (*StrawFactorEvent, error) {
	return scanStrawFactor(tx.QueryRow(ctx, "SELECT "+factorColumns+" FROM pf_coop.straw_factor_event WHERE idem_key=$1"+ledger.NewestByIdem(""), key))
}

func StrawFactorAt(ctx context.Context, tx pgx.Tx, code string, at time.Time) (*StrawFactorEvent, error) {
	var when any
	if !at.IsZero() {
		when = at.UTC()
	}
	return scanStrawFactor(tx.QueryRow(ctx, "SELECT "+factorColumns+` FROM pf_coop.straw_factor_event
 WHERE variety_code=$1 AND ($2::timestamptz IS NULL OR effective_at<=$2) ORDER BY effective_at DESC,seq DESC LIMIT 1`, code, when))
}

func StrawFactorHistory(ctx context.Context, tx pgx.Tx, code string, after int64, limit int) ([]StrawFactorEvent, error) {
	rows, err := tx.Query(ctx, "SELECT "+factorColumns+` FROM pf_coop.straw_factor_event
 WHERE variety_code=$1 AND ($2::bigint=0 OR seq<$2) ORDER BY seq DESC LIMIT $3`, code, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []StrawFactorEvent{}
	for rows.Next() {
		e, err := scanStrawFactor(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

func CurrentStrawFactors(ctx context.Context, tx pgx.Tx) (map[string]StrawFactorEvent, error) {
	rows, err := tx.Query(ctx, "SELECT "+factorColumns+` FROM (SELECT DISTINCT ON(variety_code) * FROM pf_coop.straw_factor_event ORDER BY variety_code,seq DESC) e`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]StrawFactorEvent{}
	for rows.Next() {
		e, err := scanStrawFactor(rows)
		if err != nil {
			return nil, err
		}
		out[e.VarietyCode] = *e
	}
	return out, rows.Err()
}

func InsertStrawFactor(ctx context.Context, tx pgx.Tx, e *StrawFactorEvent) error {
	var previous any
	if e.Previous != nil {
		previous = e.Previous.String()
	}
	// Read after the per-variety lock, using the DB clock rather than a browser
	// timestamp. A tiny monotonic fence also orders changes across clock skew.
	return tx.QueryRow(ctx, `INSERT INTO pf_coop.straw_factor_event
 (id,tenant_id,stream_key,variety_code,percentage,previous_percentage,effective_at,event_time,actor_ref,idem_key,rule_version)
 VALUES($1,$2,'straw_factor:'||$3,$3,$4::numeric,$5::numeric,
 greatest(clock_timestamp(),coalesce((SELECT max(effective_at)+interval '1 microsecond' FROM pf_coop.straw_factor_event WHERE variety_code=$3),'-infinity'::timestamptz)),
 clock_timestamp(),$6,$7,'htx.fn11.v1') RETURNING seq,effective_at,recorded_at`, e.ID, e.TenantID, e.VarietyCode, e.Percentage.String(), previous, e.ActorRef, e.IdemKey).Scan(&e.Seq, &e.EffectiveAt, &e.RecordedAt)
}
