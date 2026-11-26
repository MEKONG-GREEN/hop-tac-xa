package app

// AH-177 (FN-06.2, FN-06-EX-03, UX-11): Thành viên HTX, Admin khai báo sản lượng nhiều vụ theo hộ một lần — bảng nhập
// nhanh ("Lưu tất cả") hoặc tệp Excel mẫu. Mỗi dòng áp đúng quy tắc khai báo một vụ của AH-100 trong giao dịch riêng;
// dòng lỗi bị bỏ qua kèm lý do, dòng hợp lệ vẫn lưu. Phạm vi theo HTX như khai báo một vụ (AC4). Tệp được đọc và kiểm
// trọn trước khi mở giao dịch ghi. Một Idempotency-Key cho mỗi lần gửi; mỗi dòng dùng khóa con theo mã vụ.

import (
	"context"
	"errors"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/store"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tabular"
)

// HarvestBatchRow là một dòng khai báo hàng loạt của vụ theo hộ.
type HarvestBatchRow struct {
	Line            int
	SeasonRef       string
	ExpectedEventID string // trống = không kiểm phiên bản (dòng Excel)
	MassT           string
	HarvestDate     string // YYYY-MM-DD hoặc dd/mm/yyyy
	ZeroConfirmed   bool
}

// HarvestBatchRowResult là kết quả một dòng. Reason không chứa dữ liệu cá nhân.
type HarvestBatchRowResult struct {
	Line         int
	SeasonRef    *uuid.UUID
	Status       string // accepted | duplicate | rejected
	Code, Reason string
	NeedsConfirm bool
	HarvestID    *uuid.UUID
}

// HarvestBatchResult là kết quả cả lượt.
type HarvestBatchResult struct {
	Accepted, Duplicate, Rejected int
	Rows                          []HarvestBatchRowResult
}

// ParseHarvestDay nhận YYYY-MM-DD hoặc dd/mm/yyyy (ô ngày của Excel đã được đổi sang YYYY-MM-DD khi đọc tệp).
func ParseHarvestDay(raw string) (time.Time, error) {
	v := strings.TrimSpace(raw)
	if d, err := domain.ParseDay(v); err == nil {
		return d, nil
	}
	parts := strings.Split(v, "/")
	if len(parts) == 3 && len(parts[0]) >= 1 && len(parts[0]) <= 2 && len(parts[1]) >= 1 && len(parts[1]) <= 2 && len(parts[2]) == 4 {
		day, e1 := strconv.Atoi(parts[0])
		month, e2 := strconv.Atoi(parts[1])
		year, e3 := strconv.Atoi(parts[2])
		if e1 == nil && e2 == nil && e3 == nil {
			t := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
			if t.Day() == day && int(t.Month()) == month && t.Year() == year {
				return t, nil
			}
		}
	}
	return time.Time{}, errors.New("ngày không hợp lệ")
}

// HouseholdHarvestBatch khai báo sản lượng nhiều vụ theo hộ (KHMV-FN-06 C). source: web (bảng) hoặc excel (tệp).
func (s *Service) HouseholdHarvestBatch(ctx context.Context, coop uuid.UUID, rows []HarvestBatchRow, source, idemKey string) (HarvestBatchResult, error) {
	if _, err := s.matrixAccess(ctx, fnHarvest, htxroles.Create); err != nil {
		return HarvestBatchResult{}, err
	}
	a, err := s.householdAccess(ctx, coop, true)
	if err != nil {
		return HarvestBatchResult{}, err
	}
	if idemKey == "" {
		return HarvestBatchResult{}, problem.IdempotencyRequired
	}
	if len(rows) == 0 || len(rows) > MaxImportRows {
		return HarvestBatchResult{}, problem.Validation.WithField("rows", "1–200 dòng")
	}
	if source != SourceWeb && source != SourceExcel {
		return HarvestBatchResult{}, problem.Validation.WithField("source", "web hoặc excel")
	}
	refs := []uuid.UUID{}
	for _, r := range rows {
		if u, err := uuid.Parse(strings.TrimSpace(r.SeasonRef)); err == nil {
			refs = append(refs, u)
		}
	}
	var seasons map[uuid.UUID]store.Season
	if err := s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		var err error
		seasons, err = store.HouseholdSeasonsByRefs(ctx, tx, coop, refs)
		return err
	}); err != nil {
		return HarvestBatchResult{}, err
	}
	res := HarvestBatchResult{Rows: make([]HarvestBatchRowResult, 0, len(rows))}
	seen := map[uuid.UUID]int{}
	for i, r := range rows {
		if r.Line == 0 {
			r.Line = i + 1
		}
		out, err := s.harvestBatchRow(ctx, a, coop, r, source, idemKey, seasons, seen)
		if err != nil {
			return HarvestBatchResult{}, err
		}
		switch out.Status {
		case RowAccepted:
			res.Accepted++
		case RowDuplicate:
			res.Duplicate++
		default:
			res.Rejected++
		}
		res.Rows = append(res.Rows, out)
	}
	return res, nil
}

