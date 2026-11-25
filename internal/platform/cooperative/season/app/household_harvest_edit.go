package app

// AH-189 (FN-06.3, FN-06.4, FN-06-BS-01/02, EX-07/08, BR-04): Thành viên HTX sửa sản lượng đã khai hoặc hủy khai báo
// nhầm của vụ theo hộ; lịch sử chỉ ghi thêm.
//
//   - Sửa: bản bù trên sổ harvest_declaration (cũ → mới, người, thời điểm). Kiểm như lúc khai báo (Q-15). Vụ "Đã thu
//     hoạch" lý do tùy chọn (bỏ trống thì ghi câu chuẩn vì sổ đòi lý do); trạng thái đóng sau này (AH-152) bắt buộc lý
//     do. Đổi ngày thu hoạch thì việc Thu hoạch tự Xong (auto_harvest) đổi theo; việc người dùng tự tích giữ nguyên.
//   - Hủy: chỉ vụ "Đã thu hoạch" (AC4), lý do bắt buộc. Kiểm diện tích dưới khóa hộ (AC3). Ghi dòng hủy (voided) và
//     sự kiện reopened: vụ về "Đang canh tác"; việc Thu hoạch tự Xong về trạng thái trước (bản tạo mới thì xóa mềm, bản
//     từng Bỏ qua thì khôi phục Bỏ qua); việc NTT chưa xong về trạng thái trước khai báo (Q-16).
//
// Không gọi port module khác trong giao dịch.

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	memberstore "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/member/store"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/store"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/mass"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
)

const (
	// defaultHarvestEditReason: sổ khai báo đòi lý do ≥ 10 ký tự cho bản bù; vụ "Đã thu hoạch" không bắt buộc người
	// dùng nhập (AH-189 AC1) nên máy chủ ghi câu chuẩn.
	defaultHarvestEditReason = "Thành viên HTX sửa sản lượng"
	moveAutoHarvestReason    = "Đổi theo ngày thu hoạch đã sửa"
)

// harvestEditable: trạng thái vụ được sửa sản lượng. AH-152 thêm trạng thái "Đã hoàn thành vụ" (bắt buộc lý do).
var harvestEditable = map[string]bool{port.SeasonCompleted: true}

// harvestEditReasonRequired: sửa ở trạng thái khác "Đã thu hoạch" (vụ đã đóng) bắt buộc lý do.
func harvestEditReasonRequired(status string) bool { return status != port.SeasonCompleted }

func cleanReason(raw string) string { return strings.Join(strings.Fields(raw), " ") }

func checkReason(field, reason string, required bool) error {
	switch {
	case reason == "" && !required:
		return nil
	case !domain.ValidReason(reason) || len([]rune(reason)) > 500:
		return problem.Validation.WithField(field, "Nhập lý do (10–500 ký tự)")
	case domain.ContainsPII(reason):
		return problem.Validation.WithField(field, "Lý do không được chứa SĐT hoặc CCCD")
	}
	return nil
}

// HouseholdHarvestEditInput là một lần sửa khai báo sản lượng.
type HouseholdHarvestEditInput struct {
	CooperativeRef, HouseholdRef, SeasonRef, ExpectedHarvestID uuid.UUID
	MassT, Reason                                              string
	HarvestDate                                                time.Time
	ZeroConfirmed                                              bool
	Channel, IdemKey                                           string
}

