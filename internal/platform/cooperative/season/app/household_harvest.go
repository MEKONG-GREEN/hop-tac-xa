package app

// AH-100 (FN-06.1, FN-06-B3, EX-01/02/06, BR-01/02/06, FN-05-BR-06): Thành viên HTX khai báo tổng sản lượng (tấn) và
// ngày thu hoạch cuối của một vụ theo hộ đang canh tác.
//
//   - Vụ chuyển "Đã thu hoạch" (sự kiện completed trên sổ vụ, harvest_id trỏ khai báo); diện tích được giải phóng qua
//     bản chiếu household_season_capacity; ngày thu hoạch thành mốc NTT của lịch việc.
//   - Mọi việc Thu hoạch (loại việc thu_hoach, Q-05) chưa Xong — kể cả đã Bỏ qua — tự Xong theo ngày thu hoạch, gắn
//     cờ auto_harvest (bản ghi mới, hoặc bản bù thay bản Bỏ qua).
//   - Sản lượng ≥ 0, 2 chữ số lẻ (KH-26); bằng 0 phải xác nhận (KH-19); không còn ngưỡng bất thường (KH-09, Q-08).
//   - Mốc gốc ≤ ngày thu hoạch ≤ hôm nay (GMT+7). Vụ không "Đang canh tác" bị từ chối, hướng dẫn dùng Sửa sản lượng.
//
// Không gọi port module khác trong giao dịch: mọi kiểm tra và ghi nằm trong module, dưới khóa của vụ.

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
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

// ErrHarvestZeroConfirm: sản lượng bằng 0 cần người dùng xác nhận lại (KH-19).
var ErrHarvestZeroConfirm = problem.New(http.StatusConflict, "cropcycle.harvest_zero_confirmation_required", "Sản lượng bằng 0: xác nhận lại trước khi lưu")

// autoHarvestReason là lý do của bản bù khi việc Thu hoạch đã Bỏ qua được tự Xong.
const autoHarvestReason = "Tự Xong khi khai báo sản lượng của vụ"

var thousand = decimal.NewFromInt(1000)

// HouseholdHarvestInput là một lần khai báo sản lượng của vụ theo hộ.
type HouseholdHarvestInput struct {
	CooperativeRef, HouseholdRef, SeasonRef, ExpectedEventID uuid.UUID
	MassT                                                    string
	HarvestDate                                              time.Time
	ZeroConfirmed                                            bool
	GPS                                                      domain.GPS
	Channel, IdemKey                                         string
}

// HouseholdHarvestResult là kết quả khai báo; AutoCompleted là số việc Thu hoạch vừa được tự Xong.
type HouseholdHarvestResult struct {
	Season        SeasonResult
	Harvest       store.Harvest
	AutoCompleted []int
	Replayed      bool
}

