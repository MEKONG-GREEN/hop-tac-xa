package app

// AH-87 (FN-04.2, FN-04.3, FN-04-BR-02/03, KH-05, KH-26): sản lượng lúa, rơm dự kiến và ngày thu hoạch dự kiến của vụ
// theo hộ.
//
//   - Lúc mở vụ: chụp năng suất quy ước (tham số dùng chung, mặc định 6 t/ha), cờ lúa của loại cây và hệ số rơm theo
//     giống (FN-11, mặc định 35 %), ghi dòng "computed" trong cùng giao dịch mở vụ.
//   - Thành viên HTX nhập, sửa hoặc xóa giá trị của mình (KHMV-FN-04 U) khi vụ đang canh tác: dòng "entered". Rơm hệ
//     thống tính lại theo lúa đang có hiệu lực, hệ số rơm vẫn là hệ số đã chụp; rơm Thành viên nhập không bị đổi.
//   - Sửa vụ (AH-175) đổi diện tích hoặc giống: dòng "recomputed" (tham số đã chụp, hệ số rơm theo giống mới).
//   - Vụ mở trước AH-87 (chưa có dòng): đọc thì tính tại chỗ theo tham số hiện hành, không ghi; lần ghi đầu tiên mới
//     chụp tham số.
//   - Ngày thu hoạch dự kiến dùng expected_harvest_date đã lưu của vụ (AC4), không tính lại ở đây.
//
// Không gọi port module khác trong giao dịch: tham số và danh mục loại cây đọc trước; hệ số rơm cùng module đọc trong
// giao dịch.

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/store"
	mdport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
)

// ErrEstimateChanged: sản lượng dự kiến vừa được người khác sửa.
var ErrEstimateChanged = problem.New(http.StatusConflict, "cropcycle.yield_estimate_changed", "Sản lượng dự kiến vừa thay đổi; tải lại trước khi sửa")

// YieldEstimateView là sản lượng dự kiến trả cho client. Seq = 0 khi vụ chưa có dòng trong sổ (tính tại chỗ).
type YieldEstimateView struct {
	domain.YieldEstimate
	Seq       int64
	UpdatedAt *time.Time
	UpdatedBy *uuid.UUID
}

func estimateView(e store.YieldEstimateEvent) *YieldEstimateView {
	at, by := e.EventTime, e.ActorRef
	return &YieldEstimateView{YieldEstimate: e.Estimate, Seq: e.Seq, UpdatedAt: &at, UpdatedBy: &by}
}

// estimateParams là phần đọc qua port trước giao dịch.
type estimateParams struct {
	Base   decimal.Decimal
	IsRice bool
}

// yieldEstimateParams đọc năng suất quy ước hiện hành và cờ lúa của loại cây. Lỗi phụ thuộc trả ErrDependency
// (fail-closed: không tự đặt giá trị khi không đọc được danh mục).
func (s *Service) yieldEstimateParams(ctx context.Context, crop string, at time.Time) (estimateParams, error) {
	if s.dependencyTxOpen(ctx) || s.Catalogs == nil {
		return estimateParams{}, ErrDependency
	}
	base, _, err := s.param(ctx, port.ParamConventionalYieldTPerHa, at, domain.DefaultConventionalYield)
	if err != nil {
		return estimateParams{}, err
	}
	if base.IsNegative() {
		base = domain.DefaultConventionalYield
	}
	it, err := s.Catalogs.Get(ctx, mdport.CatalogCrop, crop)
	if err != nil && !errors.Is(err, mdport.ErrCatalogItemNotFound) {
		return estimateParams{}, ErrDependency.WithDetail("không tra được danh mục loại cây").WithCause(err)
	}
	return estimateParams{Base: base, IsRice: err == nil && it.Attrs[mdport.AttrIsRice] == "true"}, nil
}

// strawPctTx đọc hệ số rơm của giống trong giao dịch (cùng module); giống chưa có hệ số thì mặc định 35 %.
func strawPctTx(ctx context.Context, tx pgx.Tx, variety string) (*decimal.Decimal, error) {
	pct := domain.DefaultStrawPct
	if variety != "" {
		f, err := store.StrawFactorAt(ctx, tx, variety, time.Time{})
		if err != nil {
			return nil, err
		}
		if f != nil {
			pct = f.Percentage
		}
	}
	return &pct, nil
}

// computeEstimateTx tính sản lượng dự kiến của vụ từ tham số đã đọc (không ghi).
func computeEstimateTx(ctx context.Context, tx pgx.Tx, season store.Season, p estimateParams, enteredRice, enteredStraw *decimal.Decimal) (domain.YieldEstimate, error) {
	var straw *decimal.Decimal
	if p.IsRice {
		var err error
		if straw, err = strawPctTx(ctx, tx, season.VarietyCode); err != nil {
			return domain.YieldEstimate{}, err
		}
	}
	return domain.NewYieldEstimate(season.AreaHa, p.Base, straw, enteredRice, enteredStraw), nil
}