// CorrectHouseholdHarvest sửa khai báo sản lượng (KHMV-FN-06 U). Phiên bản (expected_harvest_id) phải là khai báo hiện
// hành; phát lại cùng Idempotency-Key trả kết quả đã ghi.
func (s *Service) CorrectHouseholdHarvest(ctx context.Context, in HouseholdHarvestEditInput) (HouseholdHarvestResult, error) {
	if _, err := s.matrixAccess(ctx, fnHarvest, htxroles.Update); err != nil {
		return HouseholdHarvestResult{}, err
	}
	a, err := s.householdAccess(ctx, in.CooperativeRef, true)
	if err != nil {
		return HouseholdHarvestResult{}, err
	}
	if in.IdemKey == "" {
		return HouseholdHarvestResult{}, problem.IdempotencyRequired
	}
	tonnes, err := domain.ParseTonnes(in.MassT)
	if err != nil {
		return HouseholdHarvestResult{}, problem.Validation.WithField("mass_t", err.Error())
	}
	kg, err := mass.ParseKg(tonnes.Mul(thousand).String())
	if err != nil {
		return HouseholdHarvestResult{}, problem.Validation.WithField("mass_t", "sản lượng không hợp lệ")
	}
	switch {
	case in.ExpectedHarvestID == uuid.Nil:
		return HouseholdHarvestResult{}, problem.Validation.WithField("expected_harvest_id", "Phiên bản khai báo không hợp lệ")
	case in.HarvestDate.IsZero():
		return HouseholdHarvestResult{}, problem.Validation.WithField("harvest_date", "Nhập ngày thu hoạch cuối (YYYY-MM-DD)")
	}
	in.Reason = cleanReason(in.Reason)
	if err := checkReason("reason", in.Reason, false); err != nil {
		return HouseholdHarvestResult{}, err
	}
	if in.Channel == "" {
		in.Channel = ChannelWeb
	}
	if in.Channel != ChannelApp && in.Channel != ChannelWeb {
		return HouseholdHarvestResult{}, problem.Validation.WithField("channel", "app hoặc web")
	}
	key := "household-harvest-edit:" + a.p.ActorRef.String() + ":" + in.IdemKey
	var out HouseholdHarvestResult
	err = s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		// Thứ tự khóa hộ → vụ như mở, sửa, hủy khai báo (trigger diện tích chạm dữ liệu của hộ).
		if err := memberstore.LockHousehold(ctx, tx, in.HouseholdRef); err != nil {
			return err
		}
		cur, err := householdTaskSeason(ctx, tx, a.p.TenantID, in.CooperativeRef, in.HouseholdRef, in.SeasonRef, true)
		if err != nil {
			return err
		}
		if old, err := store.HarvestByIdem(ctx, tx, key); err != nil {
			return err
		} else if old != nil {
			if old.SeasonRef != in.SeasonRef || old.ActorRef != a.p.ActorRef || old.Voided || old.SupersedesID == nil ||
				*old.SupersedesID != in.ExpectedHarvestID || !old.MassKg.Decimal().Equal(kg.Decimal()) ||
				domain.FormatDay(old.HarvestDate) != domain.FormatDay(in.HarvestDate) {
				return problem.IdempotencyConflict
			}
			ev, err := store.SeasonEventByID(ctx, tx, cur.CurrentEventID)
			if err != nil {
				return err
			}
			out = HouseholdHarvestResult{Season: SeasonResult{Season: *cur, Event: *ev}, Harvest: *old, Replayed: true}
			return nil
		}
		if !harvestEditable[cur.Status] {
			return ErrSeasonState.WithDetail("Chỉ sửa sản lượng của vụ đã khai báo; vụ đang canh tác thì dùng Khai báo sản lượng")
		}
		reason := in.Reason
		if reason == "" {
			if harvestEditReasonRequired(cur.Status) {
				return problem.Validation.WithField("reason", "Vụ đã hoàn thành: nhập lý do sửa sản lượng (10–500 ký tự)")
			}
			reason = defaultHarvestEditReason
		}
		if cur.HarvestID != in.ExpectedHarvestID {
			return ErrConcurrentEdit.With("current_harvest_id", cur.HarvestID.String())
		}
		old, err := store.HarvestByID(ctx, tx, cur.HarvestID)
		if err != nil {
			return err
		}
		if old == nil || old.SeasonRef != cur.SeasonRef {
			return ErrHarvestNotFound
		}
		if in.HarvestDate.Before(cur.SowingDate) || in.HarvestDate.After(s.today()) {
			return problem.Validation.WithField("harvest_date", "Ngày thu hoạch từ mốc gốc ("+domain.FormatDay(cur.SowingDate)+") đến hôm nay")
		}
		if tonnes.IsZero() && !in.ZeroConfirmed {
			return ErrHarvestZeroConfirm
		}
		if old.MassKg.Decimal().Equal(kg.Decimal()) && domain.FormatDay(old.HarvestDate) == domain.FormatDay(in.HarvestDate) {
			return problem.Validation.WithField("body", "Không có nội dung thay đổi")
		}
		yield := domain.YieldTPerHaDec(kg.Decimal(), cur.AreaHa)
		if !domain.ValidYield(yield) {
			return problem.Validation.WithField("mass_t", "Sản lượng vượt giới hạn lưu trữ theo diện tích vụ")
		}
		now := s.now()
		sup := old.ID
		h := *old
		h.ID, h.Seq, h.HarvestDate, h.MassKg, h.YieldTPerHa = ids.New(), 0, in.HarvestDate, kg, yield
		h.AnomalyKind, h.AnomalyConfirmed, h.AnomalyThreshold = "", false, nil
		if tonnes.IsZero() {
			h.AnomalyKind, h.AnomalyConfirmed = domain.AnomalyZero, true
		}
		h.Source, h.OnBehalf, h.EventTime, h.ActorRef, h.DeviceID, h.IdemKey = in.Channel, a.onBehalf(), now, a.p.ActorRef, nil, key
		h.SupersedesID, h.CorrectionReason, h.Voided = &sup, &reason, false
		if err := store.InsertHarvest(ctx, tx, &h); err != nil {
			return err
		}
		if err := store.SetSeasonHarvest(ctx, tx, cur.SeasonRef, h.ID); err != nil {
			return err
		}
		if domain.FormatDay(old.HarvestDate) != domain.FormatDay(in.HarvestDate) {
			if err := moveAutoHarvestTasks(ctx, tx, a, *cur, domain.FormatDay(in.HarvestDate), in.Channel, key, now); err != nil {
				return err
			}
		}
		if err := store.RefreshYield(ctx, tx, cur.SeasonRef); err != nil {
			return err
		}
		if err := enqueueHarvest(ctx, tx, h); err != nil {
			return err
		}
		fresh, err := store.SeasonByRef(ctx, tx, cur.SeasonRef, false)
		if err != nil {
			return err
		}
		ev, err := store.SeasonEventByID(ctx, tx, fresh.CurrentEventID)
		if err != nil {
			return err
		}
		out = HouseholdHarvestResult{Season: SeasonResult{Season: *fresh, Event: *ev}, Harvest: h}
		return nil
	})
	if err != nil {
		return HouseholdHarvestResult{}, err
	}
	items := []SeasonResult{out.Season}
	if err := s.attachEstimates(ctx, a.p.TenantID, items); err != nil {
		return HouseholdHarvestResult{}, err
	}
	out.Season = items[0]
	return out, nil
}

