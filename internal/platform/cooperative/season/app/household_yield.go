package app

// AH-153 (FN-06.6, DATA-03): Đại diện HTX (và Thành viên, Admin — KHMV-FN-06 R) xem chỉ đọc sản lượng thực tế so với dự
// kiến của vụ nông hộ trong HTX: nhóm chờ khai báo (vụ đang canh tác) và nhóm đã khai báo (đã thu hoạch, đã hoàn thành
// vụ); lọc theo Thành viên mở vụ và mùa vụ; phân trang theo ngày (NFR-14); tổng hợp trên toàn bộ bộ lọc. Phạm vi theo
// HTX (KH-02). Tham số năng suất quy ước và tên người mở vụ đọc qua cổng NGOÀI giao dịch.

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/store"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
)

// HouseholdYieldQuery là bộ lọc của màn sản lượng thực tế so với dự kiến.
type HouseholdYieldQuery struct {
	CooperativeRef uuid.UUID
	State          string // pending | declared
	OpenedBy       uuid.UUID
	SeasonCode     string
	SeasonYear     int
	After          *store.YieldCursor
	Limit          int
}

// HouseholdYieldItem là một vụ kèm dự kiến, thực tế và chênh lệch.
type HouseholdYieldItem struct {
	SeasonResult
	OpenedByLabel string
	ExpectedT     *decimal.Decimal
	ActualT       *decimal.Decimal
	DiffT         *decimal.Decimal
	DiffPct       *decimal.Decimal
	Overdue       bool
}

// HouseholdYieldTotals cộng trên toàn bộ bộ lọc (không chỉ trang). Tổng dự kiến/thực tế của nhóm đã khai báo chỉ tính
// vụ có đủ cả hai giá trị; vụ thiếu dự kiến đếm riêng.
type HouseholdYieldTotals struct {
	Pending, Declared, DeclaredWithoutEstimate int
	PendingExpectedT                           decimal.Decimal
	ExpectedT, ActualT                         decimal.Decimal
	DiffT                                      decimal.Decimal
	DiffPct                                    *decimal.Decimal
}

// HouseholdYieldPage là một trang kết quả.
type HouseholdYieldPage struct {
	Items  []HouseholdYieldItem
	Next   *store.YieldCursor
	Totals HouseholdYieldTotals
}

// HouseholdYields đọc vụ nông hộ của HTX kèm sản lượng thực tế so với dự kiến (KHMV-FN-06 R).
func (s *Service) HouseholdYields(ctx context.Context, q HouseholdYieldQuery) (HouseholdYieldPage, error) {
	if _, err := s.matrixAccess(ctx, fnHarvest, htxroles.Read); err != nil {
		return HouseholdYieldPage{}, err
	}
	a, err := s.householdAccess(ctx, q.CooperativeRef, false)
	if err != nil {
		return HouseholdYieldPage{}, err
	}
	if q.State != "pending" && q.State != "declared" {
		return HouseholdYieldPage{}, problem.Validation.WithField("state", "pending hoặc declared")
	}
	if q.SeasonYear != 0 && (q.SeasonYear < 2000 || q.SeasonYear > 2100) {
		return HouseholdYieldPage{}, problem.Validation.WithField("season_year", "năm không hợp lệ")
	}
	if len(q.SeasonCode) > 32 {
		return HouseholdYieldPage{}, problem.Validation.WithField("season_code", "mã mùa vụ không hợp lệ")
	}
	limit := clampLimit(q.Limit)
	f := store.YieldFilter{CooperativeRef: q.CooperativeRef, Declared: q.State == "declared", OpenedBy: q.OpenedBy, SeasonCode: q.SeasonCode, SeasonYear: q.SeasonYear}
	page := HouseholdYieldPage{Items: []HouseholdYieldItem{}}
	var rows []store.Season
	var facts []store.YieldFact
	var stored map[uuid.UUID]store.YieldEstimateEvent
	err = s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		var err error
		if rows, err = store.HouseholdYieldSeasons(ctx, tx, f, q.After, limit+1); err != nil {
			return err
		}
		if facts, err = store.HouseholdYieldFacts(ctx, tx, f); err != nil {
			return err
		}
		refs := make([]uuid.UUID, len(facts))
		for i, y := range facts {
			refs[i] = y.SeasonRef
		}
		stored, err = store.CurrentYieldEstimates(ctx, tx, refs)
		return err
	})
	if err != nil {
		return HouseholdYieldPage{}, err
	}
	more := len(rows) > limit
	if more {
		rows = rows[:limit]
	}
	items := make([]SeasonResult, len(rows))
	for i, r := range rows {
		items[i] = SeasonResult{Season: r}
	}
	if err := s.attachEstimates(ctx, a.p.TenantID, items); err != nil {
		return HouseholdYieldPage{}, err
	}
	today := businessDay(s.now())
	for _, r := range items {
		it := HouseholdYieldItem{SeasonResult: r}
		if r.Estimate != nil {
			e := r.Estimate.Rice()
			it.ExpectedT = &e
		}
		if r.Harvest != nil {
			t := domain.TonnesFromKg(r.Harvest.MassKg.Decimal())
			it.ActualT = &t
		}
		it.DiffT, it.DiffPct = domain.CompareYield(it.ExpectedT, it.ActualT)
		it.Overdue = !f.Declared && !r.Season.ExpectedHarvestDate.IsZero() && today.After(r.Season.ExpectedHarvestDate)
		page.Items = append(page.Items, it)
	}
	if more && len(page.Items) > 0 {
		last := page.Items[len(page.Items)-1]
		c := store.YieldCursor{SeasonRef: last.Season.SeasonRef, Day: store.NoExpectedDay}
		switch {
		case f.Declared && last.Harvest != nil:
			c.Day = last.Harvest.HarvestDate
		case !f.Declared && !last.Season.ExpectedHarvestDate.IsZero():
			c.Day = last.Season.ExpectedHarvestDate
		}
		page.Next = &c
	}
	page.Totals = s.yieldTotals(ctx, facts, stored)
	s.attachOpenerLabels(ctx, page.Items)
	return page, nil
}