// appendEstimateTx ghi một dòng vào sổ sản lượng dự kiến của vụ.
func appendEstimateTx(ctx context.Context, tx pgx.Tx, tid uuid.UUID, season store.Season, action string, est domain.YieldEstimate,
	actor uuid.UUID, at time.Time, key string) (store.YieldEstimateEvent, error) {
	e := store.YieldEstimateEvent{ID: ids.New(), TenantID: tid, SeasonRef: season.SeasonRef, CooperativeRef: season.CooperativeRef,
		HouseholdRef: season.HouseholdRef, ActorRef: actor, Action: action, Estimate: est, EventTime: at, IdemKey: key}
	err := store.InsertYieldEstimate(ctx, tx, &e)
	return e, err
}

// recomputeEstimateOnEditTx: sửa vụ (AH-175) đổi diện tích hoặc giống thì tính lại giá trị hệ thống theo tham số đã
// chụp; giữ giá trị nhập. Vụ chưa có dòng trong sổ giữ cách đọc tại chỗ.
func recomputeEstimateOnEditTx(ctx context.Context, tx pgx.Tx, tid uuid.UUID, before, after store.Season, actor uuid.UUID, at time.Time, key string) error {
	if after.AreaHa.Equal(before.AreaHa) && after.VarietyCode == before.VarietyCode {
		return nil
	}
	cur, err := store.CurrentYieldEstimate(ctx, tx, after.SeasonRef)
	if err != nil || cur == nil {
		return err
	}
	straw := cur.Estimate.StrawPct
	if cur.Estimate.IsRice && after.VarietyCode != before.VarietyCode {
		if straw, err = strawPctTx(ctx, tx, after.VarietyCode); err != nil {
			return err
		}
	}
	est := domain.NewYieldEstimate(after.AreaHa, cur.Estimate.BaseYield, straw, cur.Estimate.EnteredRice, cur.Estimate.EnteredStraw)
	_, err = appendEstimateTx(ctx, tx, tid, after, store.YieldEstimateRecomputed, est, actor, at, "yield-estimate:"+key)
	return err
}