func (s *Service) harvestBatchRow(ctx context.Context, a access, coop uuid.UUID, r HarvestBatchRow, source, idemKey string,
	seasons map[uuid.UUID]store.Season, seen map[uuid.UUID]int) (HarvestBatchRowResult, error) {
	out := HarvestBatchRowResult{Line: r.Line}
	reject := func(code, reason string) (HarvestBatchRowResult, error) {
		out.Status, out.Code, out.Reason = RowRejected, code, reason
		return out, nil
	}
	ref, err := uuid.Parse(strings.TrimSpace(r.SeasonRef))
	if err != nil {
		return reject(problem.Validation.Code, "Thiếu hoặc sai mã vụ")
	}
	out.SeasonRef = &ref
	if line, dup := seen[ref]; dup {
		return reject(problem.Validation.Code, "Vụ trùng với dòng "+strconv.Itoa(line)+" trong cùng lượt")
	}
	seen[ref] = r.Line
	sn, ok := seasons[ref]
	if !ok {
		return reject(ErrSeasonNotFound.Code, "Mã vụ không thuộc HTX hoặc không phải vụ nông hộ")
	}
	mass := strings.TrimSpace(r.MassT)
	if mass == "" {
		return reject(problem.Validation.Code, "Thiếu sản lượng (tấn)")
	}
	if !strings.Contains(mass, ".") {
		mass = strings.Replace(mass, ",", ".", 1)
	}
	if strings.TrimSpace(r.HarvestDate) == "" {
		return reject(problem.Validation.Code, "Thiếu ngày thu hoạch cuối")
	}
	day, err := ParseHarvestDay(r.HarvestDate)
	if err != nil {
		return reject(problem.Validation.Code, "Ngày thu hoạch cuối phải có dạng dd/mm/yyyy hoặc YYYY-MM-DD")
	}
	in := HouseholdHarvestInput{CooperativeRef: coop, HouseholdRef: sn.HouseholdRef, SeasonRef: ref, MassT: mass, HarvestDate: day,
		ZeroConfirmed: r.ZeroConfirmed, GPS: domain.GPS{Source: "no_device"}, Channel: ChannelWeb, Source: source,
		IdemKey: idemKey + ":" + ref.String(), AnyEvent: true}
	if v := strings.TrimSpace(r.ExpectedEventID); v != "" {
		ev, err := uuid.Parse(v)
		if err != nil {
			return reject(problem.Validation.Code, "Phiên bản vụ không hợp lệ")
		}
		in.ExpectedEventID = ev
	}
	h, err := s.declareHouseholdHarvest(ctx, a, in)
	if err != nil {
		pr := problem.From(err)
		if pr.Status >= 500 {
			return HarvestBatchRowResult{}, err
		}
		reason := rowReason(pr)
		switch pr.Code {
		case ErrSeasonState.Code:
			reason = "Vụ không còn \"Đang canh tác\" (" + seasonStatusLabel(sn.Status) + "); vụ đã khai báo thì dùng Sửa sản lượng"
		case ErrTaskChanged.Code:
			reason = "Vụ vừa thay đổi ở nơi khác; tải lại danh sách rồi nhập lại"
		case ErrHarvestZeroConfirm.Code:
			out.NeedsConfirm = true
			reason = "Sản lượng bằng 0: đánh dấu xác nhận rồi lưu lại"
		case problem.IdempotencyConflict.Code:
			reason = "Dòng đã gửi với nội dung khác trong lần lưu trước; tải lại danh sách"
		}
		return reject(pr.Code, reason)
	}
	out.Status, out.HarvestID = RowAccepted, &h.Harvest.ID
	if h.Replayed {
		out.Status = RowDuplicate
	}
	return out, nil
}

func seasonStatusLabel(status string) string {
	switch status {
	case port.SeasonOpen:
		return "Đang canh tác"
	case port.SeasonCompleted:
		return "Đã thu hoạch"
	case port.SeasonClosed:
		return "Đã hoàn thành vụ"
	case port.SeasonCancelled:
		return "Đã dừng vụ"
	}
	return status
}

// Cột của tệp mẫu khai báo sản lượng vụ nông hộ (tiêu đề chuẩn hóa bằng tabular.NormalizeHeader).
var householdImportAliases = map[string]string{
	"ma_vu": "season_ref", "season_ref": "season_ref",
	"san_luong_tan": "mass_t", "san_luong": "mass_t", "mass_t": "mass_t", "tan": "mass_t",
	"ngay_thu_hoach_cuoi": "harvest_date", "ngay_thu_hoach": "harvest_date", "harvest_date": "harvest_date",
	"xac_nhan_0": "zero_confirmed", "xac_nhan_san_luong_0": "zero_confirmed", "zero_confirmed": "zero_confirmed",
}