// yieldTotals cộng dự kiến, thực tế trên mọi vụ khớp bộ lọc. Vụ mở trước AH-87 (chưa có dòng sổ dự kiến) tính tại chỗ
// theo năng suất quy ước hiện hành; không đọc được tham số thì vụ đó coi như thiếu dự kiến.
func (s *Service) yieldTotals(ctx context.Context, facts []store.YieldFact, stored map[uuid.UUID]store.YieldEstimateEvent) HouseholdYieldTotals {
	t := HouseholdYieldTotals{}
	params := map[string]*estimateParams{}
	expected := func(y store.YieldFact) *decimal.Decimal {
		if e, ok := stored[y.SeasonRef]; ok {
			v := e.Estimate.Rice()
			return &v
		}
		if _, done := params[y.CropCode]; !done {
			params[y.CropCode] = nil
			if p, err := s.yieldEstimateParams(ctx, y.CropCode, s.now()); err == nil {
				params[y.CropCode] = &p
			}
		}
		if p := params[y.CropCode]; p != nil {
			v := domain.NewYieldEstimate(y.AreaHa, p.Base, nil, nil, nil).Rice()
			return &v
		}
		return nil
	}
	for _, y := range facts {
		e := expected(y)
		if !y.Declared {
			t.Pending++
			if e != nil {
				t.PendingExpectedT = t.PendingExpectedT.Add(*e)
			}
			continue
		}
		t.Declared++
		if e == nil || y.MassKg == nil {
			t.DeclaredWithoutEstimate++
			continue
		}
		t.ExpectedT = t.ExpectedT.Add(*e)
		t.ActualT = t.ActualT.Add(domain.TonnesFromKg(*y.MassKg))
	}
	if diff, pct := domain.CompareYield(&t.ExpectedT, &t.ActualT); diff != nil {
		t.DiffT, t.DiffPct = *diff, pct
	}
	return t
}

// attachOpenerLabels gắn tên hiển thị người mở vụ (thiếu danh bạ vẫn trả danh sách).
func (s *Service) attachOpenerLabels(ctx context.Context, items []HouseholdYieldItem) {
	if s.Names == nil || len(items) == 0 {
		return
	}
	refs := []uuid.UUID{}
	seen := map[uuid.UUID]bool{}
	for _, it := range items {
		if r := it.Season.OpenedBy; r != uuid.Nil && !seen[r] {
			seen[r] = true
			refs = append(refs, r)
		}
	}
	labels, err := s.Names.DisplayNames(ctx, refs)
	if err != nil {
		return
	}
	for i := range items {
		items[i].OpenedByLabel = labels[items[i].Season.OpenedBy]
	}
}