// DeclareHouseholdHarvest khai báo sản lượng của vụ theo hộ (KHMV-FN-06 C). Thành viên HTX ghi cho hộ cùng HTX;
// Đại diện HTX chỉ xem (403). Phát lại cùng Idempotency-Key trả kết quả đã ghi.
func (s *Service) DeclareHouseholdHarvest(ctx context.Context, in HouseholdHarvestInput) (HouseholdHarvestResult, error) {
	if _, err := s.matrixAccess(ctx, fnHarvest, htxroles.Create); err != nil {
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
	case in.ExpectedEventID == uuid.Nil:
		return HouseholdHarvestResult{}, problem.Validation.WithField("expected_season_event_id", "Phiên bản vụ không hợp lệ")
	case in.HarvestDate.IsZero():
		return HouseholdHarvestResult{}, problem.Validation.WithField("harvest_date", "Nhập ngày thu hoạch cuối (YYYY-MM-DD)")
	}
	if in.Channel == "" {
		in.Channel = ChannelApp
	}
	if in.Channel != ChannelApp && in.Channel != ChannelWeb {
		return HouseholdHarvestResult{}, problem.Validation.WithField("channel", "app hoặc web")
	}
	if err := in.GPS.Normalize(); err != nil {
		return HouseholdHarvestResult{}, fieldProblem(err)
	}
	if in.GPS.Source != "device" && in.GPS.Source != "denied" && in.GPS.Source != "no_device" {
		return HouseholdHarvestResult{}, problem.Validation.WithField("gps.source", "chỉ nhận GPS thiết bị hoặc nhãn thiếu GPS")
	}
	if in.GPS.HasPoint() {
		lon, lat := in.GPS.Lon.Round(7), in.GPS.Lat.Round(7)
		in.GPS.Lon, in.GPS.Lat = &lon, &lat
	}
	if in.GPS.AccuracyM != nil {
		if in.GPS.AccuracyM.GreaterThan(decimal.RequireFromString("999999.99")) {
			return HouseholdHarvestResult{}, problem.Validation.WithField("gps.accuracy_m", "tối đa 999999.99")
		}
		n := in.GPS.AccuracyM.Round(2)
		in.GPS.AccuracyM = &n
	}
	key := "household-harvest:" + a.p.ActorRef.String() + ":" + in.IdemKey
	var out HouseholdHarvestResult
	err = s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		// Thứ tự khóa hộ → vụ như mở, sửa vụ (AH-175) và hủy khai báo (AH-189): tránh deadlock với trigger diện tích.
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
			if old.SeasonRef != in.SeasonRef || old.ActorRef != a.p.ActorRef || !old.MassKg.Decimal().Equal(kg.Decimal()) ||
				domain.FormatDay(old.HarvestDate) != domain.FormatDay(in.HarvestDate) || old.Source != in.Channel {
				return problem.IdempotencyConflict
			}
			ev, err := store.SeasonEventByID(ctx, tx, cur.CurrentEventID)
			if err != nil {
				return err
			}
			out = HouseholdHarvestResult{Season: SeasonResult{Season: *cur, Event: *ev}, Harvest: *old, Replayed: true}
			return nil
		}
		if cur.Status != port.SeasonOpen {
			return ErrSeasonState.WithDetail("Chỉ khai báo sản lượng cho vụ đang canh tác; vụ đã khai báo thì dùng Sửa sản lượng")
		}
		if cur.CurrentEventID != in.ExpectedEventID {
			return ErrTaskChanged.With("current_event_id", cur.CurrentEventID)
		}
		today := s.today()
		if in.HarvestDate.Before(cur.SowingDate) || in.HarvestDate.After(today) {
			return problem.Validation.WithField("harvest_date", "Ngày thu hoạch từ mốc gốc ("+domain.FormatDay(cur.SowingDate)+") đến hôm nay")
		}
		if tonnes.IsZero() && !in.ZeroConfirmed {
			return ErrHarvestZeroConfirm
		}
		yield := domain.YieldTPerHaDec(kg.Decimal(), cur.AreaHa)
		if !domain.ValidYield(yield) {
			return problem.Validation.WithField("mass_t", "Sản lượng vượt giới hạn lưu trữ theo diện tích vụ")
		}
		loc := domain.LocMissing
		if in.GPS.HasPoint() {
			loc = "unverified"
		}
		now := s.now()
		h := store.Harvest{
			ID: ids.New(), TenantID: a.p.TenantID, SeasonRef: cur.SeasonRef, HouseholdRef: cur.HouseholdRef,
			CooperativeRef: cur.CooperativeRef, CommuneCode: cur.CommuneCode, SeasonCode: cur.SeasonCode, SeasonYear: cur.SeasonYear,
			VarietyCode: cur.VarietyCode, HarvestDate: in.HarvestDate, MassKg: kg, AreaHa: cur.AreaHa, YieldTPerHa: yield,
			Source: in.Channel, GPS: in.GPS, LocationStatus: loc, OnBehalf: a.onBehalf(), EventTime: now, ActorRef: a.p.ActorRef, IdemKey: key,
		}
		if tonnes.IsZero() {
			h.AnomalyKind, h.AnomalyConfirmed = domain.AnomalyZero, true
		}
		if err := store.InsertHarvest(ctx, tx, &h); err != nil {
			return err
		}
		prev, err := store.SeasonEventByID(ctx, tx, cur.CurrentEventID)
		if err != nil {
			return err
		}
		ev := *prev
		ev.ID, ev.Seq, ev.Action, ev.HarvestID, ev.Reason = ids.New(), 0, "completed", h.ID, ""
		ev.SupersedesID, ev.CorrectionReason = nil, nil
		ev.GPS, ev.LocationStatus, ev.DistanceM = in.GPS, loc, nil
		ev.OnBehalf, ev.Channel, ev.EventTime, ev.ActorRef, ev.DeviceID, ev.IdemKey = a.onBehalf(), in.Channel, now, a.p.ActorRef, nil, key
		if err := store.InsertSeasonEvent(ctx, tx, &ev); err != nil {
			return err
		}
		completed := store.ApplySeasonEvent(cur, ev)
		if err := store.SaveSeason(ctx, tx, a.p.TenantID, completed); err != nil {
			return err
		}
		auto, err := s.autoCompleteHarvestTasks(ctx, tx, a, completed, in, key, now)
		if err != nil {
			return err
		}
		if err := afterSeasonChange(ctx, tx, a.p.TenantID, completed, now); err != nil {
			return err
		}
		if err := enqueueHarvest(ctx, tx, h); err != nil {
			return err
		}
		if err := enqueue(ctx, tx, a.p.TenantID, port.EventSeasonCompleted, now, port.SeasonCompletedV1{
			SeasonRef: ev.SeasonRef, EventID: ev.ID, PlotRef: ev.PlotRef, HouseholdRef: uuidPtr(ev.HouseholdRef), CooperativeRef: ev.CooperativeRef,
			CommuneCode: ev.CommuneCode, SeasonCode: ev.SeasonCode, SeasonYear: ev.SeasonYear, VarietyCode: ev.VarietyCode, HarvestID: h.ID,
			HarvestDate: domain.FormatDay(h.HarvestDate), AreaHa: h.AreaHa.StringFixed(4), ActorRef: ev.ActorRef, EventTime: now.Format(time.RFC3339),
		}); err != nil {
			return err
		}
		fresh, err := store.SeasonByRef(ctx, tx, cur.SeasonRef, false)
		if err != nil {
			return err
		}
		out = HouseholdHarvestResult{Season: SeasonResult{Season: *fresh, Event: ev}, Harvest: h, AutoCompleted: auto}
		return nil
	})
	if err != nil {
		return HouseholdHarvestResult{}, err
	}
	// AC2: trả kèm sản lượng dự kiến (AH-87) và khai báo để so thực tế với dự kiến.
	items := []SeasonResult{out.Season}
	if err := s.attachEstimates(ctx, a.p.TenantID, items); err != nil {
		return HouseholdHarvestResult{}, err
	}
	out.Season = items[0]
	return out, nil
}

