package store

// AH-87: sổ sản lượng lúa, rơm dự kiến của vụ theo hộ (pf_coop.household_yield_estimate_event). Mỗi dòng là trạng
// thái đầy đủ; bản hiện hành là dòng seq lớn nhất của stream "yield_estimate:<season_ref>". seq, chuỗi băm và tính
// bất biến do shared.apply_ledger đặt.

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ledger"
)

// Hành động trên sổ sản lượng dự kiến.
const (
	YieldEstimateComputed   = "computed"
	YieldEstimateEntered    = "entered"
	YieldEstimateRecomputed = "recomputed"
	yieldEstimateRule       = "htx.khmv.fn04.yield_estimate.v1"
)

// YieldEstimateEvent là một dòng của sổ.
type YieldEstimateEvent struct {
	ID, TenantID, SeasonRef, CooperativeRef, HouseholdRef, ActorRef uuid.UUID
	Seq                                                             int64
	Action                                                          string
	Estimate                                                        domain.YieldEstimate
	EventTime, RecordedAt                                           time.Time
	IdemKey                                                         string
}

const yieldEstimateColumns = `id, tenant_id, season_ref, cooperative_ref, household_ref, actor_ref, seq, action,
 area_ha::text, base_yield_t_per_ha::text, is_rice, straw_pct::text, computed_rice_t::text, computed_straw_t::text,
 entered_rice_t::text, entered_straw_t::text, event_time, recorded_at, idem_key`

func scanYieldEstimate(row pgx.Row) (*YieldEstimateEvent, error) {
	var e YieldEstimateEvent
	var area, base, rice string
	var straw, cstraw, erice, estraw *string
	err := row.Scan(&e.ID, &e.TenantID, &e.SeasonRef, &e.CooperativeRef, &e.HouseholdRef, &e.ActorRef, &e.Seq, &e.Action,
		&area, &base, &e.Estimate.IsRice, &straw, &rice, &cstraw, &erice, &estraw, &e.EventTime, &e.RecordedAt, &e.IdemKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	for _, f := range []struct {
		raw string
		dst *decimal.Decimal
	}{{area, &e.Estimate.AreaHa}, {base, &e.Estimate.BaseYield}, {rice, &e.Estimate.ComputedRice}} {
		if *f.dst, err = decimal.NewFromString(f.raw); err != nil {
			return nil, err
		}
	}
	for _, f := range []struct {
		raw *string
		dst **decimal.Decimal
	}{{straw, &e.Estimate.StrawPct}, {cstraw, &e.Estimate.ComputedStraw}, {erice, &e.Estimate.EnteredRice}, {estraw, &e.Estimate.EnteredStraw}} {
		if f.raw == nil {
			continue
		}
		d, err := decimal.NewFromString(*f.raw)
		if err != nil {
			return nil, err
		}
		*f.dst = &d
	}
	return &e, nil
}

// CurrentYieldEstimate đọc bản hiện hành của một vụ (nil khi vụ chưa có dòng nào — vụ mở trước AH-87).
func CurrentYieldEstimate(ctx context.Context, tx pgx.Tx, season uuid.UUID) (*YieldEstimateEvent, error) {
	return scanYieldEstimate(tx.QueryRow(ctx, "SELECT "+yieldEstimateColumns+` FROM pf_coop.household_yield_estimate_event
 WHERE stream_key = 'yield_estimate:' || $1::text ORDER BY seq DESC LIMIT 1`, season))
}

// CurrentYieldEstimates đọc bản hiện hành của nhiều vụ (vụ chưa có dòng không có trong map).
func CurrentYieldEstimates(ctx context.Context, tx pgx.Tx, seasons []uuid.UUID) (map[uuid.UUID]YieldEstimateEvent, error) {
	out := map[uuid.UUID]YieldEstimateEvent{}
	if len(seasons) == 0 {
		return out, nil
	}
	keys := make([]string, len(seasons))
	for i, s := range seasons {
		keys[i] = "yield_estimate:" + s.String()
	}
	rows, err := tx.Query(ctx, "SELECT "+yieldEstimateColumns+` FROM (SELECT DISTINCT ON (stream_key) *
 FROM pf_coop.household_yield_estimate_event WHERE stream_key = ANY($1::text[]) ORDER BY stream_key, seq DESC) e`, keys)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		e, err := scanYieldEstimate(rows)
		if err != nil {
			return nil, err
		}
		out[e.SeasonRef] = *e
	}
	return out, rows.Err()
}

// YieldEstimateByIdem đọc dòng đã ghi theo idem_key (phát lại).
func YieldEstimateByIdem(ctx context.Context, tx pgx.Tx, key string) (*YieldEstimateEvent, error) {
	return scanYieldEstimate(tx.QueryRow(ctx, "SELECT "+yieldEstimateColumns+
		" FROM pf_coop.household_yield_estimate_event WHERE idem_key = $1"+ledger.NewestByIdem(""), key))
}

func decimalArg(d *decimal.Decimal) any {
	if d == nil {
		return nil
	}
	return d.String()
}

// InsertYieldEstimate ghi thêm một dòng; seq, recorded_at lấy từ sổ.
func InsertYieldEstimate(ctx context.Context, tx pgx.Tx, e *YieldEstimateEvent) error {
	x := e.Estimate
	return tx.QueryRow(ctx, `INSERT INTO pf_coop.household_yield_estimate_event
 (id, tenant_id, stream_key, season_ref, cooperative_ref, household_ref, action, area_ha, base_yield_t_per_ha, is_rice,
  straw_pct, computed_rice_t, computed_straw_t, entered_rice_t, entered_straw_t, event_time, actor_ref, idem_key, rule_version)
 VALUES ($1, $2, 'yield_estimate:' || $3::uuid::text, $3::uuid, $4, $5, $6, $7::numeric, $8::numeric, $9,
  $10::numeric, $11::numeric, $12::numeric, $13::numeric, $14::numeric, $15, $16, $17, $18)
 RETURNING seq, recorded_at`,
		e.ID, e.TenantID, e.SeasonRef, e.CooperativeRef, e.HouseholdRef, e.Action, x.AreaHa.String(), x.BaseYield.String(), x.IsRice,
		decimalArg(x.StrawPct), x.ComputedRice.String(), decimalArg(x.ComputedStraw), decimalArg(x.EnteredRice), decimalArg(x.EnteredStraw),
		e.EventTime, e.ActorRef, e.IdemKey, yieldEstimateRule).Scan(&e.Seq, &e.RecordedAt)
}