// ImportHouseholdHarvests nhập sản lượng vụ nông hộ từ .xlsx (hoặc CSV UTF-8). Tệp sai mẫu bị từ chối toàn bộ; dòng
// trống (không có sản lượng, ngày, xác nhận) bị bỏ qua; dòng lỗi trả kèm lý do, chỉ dòng hợp lệ được ghi.
func (s *Service) ImportHouseholdHarvests(ctx context.Context, coop uuid.UUID, src io.Reader, idemKey string) (HarvestBatchResult, error) {
	if _, err := s.matrixAccess(ctx, fnHarvest, htxroles.Create); err != nil {
		return HarvestBatchResult{}, err
	}
	if _, err := s.householdAccess(ctx, coop, true); err != nil {
		return HarvestBatchResult{}, err
	}
	if idemKey == "" {
		return HarvestBatchResult{}, problem.IdempotencyRequired
	}
	lim := tabular.Limits{MaxBytes: MaxImportBytes, MaxRows: MaxImportRows}
	tb, err := tabular.Read(src, lim)
	if err != nil {
		var fe *tabular.FormulaError
		if errors.As(err, &fe) || errors.Is(err, tabular.ErrTooManyRows) || errors.Is(err, tabular.ErrTooLarge) ||
			errors.Is(err, tabular.ErrTooManyColumns) || errors.Is(err, tabular.ErrEncoding) || errors.Is(err, tabular.ErrLegacyExcel) ||
			errors.Is(err, tabular.ErrEmpty) || errors.Is(err, tabular.ErrFormat) {
			return HarvestBatchResult{}, ErrImportInvalid.WithDetail(tabular.Message(err, lim))
		}
		return HarvestBatchResult{}, err
	}
	col := tb.Columns(householdImportAliases)
	if miss := tabular.Missing(col, "season_ref", "mass_t", "harvest_date"); len(miss) > 0 {
		return HarvestBatchResult{}, ErrImportInvalid.WithDetail("Thiếu cột Mã vụ, Sản lượng (tấn) hoặc Ngày thu hoạch cuối — dùng tệp mẫu")
	}
	rows := make([]HarvestBatchRow, 0, len(tb.Rows))
	for _, r := range tb.Rows {
		mass, day, zero := r.Get(col, "mass_t"), r.Get(col, "harvest_date"), strings.ToLower(strings.TrimSpace(r.Get(col, "zero_confirmed")))
		if strings.TrimSpace(mass) == "" && strings.TrimSpace(day) == "" && zero == "" {
			continue // dòng của tệp mẫu chưa nhập: không phải lỗi
		}
		rows = append(rows, HarvestBatchRow{Line: r.Line, SeasonRef: r.Get(col, "season_ref"), MassT: mass, HarvestDate: day,
			ZeroConfirmed: zero == "x" || zero == "1" || zero == "true" || zero == "co" || zero == "có"})
	}
	if len(rows) == 0 {
		return HarvestBatchResult{}, ErrImportInvalid.WithDetail("Tệp chưa có dòng nào nhập sản lượng")
	}
	return s.HouseholdHarvestBatch(ctx, coop, rows, SourceExcel, idemKey)
}

// HarvestTemplateRow là một dòng của tệp mẫu (vụ chờ khai báo).
type HarvestTemplateRow struct {
	Season        store.Season
	HouseholdCode string
}

// HarvestTemplate liệt kê vụ chờ khai báo cho tệp mẫu (KHMV-FN-06 C): mặc định chỉ vụ đã ghi việc Thu hoạch (KH-20),
// all = mọi vụ đang canh tác. Tối đa MaxImportRows vụ, theo ngày thu hoạch dự kiến.
func (s *Service) HarvestTemplate(ctx context.Context, coop uuid.UUID, all bool) ([]HarvestTemplateRow, error) {
	if _, err := s.matrixAccess(ctx, fnHarvest, htxroles.Create); err != nil {
		return nil, err
	}
	a, err := s.householdAccess(ctx, coop, true)
	if err != nil {
		return nil, err
	}
	out := []HarvestTemplateRow{}
	err = s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		rows, err := store.HouseholdYieldSeasons(ctx, tx, store.YieldFilter{CooperativeRef: coop, HarvestRecordedOnly: !all}, nil, MaxImportRows)
		if err != nil {
			return err
		}
		hh := make([]uuid.UUID, 0, len(rows))
		for _, r := range rows {
			hh = append(hh, r.HouseholdRef)
		}
		codes, err := store.HouseholdCodes(ctx, tx, hh)
		if err != nil {
			return err
		}
		for _, r := range rows {
			out = append(out, HarvestTemplateRow{Season: r, HouseholdCode: codes[r.HouseholdRef]})
		}
		return nil
	})
	return out, err
}