// autoCompleteHarvestTasks tự Xong mọi việc Thu hoạch chưa Xong của vụ theo ngày thu hoạch (AC1). Việc đã Bỏ qua bằng
// bản ghi được thay bằng bản bù "done"; việc chưa có bản ghi (hoặc bị bỏ qua trong kế hoạch) được ghi bản mới. Bản ghi
// dùng GPS của lần khai báo và mang cờ auto_harvest.
func (s *Service) autoCompleteHarvestTasks(ctx context.Context, tx pgx.Tx, a access, cur store.Season, in HouseholdHarvestInput, key string, now time.Time) ([]int, error) {
	check, err := s.householdChecklistTx(ctx, tx, cur, true)
	if err != nil {
		return nil, err
	}
	var snapshot domain.SeasonSnapshot
	if err := json.Unmarshal(cur.SOPSnapshot, &snapshot); err != nil {
		return nil, err
	}
	doneOn := domain.FormatDay(in.HarvestDate)
	loc := domain.LocMissing
	if in.GPS.HasPoint() {
		loc = "unverified"
	}
	auto := []int{}
	for _, stage := range check.Progress.Stages {
		for _, t := range stage.Tasks {
			if t.ActivityCode != domain.ActThuHoach || t.DoneOn != nil {
				continue
			}
			step, ok := snapshot.Step(t.Seq)
			if !ok {
				return nil, ErrDependency
			}
			var skippedRecord *store.FarmLog
			for i := range check.Records {
				r := check.Records[i]
				if r.TaskCompletion != nil && r.TaskCompletion.StepSeq == t.Seq && r.TaskCompletion.State == "skipped" {
					skippedRecord = &check.Records[i]
				}
			}
			var l store.FarmLog
			if skippedRecord != nil {
				meta := *skippedRecord.TaskCompletion
				meta.State, meta.DoneOn, meta.AutoHarvest, meta.SeasonEventID = "done", doneOn, true, cur.CurrentEventID
				l = *skippedRecord
				sup, reason := skippedRecord.ID, autoHarvestReason
				l.ID, l.Version, l.TaskCompletion = ids.New(), 0, &meta
				l.SupersedesID, l.CorrectionReason = &sup, &reason
			} else {
				inputs := []domain.Input{}
				for _, m := range step.Materials {
					name := m.Name
					if name == "" {
						name = m.MaterialCode
					}
					inputs = append(inputs, domain.Input{Name: name, Qty: m.DosePerHa.Mul(cur.AreaHa), Unit: m.Unit})
				}
				id, seq := ids.New(), t.Seq
				meta := domain.TaskRecord{StepSeq: t.Seq, StageSeq: step.StageSeq, State: "done", DoneOn: doneOn, SOPVersionID: cur.SOPVersionID,
					SeasonEventID: cur.CurrentEventID, Step: step, AutoHarvest: true}
				l = store.FarmLog{ID: id, TenantID: a.p.TenantID, LogRef: id, SeasonRef: cur.SeasonRef, HouseholdRef: cur.HouseholdRef,
					CooperativeRef: cur.CooperativeRef, CommuneCode: cur.CommuneCode, ActivityCode: step.ActivityCode, SOPStepSeq: &seq,
					Inputs: inputs, EntryKind: "household", TaskCompletion: &meta}
			}
			l.GPS, l.LocationStatus, l.Channel, l.EventTime = in.GPS, loc, in.Channel, now
			l.ActorRef, l.OnBehalf, l.DeviceID, l.IdemKey = a.p.ActorRef, a.onBehalf(), nil, key+":auto:"+strconv.Itoa(t.Seq)
			l.BecameCurrent, l.RecordedAt = true, time.Time{}
			if err := store.InsertFarmLog(ctx, tx, &l); err != nil {
				return nil, err
			}
			if err := store.SaveHouseholdTask(ctx, tx, l); err != nil {
				return nil, err
			}
			meta := l.TaskCompletion
			if err := enqueue(ctx, tx, a.p.TenantID, port.EventHouseholdTaskCompleted, now, port.HouseholdTaskCompletedV1{LogID: l.ID,
				SeasonRef: l.SeasonRef, HouseholdRef: l.HouseholdRef, CooperativeRef: l.CooperativeRef, SOPVersionID: meta.SOPVersionID,
				StepSeq: meta.StepSeq, DoneOn: meta.DoneOn, GPSMissing: !l.GPS.HasPoint(), ActorRef: l.ActorRef, EventTime: now.Format(time.RFC3339)}); err != nil {
				return nil, err
			}
			auto = append(auto, t.Seq)
		}
	}
	return auto, nil
}