// HouseholdHarvestVoidInput là một lần hủy khai báo sản lượng.
type HouseholdHarvestVoidInput struct {
	CooperativeRef, HouseholdRef, SeasonRef, ExpectedHarvestID uuid.UUID
	Reason, Channel, IdemKey                                   string
}

// VoidHouseholdHarvest hủy khai báo sản lượng của vụ "Đã thu hoạch" (KHMV-FN-06 D): vụ về "Đang canh tác".
func (s *Service) VoidHouseholdHarvest(ctx context.Context, in HouseholdHarvestVoidInput) (SeasonResult, error) {
	if _, err := s.matrixAccess(ctx, fnHarvest, htxroles.Delete); err != nil {
		return SeasonResult{}, err
	}
	a, err := s.householdAccess(ctx, in.CooperativeRef, true)
	if err != nil {
		return SeasonResult{}, err
	}
	if in.IdemKey == "" {
		return SeasonResult{}, problem.IdempotencyRequired
	}
	in.Reason = cleanReason(in.Reason)
	if err := checkReason("reason", in.Reason, true); err != nil {
		return SeasonResult{}, err
	}
	if in.ExpectedHarvestID == uuid.Nil {
		return SeasonResult{}, problem.Validation.WithField("expected_harvest_id", "Phiên bản khai báo không hợp lệ")
	}
	if in.Channel == "" {
		in.Channel = ChannelWeb
	}
	if in.Channel != ChannelApp && in.Channel != ChannelWeb {
		return SeasonResult{}, problem.Validation.WithField("channel", "app hoặc web")
	}
	key := "household-harvest-void:" + a.p.ActorRef.String() + ":" + in.IdemKey
	var out SeasonResult
	err = s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		if err := memberstore.LockHousehold(ctx, tx, in.HouseholdRef); err != nil {
			return err
		}
		cur, err := householdTaskSeason(ctx, tx, a.p.TenantID, in.CooperativeRef, in.HouseholdRef, in.SeasonRef, true)
		if err != nil {
			return err
		}
		if old, err := store.HarvestByIdem(ctx, tx, key); err != nil {
			return err
		} else if old != nil {
			if old.SeasonRef != in.SeasonRef || old.ActorRef != a.p.ActorRef || !old.Voided || old.SupersedesID == nil ||
				*old.SupersedesID != in.ExpectedHarvestID || old.CorrectionReason == nil || *old.CorrectionReason != in.Reason {
				return problem.IdempotencyConflict
			}
			ev, err := store.SeasonEventByID(ctx, tx, cur.CurrentEventID)
			if err != nil {
				return err
			}
			out = SeasonResult{Season: *cur, Event: *ev, Replayed: true}
			return nil
		}
		if cur.Status != port.SeasonCompleted {
			return ErrSeasonState.WithDetail("Chỉ hủy khai báo của vụ Đã thu hoạch; vụ đang canh tác hoặc đã hoàn thành không hủy được")
		}
		if cur.HarvestID != in.ExpectedHarvestID {
			return ErrConcurrentEdit.With("current_harvest_id", cur.HarvestID.String())
		}
		// AC3: vụ quay lại "Đang canh tác" không được làm tổng diện tích đang canh tác vượt diện tích đất.
		hh, err := memberstore.HouseholdByRef(ctx, tx, in.HouseholdRef)
		if err != nil {
			return err
		}
		if hh == nil || hh.TenantID != a.p.TenantID || hh.CooperativeRef != in.CooperativeRef {
			return ErrSeasonHousehold
		}
		if hh.CultivatedAreaHa == nil {
			return ErrHouseholdLand
		}
		used, err := memberstore.ActiveHouseholdArea(ctx, tx, hh.Ref)
		if err != nil {
			return err
		}
		if used.Add(cur.AreaHa).GreaterThan(*hh.CultivatedAreaHa) {
			remaining := decimal.Max(decimal.Zero, hh.CultivatedAreaHa.Sub(used))
			return ErrHouseholdArea.WithDetail("Hủy khai báo làm diện tích đang canh tác vượt diện tích đất của hộ").
				With("remaining_area_ha", remaining.String()).With("active_area_ha", used.String()).
				With("land_area_ha", hh.CultivatedAreaHa.String()).With("season_area_ha", cur.AreaHa.String())
		}
		old, err := store.HarvestByID(ctx, tx, cur.HarvestID)
		if err != nil {
			return err
		}
		if old == nil || old.SeasonRef != cur.SeasonRef {
			return ErrHarvestNotFound
		}
		now := s.now()
		reason, sup := in.Reason, old.ID
		v := *old
		v.ID, v.Seq, v.Voided = ids.New(), 0, true
		v.Source, v.OnBehalf, v.EventTime, v.ActorRef, v.DeviceID, v.IdemKey = in.Channel, a.onBehalf(), now, a.p.ActorRef, nil, key
		v.SupersedesID, v.CorrectionReason = &sup, &reason
		if err := store.InsertHarvest(ctx, tx, &v); err != nil {
			return err
		}
		prev, err := store.SeasonEventByID(ctx, tx, cur.CurrentEventID)
		if err != nil {
			return err
		}
		ev := *prev
		supEv := cur.CurrentEventID
		ev.ID, ev.Seq, ev.Action, ev.HarvestID, ev.Reason = ids.New(), 0, "reopened", uuid.Nil, ""
		ev.SupersedesID, ev.CorrectionReason = &supEv, &reason
		ev.OnBehalf, ev.Channel, ev.EventTime, ev.ActorRef, ev.DeviceID, ev.IdemKey = a.onBehalf(), in.Channel, now, a.p.ActorRef, nil, key
		if err := store.InsertSeasonEvent(ctx, tx, &ev); err != nil {
			return err
		}
		reopened := store.ApplySeasonEvent(cur, ev)
		if err := store.SaveSeason(ctx, tx, a.p.TenantID, reopened); err != nil {
			return err
		}
		if err := revertAutoHarvestTasks(ctx, tx, a, reopened, reason, in.Channel, key, now); err != nil {
			return err
		}
		if err := afterSeasonChange(ctx, tx, a.p.TenantID, reopened, now); err != nil {
			return err
		}
		if err := enqueue(ctx, tx, a.p.TenantID, port.EventSeasonCorrected, now, port.SeasonCorrectedV1{
			SeasonRef: ev.SeasonRef, EventID: ev.ID, Action: ev.Action, Status: reopened.Status, PlotRef: ev.PlotRef,
			HouseholdRef: uuidPtr(ev.HouseholdRef), CooperativeRef: ev.CooperativeRef, CommuneCode: ev.CommuneCode,
			SeasonCode: ev.SeasonCode, SeasonYear: ev.SeasonYear, VarietyCode: ev.VarietyCode, SOPVersionID: ev.SOPVersionID,
			SowingDate: domain.FormatDay(ev.SowingDate), ExpectedHarvestDate: domain.FormatDay(ev.ExpectedHarvestDate),
			SupersedesID: supEv, CorrectionReason: reason, ActorRef: ev.ActorRef, EventTime: now.Format(time.RFC3339),
		}); err != nil {
			return err
		}
		fresh, err := store.SeasonByRef(ctx, tx, cur.SeasonRef, false)
		if err != nil {
			return err
		}
		out = SeasonResult{Season: *fresh, Event: ev}
		return nil
	})
	if err != nil {
		return SeasonResult{}, err
	}
	items := []SeasonResult{out}
	if err := s.attachEstimates(ctx, a.p.TenantID, items); err != nil {
		return SeasonResult{}, err
	}
	return items[0], nil
}

