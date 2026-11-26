package store

// AH-153 (FN-06.6): vụ nông hộ của HTX cho màn sản lượng thực tế so với dự kiến.

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

// YieldFilter lọc vụ theo HTX, nhóm (chờ khai báo / đã khai báo), người mở vụ và mùa vụ.
type YieldFilter struct {
	CooperativeRef uuid.UUID
	Declared       bool
	OpenedBy       uuid.UUID
	SeasonCode     string
	SeasonYear     int
}

// YieldCursor là vị trí sau dòng cuối của trang trước: ngày (dự kiến với nhóm chờ, thu hoạch với nhóm đã khai báo) và
// season_ref. Nhóm chờ khai báo đưa vụ chưa có ngày dự kiến xuống cuối (ngày 9999-12-31).
type YieldCursor struct {
	Day       time.Time
	SeasonRef uuid.UUID
}

// NoExpectedDay thay ngày dự kiến trống khi sắp xếp nhóm chờ khai báo.
var NoExpectedDay = time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC)

func (f YieldFilter) statuses() []string {
	if f.Declared {
		return []string{"completed", "closed"}
	}
	return []string{"open"}
}

// HouseholdYieldSeasons đọc một trang vụ. Nhóm chờ khai báo theo ngày thu hoạch dự kiến tăng dần; nhóm đã khai báo
// theo ngày thu hoạch giảm dần; cùng ngày theo season_ref.
func HouseholdYieldSeasons(ctx context.Context, tx pgx.Tx, f YieldFilter, after *YieldCursor, limit int) ([]Season, error) {
	var day *time.Time
	var ref *uuid.UUID
	if after != nil {
		day, ref = &after.Day, &after.SeasonRef
	}
	where := ` FROM pf_coop.season_current s LEFT JOIN pf_coop.harvest_declaration h ON h.id = s.harvest_id AND h.season_ref = s.season_ref
	 WHERE s.entry_kind='household' AND s.cooperative_ref=$1 AND s.status = ANY($2::text[])
	 AND ($3::uuid IS NULL OR s.opened_by=$3::uuid) AND ($4::text='' OR s.season_code=$4) AND ($5::int=0 OR s.season_year=$5)`
	var q string
	if f.Declared {
		q = "SELECT " + seasonCols + where + ` AND h.id IS NOT NULL
	 AND ($6::date IS NULL OR (h.harvest_date, s.season_ref) < ($6::date, $7::uuid))
	 ORDER BY h.harvest_date DESC, s.season_ref DESC LIMIT $8`
	} else {
		q = "SELECT " + seasonCols + where + `
	 AND ($6::date IS NULL OR (coalesce(s.expected_harvest_date, DATE '9999-12-31'), s.season_ref) > ($6::date, $7::uuid))
	 ORDER BY coalesce(s.expected_harvest_date, DATE '9999-12-31'), s.season_ref LIMIT $8`
	}
	rows, err := tx.Query(ctx, q, f.CooperativeRef, f.statuses(), nilIfZero(f.OpenedBy), f.SeasonCode, f.SeasonYear, day, ref, limit)
	if err != nil {
		return nil, err
	}
	return collectSeasons(rows)
}

// YieldFact là dữ kiện tối thiểu của một vụ để cộng tổng (cả hai nhóm).
type YieldFact struct {
	SeasonRef uuid.UUID
	CropCode  string
	AreaHa    decimal.Decimal
	Declared  bool
	MassKg    *decimal.Decimal
}

// HouseholdYieldFacts đọc mọi vụ chờ khai báo và đã khai báo khớp bộ lọc (bỏ qua nhóm) để cộng tổng.
func HouseholdYieldFacts(ctx context.Context, tx pgx.Tx, f YieldFilter) ([]YieldFact, error) {
	rows, err := tx.Query(ctx, `SELECT s.season_ref, coalesce(s.crop_code, ''), s.area_ha::text, s.status <> 'open', h.mass_kg::text
	 FROM pf_coop.season_current s LEFT JOIN pf_coop.harvest_declaration h ON h.id = s.harvest_id AND h.season_ref = s.season_ref
	 WHERE s.entry_kind='household' AND s.cooperative_ref=$1 AND s.status IN ('open', 'completed', 'closed')
	 AND ($2::uuid IS NULL OR s.opened_by=$2::uuid) AND ($3::text='' OR s.season_code=$3) AND ($4::int=0 OR s.season_year=$4)
	 ORDER BY s.season_ref`, f.CooperativeRef, nilIfZero(f.OpenedBy), f.SeasonCode, f.SeasonYear)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []YieldFact{}
	for rows.Next() {
		var y YieldFact
		var area string
		var mass *string
		if err := rows.Scan(&y.SeasonRef, &y.CropCode, &area, &y.Declared, &mass); err != nil {
			return nil, err
		}
		a, err := decimal.NewFromString(area)
		if err != nil {
			return nil, err
		}
		y.AreaHa = a
		if mass != nil {
			m, err := decimal.NewFromString(*mass)
			if err != nil {
				return nil, err
			}
			y.MassKg = &m
		}
		out = append(out, y)
	}
	return out, rows.Err()
}
