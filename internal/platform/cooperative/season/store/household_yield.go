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
	// HarvestRecordedOnly: nhóm chờ khai báo chỉ lấy vụ đã ghi việc Thu hoạch (AH-177, KH-20).
	HarvestRecordedOnly bool
}

// harvestRecordedSQL: vụ có việc Thu hoạch (loại việc thu_hoach của quy trình, gồm việc tự thêm) đang Xong.
const harvestRecordedSQL = `EXISTS (SELECT 1 FROM pf_coop.household_task_current t WHERE t.tenant_id = s.tenant_id AND t.season_ref = s.season_ref
	 AND t.state = 'done' AND t.sop_step_seq IN (SELECT (e->>'seq')::int FROM jsonb_array_elements(
	 coalesce(s.sop_snapshot->'steps', '[]'::jsonb) || coalesce(s.sop_snapshot->'custom'->'added', '[]'::jsonb)) e
	 WHERE e->>'activity_code' = 'thu_hoach'))`

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
	 AND ($3::uuid IS NULL OR s.opened_by=$3::uuid) AND ($4::text='' OR s.season_code=$4) AND ($5::int=0 OR s.season_year=$5)
	 AND (NOT $9::bool OR s.status <> 'open' OR ` + harvestRecordedSQL + `)`
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
	rows, err := tx.Query(ctx, q, f.CooperativeRef, f.statuses(), nilIfZero(f.OpenedBy), f.SeasonCode, f.SeasonYear, day, ref, limit, f.HarvestRecordedOnly)
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
	 AND (NOT $5::bool OR s.status <> 'open' OR `+harvestRecordedSQL+`)
	 ORDER BY s.season_ref`, f.CooperativeRef, nilIfZero(f.OpenedBy), f.SeasonCode, f.SeasonYear, f.HarvestRecordedOnly)
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

// HarvestRecorded trả các vụ (trong danh sách) đã ghi việc Thu hoạch.
func HarvestRecorded(ctx context.Context, tx pgx.Tx, seasons []uuid.UUID) (map[uuid.UUID]bool, error) {
	out := map[uuid.UUID]bool{}
	if len(seasons) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, `SELECT s.season_ref FROM pf_coop.season_current s WHERE s.season_ref = ANY($1::uuid[]) AND `+harvestRecordedSQL, uuidStrings(seasons))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var r uuid.UUID
		if err := rows.Scan(&r); err != nil {
			return nil, err
		}
		out[r] = true
	}
	return out, rows.Err()
}

// HouseholdSeasonsByRefs đọc vụ theo hộ của HTX theo danh sách mã vụ (nhập hàng loạt — AH-177). Mã không thuộc HTX
// hoặc không phải vụ theo hộ không có trong kết quả.
func HouseholdSeasonsByRefs(ctx context.Context, tx pgx.Tx, coop uuid.UUID, refs []uuid.UUID) (map[uuid.UUID]Season, error) {
	out := map[uuid.UUID]Season{}
	if len(refs) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, "SELECT "+seasonCols+` FROM pf_coop.season_current s
	 WHERE s.entry_kind='household' AND s.cooperative_ref=$1 AND s.season_ref = ANY($2::uuid[])`, coop, uuidStrings(refs))
	if err != nil {
		return nil, err
	}
	list, err := collectSeasons(rows)
	if err != nil {
		return nil, err
	}
	for _, sn := range list {
		out[sn.SeasonRef] = sn
	}
	return out, nil
}

// HouseholdCodes đọc mã hộ (không phải dữ liệu cá nhân) theo household_ref cho tệp mẫu.
func HouseholdCodes(ctx context.Context, tx pgx.Tx, refs []uuid.UUID) (map[uuid.UUID]string, error) {
	out := map[uuid.UUID]string{}
	if len(refs) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, `SELECT household_ref, coalesce(native_code, '') FROM pf_coop.household WHERE household_ref = ANY($1::uuid[])`, uuidStrings(refs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var r uuid.UUID
		var c string
		if err := rows.Scan(&r, &c); err != nil {
			return nil, err
		}
		out[r] = c
	}
	return out, rows.Err()
}

func uuidStrings(refs []uuid.UUID) []string {
	out := make([]string, len(refs))
	for i, r := range refs {
		out[i] = r.String()
	}
	return out
}