// HouseholdHarvestHistory trả mọi phiên bản khai báo sản lượng của vụ theo thứ tự ghi (người xem được vụ đều xem được).
func (s *Service) HouseholdHarvestHistory(ctx context.Context, coop, hh, season uuid.UUID) ([]store.Harvest, error) {
	if _, err := s.matrixAccess(ctx, fnHarvest, htxroles.Read); err != nil {
		return nil, err
	}
	a, err := s.householdAccess(ctx, coop, false)
	if err != nil {
		return nil, err
	}
	var out []store.Harvest
	err = s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		if _, err := householdTaskSeason(ctx, tx, a.p.TenantID, coop, hh, season, false); err != nil {
			return err
		}
		var err error
		out, err = store.HarvestHistory(ctx, tx, season)
		return err
	})
	return out, err
}

// writeTaskCorrection ghi một bản bù của nhật ký việc theo hộ do hệ thống tạo (GPS giữ như bản trước; event_time là lúc
// ghi để ngày làm không vượt ngày ghi).
func writeTaskCorrection(ctx context.Context, tx pgx.Tx, a access, last store.FarmLog, meta domain.TaskRecord, inputs []domain.Input,
	reason, channel, key string, now time.Time) error {
	next := last
	sup := last.ID
	next.ID, next.Version, next.TaskCompletion, next.Inputs = ids.New(), 0, &meta, inputs
	next.SupersedesID, next.CorrectionReason = &sup, &reason
	next.ActorRef, next.OnBehalf, next.Channel, next.DeviceID, next.IdemKey = a.p.ActorRef, a.onBehalf(), channel, nil, key
	next.EventTime, next.BecameCurrent, next.RecordedAt = now, true, time.Time{}
	if err := store.InsertFarmLog(ctx, tx, &next); err != nil {
		return err
	}
	return store.SaveHouseholdTask(ctx, tx, next)
}

