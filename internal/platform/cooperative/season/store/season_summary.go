package store

// AH-152: sổ tổng kết vụ theo hộ (pf_coop.household_season_summary_event) — bản chụp cố định lúc hoàn thành vụ.

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ledger"
)

const seasonSummaryRule = "htx.khmv.fn06.season_summary.v1"

// SeasonSummaryEvent là một dòng của sổ tổng kết.
type SeasonSummaryEvent struct {
	ID, TenantID, SeasonRef, CooperativeRef, HouseholdRef, SeasonEventID, ActorRef uuid.UUID
	Seq                                                                            int64
	Summary                                                                        json.RawMessage
	CostVND                                                                        *decimal.Decimal
	Note                                                                           *string
	EventTime, RecordedAt                                                          time.Time
	IdemKey                                                                        string
}

const seasonSummaryCols = `id, tenant_id, season_ref, cooperative_ref, household_ref, season_event_id, actor_ref, seq, summary,
 cost_vnd::text, note, event_time, recorded_at, idem_key`

func scanSeasonSummary(row pgx.Row) (*SeasonSummaryEvent, error) {
	var e SeasonSummaryEvent
	var cost *string
	err := row.Scan(&e.ID, &e.TenantID, &e.SeasonRef, &e.CooperativeRef, &e.HouseholdRef, &e.SeasonEventID, &e.ActorRef, &e.Seq,
		&e.Summary, &cost, &e.Note, &e.EventTime, &e.RecordedAt, &e.IdemKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if cost != nil {
		d, err := decimal.NewFromString(*cost)
		if err != nil {
			return nil, err
		}
		e.CostVND = &d
	}
	return &e, nil
}

// CurrentSeasonSummary đọc bản tổng kết mới nhất của vụ (nil khi chưa hoàn thành).
func CurrentSeasonSummary(ctx context.Context, tx pgx.Tx, season uuid.UUID) (*SeasonSummaryEvent, error) {
	return scanSeasonSummary(tx.QueryRow(ctx, "SELECT "+seasonSummaryCols+` FROM pf_coop.household_season_summary_event
 WHERE stream_key = 'season_summary:' || $1::text ORDER BY seq DESC LIMIT 1`, season))
}

// SeasonSummaryByIdem đọc dòng đã ghi theo idem_key (phát lại).
func SeasonSummaryByIdem(ctx context.Context, tx pgx.Tx, key string) (*SeasonSummaryEvent, error) {
	return scanSeasonSummary(tx.QueryRow(ctx, "SELECT "+seasonSummaryCols+
		" FROM pf_coop.household_season_summary_event WHERE idem_key = $1"+ledger.NewestByIdem(""), key))
}

// InsertSeasonSummary ghi thêm bản tổng kết; seq, recorded_at lấy từ sổ.
func InsertSeasonSummary(ctx context.Context, tx pgx.Tx, e *SeasonSummaryEvent) error {
	var cost any
	if e.CostVND != nil {
		cost = e.CostVND.String()
	}
	return tx.QueryRow(ctx, `INSERT INTO pf_coop.household_season_summary_event
 (id, tenant_id, stream_key, season_ref, cooperative_ref, household_ref, season_event_id, summary, cost_vnd, note,
  event_time, actor_ref, idem_key, rule_version)
 VALUES ($1, $2, 'season_summary:' || $3::uuid::text, $3::uuid, $4, $5, $6, $7::jsonb, $8::numeric, $9, $10, $11, $12, $13)
 RETURNING seq, recorded_at`,
		e.ID, e.TenantID, e.SeasonRef, e.CooperativeRef, e.HouseholdRef, e.SeasonEventID, string(e.Summary), cost, e.Note,
		e.EventTime, e.ActorRef, e.IdemKey, seasonSummaryRule).Scan(&e.Seq, &e.RecordedAt)
}