// attachEstimates gắn sản lượng dự kiến vào kết quả vụ theo hộ. Vụ chưa có dòng trong sổ được tính tại chỗ theo
// tham số hiện hành; không đọc được tham số thì để trống (không bịa số).
func (s *Service) attachEstimates(ctx context.Context, tid uuid.UUID, items []SeasonResult) error {
	if len(items) == 0 {
		return nil
	}
	refs := make([]uuid.UUID, len(items))
	for i, r := range items {
		refs[i] = r.Season.SeasonRef
	}
	var stored map[uuid.UUID]store.YieldEstimateEvent
	if err := s.inTx(ctx, tid, func(tx pgx.Tx) error {
		var err error
		stored, err = store.CurrentYieldEstimates(ctx, tx, refs)
		return err
	}); err != nil {
		return err
	}
	params := map[string]*estimateParams{}
	var missing []int
	for i := range items {
		if e, ok := stored[items[i].Season.SeasonRef]; ok {
			items[i].Estimate = estimateView(e)
			continue
		}
		crop := items[i].Season.CropCode
		if _, done := params[crop]; !done {
			params[crop] = nil
			if p, err := s.yieldEstimateParams(ctx, crop, s.now()); err == nil {
				params[crop] = &p
			}
		}
		if params[crop] != nil {
			missing = append(missing, i)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return s.inTx(ctx, tid, func(tx pgx.Tx) error {
		for _, i := range missing {
			est, err := computeEstimateTx(ctx, tx, items[i].Season, *params[items[i].Season.CropCode], nil, nil)
			if err != nil {
				return err
			}
			items[i].Estimate = &YieldEstimateView{YieldEstimate: est}
		}
		return nil
	})
}

// YieldEstimateInput là giá trị Thành viên HTX nhập cho cả vụ; nil = dùng giá trị hệ thống tính.
type YieldEstimateInput struct {
	CooperativeRef, HouseholdRef, SeasonRef uuid.UUID
	ExpectedSeq                             int64
	RiceT, StrawT                           *string
	IdemKey                                 string
}

// YieldEstimateResult là kết quả một lần ghi.
type YieldEstimateResult struct {
	Estimate YieldEstimateView
	Replayed bool
}

func sameDecimalPtr(a, b *decimal.Decimal) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}

// SaveYieldEstimate ghi giá trị Thành viên HTX nhập (KHMV-FN-04 U). Chỉ vụ theo hộ đang canh tác; Đại diện HTX chỉ
// xem (403). Phiên bản (expected_seq) phải khớp bản hiện hành; phát lại cùng Idempotency-Key trả kết quả đã ghi.
func (s *Service) SaveYieldEstimate(ctx context.Context, in YieldEstimateInput) (YieldEstimateResult, error) {
	if _, err := s.matrixAccess(ctx, fnPlan, htxroles.Update); err != nil {
		return YieldEstimateResult{}, err
	}
	a, err := s.householdAccess(ctx, in.CooperativeRef, true)
	if err != nil {
		return YieldEstimateResult{}, err
	}
	if in.IdemKey == "" {
		return YieldEstimateResult{}, problem.IdempotencyRequired
	}
	if in.ExpectedSeq < 0 {
		return YieldEstimateResult{}, problem.Validation.WithField("expected_seq", "Phiên bản sản lượng dự kiến không hợp lệ")
	}
	var rice, straw *decimal.Decimal
	fields := domain.FieldErrors{}
	for _, f := range []struct {
		name string
		raw  *string
		dst  **decimal.Decimal
	}{{"rice_t", in.RiceT, &rice}, {"straw_t", in.StrawT, &straw}} {
		if f.raw == nil || *f.raw == "" {
			continue
		}
		d, err := domain.ParseTonnes(*f.raw)
		if err != nil {
			fields = append(fields, domain.FieldError{Field: f.name, Reason: err.Error()})
			continue
		}
		*f.dst = &d
	}
	if len(fields) > 0 {
		return YieldEstimateResult{}, fieldProblem(fields)
	}
	key := "yield-entered:" + a.p.ActorRef.String() + ":" + in.IdemKey
	replayed := func(old *store.YieldEstimateEvent) (YieldEstimateResult, error) {
		if old.SeasonRef != in.SeasonRef || old.ActorRef != a.p.ActorRef || old.Action != store.YieldEstimateEntered ||
			old.Seq-1 != in.ExpectedSeq || !sameDecimalPtr(old.Estimate.EnteredRice, rice) || !sameDecimalPtr(old.Estimate.EnteredStraw, straw) {
			return YieldEstimateResult{}, problem.IdempotencyConflict
		}
		return YieldEstimateResult{Estimate: *estimateView(*old), Replayed: true}, nil
	}
	// Đọc trước giao dịch: phát lại, vụ, bản hiện hành. Vụ chưa có dòng trong sổ cần tham số qua port (ngoài giao dịch).
	var pre *store.Season
	var old, current *store.YieldEstimateEvent
	if err := s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		var e error
		if pre, e = householdTaskSeason(ctx, tx, a.p.TenantID, in.CooperativeRef, in.HouseholdRef, in.SeasonRef, false); e != nil {
			return e
		}
		if old, e = store.YieldEstimateByIdem(ctx, tx, key); e != nil || old != nil {
			return e
		}
		current, e = store.CurrentYieldEstimate(ctx, tx, in.SeasonRef)
		return e
	}); err != nil {
		return YieldEstimateResult{}, err
	}
	if old != nil {
		return replayed(old)
	}
	var params *estimateParams
	if current == nil {
		p, err := s.yieldEstimateParams(ctx, pre.CropCode, s.now())
		if err != nil {
			return YieldEstimateResult{}, err
		}
		params = &p
	}
	var out YieldEstimateResult
	err = s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		cur, err := householdTaskSeason(ctx, tx, a.p.TenantID, in.CooperativeRef, in.HouseholdRef, in.SeasonRef, true)
		if err != nil {
			return err
		}
		if old, err := store.YieldEstimateByIdem(ctx, tx, key); err != nil {
			return err
		} else if old != nil {
			out, err = replayed(old)
			return err
		}
		if cur.Status != port.SeasonOpen {
			return ErrSeasonState.WithDetail("Chỉ sửa sản lượng dự kiến khi vụ đang canh tác")
		}
		latest, err := store.CurrentYieldEstimate(ctx, tx, in.SeasonRef)
		if err != nil {
			return err
		}
		var seq int64
		if latest != nil {
			seq = latest.Seq
		}
		if seq != in.ExpectedSeq {
			return ErrEstimateChanged.With("current_seq", seq)
		}
		var est domain.YieldEstimate
		switch {
		case latest != nil:
			est = latest.Estimate
			est.EnteredRice, est.EnteredStraw = rice, straw
		case params != nil:
			if est, err = computeEstimateTx(ctx, tx, *cur, *params, rice, straw); err != nil {
				return err
			}
		default:
			// Bản hiện hành vừa bị xóa khỏi tầm nhìn giữa hai lần đọc: không thể xảy ra với sổ chỉ ghi thêm.
			return ErrEstimateChanged
		}
		// Trả lời dev-agent 23/11/2026, câu 4: gửi rơm cho vụ không phải lúa thì từ chối cả lệnh.
		if !est.IsRice && straw != nil {
			return problem.Validation.WithField("straw_t", "Vụ không phải lúa: không có sản lượng rơm dự kiến")
		}
		est.Recompute()
		now := s.now()
		e, err := appendEstimateTx(ctx, tx, a.p.TenantID, *cur, store.YieldEstimateEntered, est, a.p.ActorRef, now, key)
		if err != nil {
			return err
		}
		if err := store.RefreshYield(ctx, tx, cur.SeasonRef); err != nil {
			return err
		}
		out = YieldEstimateResult{Estimate: *estimateView(e)}
		return nil
	})
	return out, err
}