// moveAutoHarvestTasks đổi ngày làm của việc Thu hoạch tự Xong theo ngày thu hoạch mới (sửa sản lượng).
func moveAutoHarvestTasks(ctx context.Context, tx pgx.Tx, a access, cur store.Season, doneOn, channel, key string, now time.Time) error {
	logs, err := store.HouseholdTasks(ctx, tx, cur.SeasonRef)
	if err != nil {
		return err
	}
	for _, l := range logs {
		m := l.TaskCompletion
		if m == nil || !m.AutoHarvest || m.State != "done" || m.DoneOn == doneOn {
			continue
		}
		meta := *m
		meta.DoneOn, meta.SeasonEventID = doneOn, cur.CurrentEventID
		if err := writeTaskCorrection(ctx, tx, a, l, meta, l.Inputs, moveAutoHarvestReason, channel, key+":move:"+l.LogRef.String(), now); err != nil {
			return err
		}
	}
	return nil
}

// revertAutoHarvestTasks đưa việc Thu hoạch tự Xong về trạng thái trước khai báo (hủy khai báo, AH-189 AC2): bản ghi
// do khai báo tạo ra thì xóa mềm; bản ghi từng có phiên bản người dùng (vd Bỏ qua) thì khôi phục phiên bản đó.
func revertAutoHarvestTasks(ctx context.Context, tx pgx.Tx, a access, cur store.Season, reason, channel, key string, now time.Time) error {
	logs, err := store.HouseholdTasks(ctx, tx, cur.SeasonRef)
	if err != nil {
		return err
	}
	for _, l := range logs {
		m := l.TaskCompletion
		if m == nil || !m.AutoHarvest || m.State != "done" {
			continue
		}
		versions, err := store.FarmLogVersions(ctx, tx, l.LogRef)
		if err != nil {
			return err
		}
		var before *store.FarmLog
		for i := range versions {
			if v := versions[i].TaskCompletion; v != nil && !v.AutoHarvest && v.State != "voided" {
				before = &versions[i]
			}
		}
		meta, inputs := *m, l.Inputs
		if before != nil {
			meta, inputs = *before.TaskCompletion, before.Inputs
		} else {
			meta.State = "voided"
		}
		meta.SeasonEventID = cur.CurrentEventID
		if err := writeTaskCorrection(ctx, tx, a, l, meta, inputs, reason, channel, key+":revert:"+l.LogRef.String(), now); err != nil {
			return err
		}
	}
	return nil
}
