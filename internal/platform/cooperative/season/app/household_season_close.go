package app

// AH-152 (FN-06.5, FN-06-B4, EX-05, BR-05, KH-06, KH-27): tổng kết "Ghi lại vụ" và hoàn thành vụ theo hộ.
//
//   - Tổng kết tự tính: năng suất (sản lượng ÷ diện tích vụ), tổng vật tư theo liều thực tế (cộng các bản ghi việc
//     đang hiệu lực), số việc sớm / đúng hạn / trễ / bỏ qua / chưa làm. Thành viên HTX nhập chi phí (VND, tùy chọn) và
//     ghi chú (tùy chọn).
//   - Hoàn thành vụ: chỉ vụ "Đã thu hoạch" (đã có sản lượng — AC3, AC4) và mọi việc giai đoạn cuối (trừ việc tổng kết)
//     đã Xong hoặc Bỏ qua (AC2). Ghi bản chụp tổng kết cố định (KH-27), việc tổng kết thành Xong (nếu quy trình có), sự
//     kiện closed. Vụ "Đã hoàn thành vụ" là đóng, không quay lại; các luồng ghi việc chặn theo allowlist trạng thái.
//   - Đại diện HTX chỉ xem tổng kết (AC5).
//
// Không gọi port module khác trong giao dịch.

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
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
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
)

var (
	// ErrHarvestRequired: vụ đang canh tác chưa khai báo sản lượng không chuyển thẳng "Đã hoàn thành vụ" (AC3, AC4).
	ErrHarvestRequired = problem.New(http.StatusConflict, "cropcycle.season_harvest_required", "Chưa khai báo sản lượng: khai báo sản lượng trước khi hoàn thành vụ")
	// ErrLastStageUnresolved: còn việc giai đoạn cuối chưa Xong hoặc Bỏ qua (AC2).
	ErrLastStageUnresolved = problem.New(http.StatusConflict, "cropcycle.season_last_stage_unresolved", "Còn việc giai đoạn cuối chưa Xong hoặc Bỏ qua")

	costPattern = regexp.MustCompile(`^[0-9]{1,12}$`)
)

// SeasonSummaryData là nội dung tổng kết (lưu cố định trong sổ khi hoàn thành vụ).
type SeasonSummaryData struct {
	AreaHa         string                   `json:"area_ha"`
	MassKg         *string                  `json:"mass_kg"`
	MassT          *string                  `json:"mass_t"`
	YieldTPerHa    *string                  `json:"yield_t_per_ha"`
	HarvestDate    *string                  `json:"harvest_date"`
	Materials      []domain.SummaryMaterial `json:"materials"`
	Tasks          domain.SummaryTasks      `json:"tasks"`
	SummaryStepSeq *int                     `json:"summary_step_seq"`
}

// SeasonSummaryView là tổng kết trả cho client: tính tại chỗ khi vụ chưa đóng, bản chụp khi đã đóng.
type SeasonSummaryView struct {
	Data            SeasonSummaryData
	Status          string
	CanClose        bool
	Blockers        []string
	UnresolvedSteps []int
	HarvestReminder bool
	CostVND         *decimal.Decimal
	Note            *string
	ClosedAt        *time.Time
	ClosedBy        *uuid.UUID
}

// computeSeasonSummaryTx tính tổng kết của vụ từ khai báo hiện hành, checklist và bản ghi việc đang hiệu lực.
func (s *Service) computeSeasonSummaryTx(ctx context.Context, tx pgx.Tx, cur store.Season) (SeasonSummaryView, error) {
	var snap domain.SeasonSnapshot
	if err := json.Unmarshal(cur.SOPSnapshot, &snap); err != nil {
		return SeasonSummaryView{}, err
	}
	check, err := s.householdChecklistTx(ctx, tx, cur, false)
	if err != nil {
		return SeasonSummaryView{}, err
	}
	summarySeq := domain.SummaryStepSeq(snap)
	inputs := []domain.Input{}
	for _, l := range check.Records {
		if m := l.TaskCompletion; m != nil && (m.State == "done" || m.State == "partial") {
			inputs = append(inputs, l.Inputs...)
		}
	}
	tasks, unresolved, lastResolved := domain.SummarizeTasks(check.Progress, summarySeq)
	data := SeasonSummaryData{AreaHa: cur.AreaHa.StringFixed(2), Materials: domain.AggregateMaterials(inputs), Tasks: tasks}
	if summarySeq > 0 {
		data.SummaryStepSeq = &summarySeq
	}
	if cur.HarvestID != uuid.Nil {
		h, err := store.HarvestByID(ctx, tx, cur.HarvestID)
		if err != nil {
			return SeasonSummaryView{}, err
		}
		if h != nil && h.SeasonRef == cur.SeasonRef {
			kg, t := h.MassKg.String(), h.MassKg.Decimal().Div(thousand).StringFixed(2)
			y, d := h.YieldTPerHa.StringFixed(2), domain.FormatDay(h.HarvestDate)
			data.MassKg, data.MassT, data.YieldTPerHa, data.HarvestDate = &kg, &t, &y, &d
		}
	}
	v := SeasonSummaryView{Data: data, Status: cur.Status, UnresolvedSteps: unresolved, Blockers: []string{}}
	if cur.Status != port.SeasonCompleted || data.MassKg == nil {
		v.Blockers = append(v.Blockers, "harvest_required")
	}
	if len(unresolved) > 0 {
		v.Blockers = append(v.Blockers, "last_stage_unresolved")
	}
	v.CanClose = cur.Status == port.SeasonCompleted && len(v.Blockers) == 0
	v.HarvestReminder = cur.Status == port.SeasonOpen && lastResolved
	return v, nil
}

func summaryFromEvent(e store.SeasonSummaryEvent, status string) (SeasonSummaryView, error) {
	var data SeasonSummaryData
	if err := json.Unmarshal(e.Summary, &data); err != nil {
		return SeasonSummaryView{}, err
	}
	if data.Materials == nil {
		data.Materials = []domain.SummaryMaterial{}
	}
	at, by := e.EventTime, e.ActorRef
	return SeasonSummaryView{Data: data, Status: status, Blockers: []string{}, UnresolvedSteps: []int{}, CostVND: e.CostVND,
		Note: e.Note, ClosedAt: &at, ClosedBy: &by}, nil
}

// HouseholdSeasonSummary đọc tổng kết vụ (mọi vai xem được vụ; Đại diện HTX chỉ xem — AC5).
func (s *Service) HouseholdSeasonSummary(ctx context.Context, coop, hh, season uuid.UUID) (SeasonSummaryView, error) {
	a, err := s.householdAccess(ctx, coop, false)
	if err != nil {
		return SeasonSummaryView{}, err
	}
	var out SeasonSummaryView
	err = s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		cur, err := householdTaskSeason(ctx, tx, a.p.TenantID, coop, hh, season, false)
		if err != nil {
			return err
		}
		if cur.Status == port.SeasonClosed {
			e, err := store.CurrentSeasonSummary(ctx, tx, cur.SeasonRef)
			if err != nil {
				return err
			}
			if e != nil {
				out, err = summaryFromEvent(*e, cur.Status)
				return err
			}
		}
		out, err = s.computeSeasonSummaryTx(ctx, tx, *cur)
		return err
	})
	return out, err
}

// SeasonCloseInput là một lần hoàn thành vụ.
type SeasonCloseInput struct {
	CooperativeRef, HouseholdRef, SeasonRef, ExpectedEventID uuid.UUID
	CostVND, Note                                            string
	GPS                                                      domain.GPS
	Channel, IdemKey                                         string
}

// CloseHouseholdSeason hoàn thành vụ theo hộ (KHMV-FN-06 U). Phát lại cùng Idempotency-Key trả kết quả đã ghi.
func (s *Service) CloseHouseholdSeason(ctx context.Context, in SeasonCloseInput) (SeasonResult, SeasonSummaryView, error) {
	if _, err := s.matrixAccess(ctx, fnHarvest, htxroles.Update); err != nil {
		return SeasonResult{}, SeasonSummaryView{}, err
	}
	a, err := s.householdAccess(ctx, in.CooperativeRef, true)
	if err != nil {
		return SeasonResult{}, SeasonSummaryView{}, err
	}
	if in.IdemKey == "" {
		return SeasonResult{}, SeasonSummaryView{}, problem.IdempotencyRequired
	}
	if in.ExpectedEventID == uuid.Nil {
		return SeasonResult{}, SeasonSummaryView{}, problem.Validation.WithField("expected_season_event_id", "Phiên bản vụ không hợp lệ")
	}
	var cost *decimal.Decimal
	if c := strings.TrimSpace(in.CostVND); c != "" {
		if !costPattern.MatchString(c) {
			return SeasonResult{}, SeasonSummaryView{}, problem.Validation.WithField("cost_vnd", "Chi phí là số nguyên VND không âm, tối đa 12 chữ số")
		}
		d := decimal.RequireFromString(c)
		cost = &d
	}
	var note *string
	if n := strings.TrimSpace(in.Note); n != "" {
		switch {
		case len([]rune(n)) > 1000:
			return SeasonResult{}, SeasonSummaryView{}, problem.Validation.WithField("note", "Ghi chú tối đa 1000 ký tự")
		case domain.ContainsPII(n):
			return SeasonResult{}, SeasonSummaryView{}, problem.Validation.WithField("note", "Ghi chú không được chứa SĐT hoặc CCCD")
		}
		note = &n
	}
	if in.Channel == "" {
		in.Channel = ChannelWeb
	}
	if in.Channel != ChannelApp && in.Channel != ChannelWeb {
		return SeasonResult{}, SeasonSummaryView{}, problem.Validation.WithField("channel", "app hoặc web")
	}
	if err := in.GPS.Normalize(); err != nil {
		return SeasonResult{}, SeasonSummaryView{}, fieldProblem(err)
	}
	if in.GPS.Source == "" {
		in.GPS.Source = "no_device"
	}
	if in.GPS.Source != "device" && in.GPS.Source != "denied" && in.GPS.Source != "no_device" {
		return SeasonResult{}, SeasonSummaryView{}, problem.Validation.WithField("gps.source", "chỉ nhận GPS thiết bị hoặc nhãn thiếu GPS")
	}
	if in.GPS.HasPoint() {
		lon, lat := in.GPS.Lon.Round(7), in.GPS.Lat.Round(7)
		in.GPS.Lon, in.GPS.Lat = &lon, &lat
	}
	if in.GPS.AccuracyM != nil {
		n := in.GPS.AccuracyM.Round(2)
		in.GPS.AccuracyM = &n
	}
	key := "household-season-close:" + a.p.ActorRef.String() + ":" + in.IdemKey
	var out SeasonResult
	var view SeasonSummaryView
	err = s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		if err := memberstore.LockHousehold(ctx, tx, in.HouseholdRef); err != nil {
			return err
		}
		cur, err := householdTaskSeason(ctx, tx, a.p.TenantID, in.CooperativeRef, in.HouseholdRef, in.SeasonRef, true)
		if err != nil {
			return err
		}
		if old, err := store.SeasonSummaryByIdem(ctx, tx, key); err != nil {
			return err
		} else if old != nil {
			if old.SeasonRef != in.SeasonRef || old.ActorRef != a.p.ActorRef || !sameDecimalPtr(old.CostVND, cost) ||
				(old.Note == nil) != (note == nil) || (note != nil && *old.Note != *note) {
				return problem.IdempotencyConflict
			}
			ev, err := store.SeasonEventByID(ctx, tx, cur.CurrentEventID)
			if err != nil {
				return err
			}
			out = SeasonResult{Season: *cur, Event: *ev, Replayed: true}
			view, err = summaryFromEvent(*old, cur.Status)
			return err
		}
		switch cur.Status {
		case port.SeasonOpen:
			return ErrHarvestRequired
		case port.SeasonCompleted:
		case port.SeasonClosed:
			return ErrSeasonState.WithDetail("Vụ đã hoàn thành; vụ đóng không quay lại")
		default:
			return ErrSeasonState.WithDetail("Chỉ hoàn thành vụ đã khai báo sản lượng")
		}
		if cur.CurrentEventID != in.ExpectedEventID {
			return ErrTaskChanged.With("current_event_id", cur.CurrentEventID)
		}
		summary, err := s.computeSeasonSummaryTx(ctx, tx, *cur)
		if err != nil {
			return err
		}
		if summary.Data.MassKg == nil {
			return ErrHarvestRequired
		}
		if len(summary.UnresolvedSteps) > 0 {
			return ErrLastStageUnresolved.With("unresolved_steps", summary.UnresolvedSteps)
		}
		now := s.now()
		loc := domain.LocMissing
		if in.GPS.HasPoint() {
			loc = "unverified"
		}
		// Việc tổng kết vụ (nếu quy trình có và chưa Xong) thành Xong trong cùng thao tác.
		if seq := summary.Data.SummaryStepSeq; seq != nil {
			if err := s.completeSummaryStep(ctx, tx, a, *cur, *seq, in, loc, key, now); err != nil {
				return err
			}
		}
		prev, err := store.SeasonEventByID(ctx, tx, cur.CurrentEventID)
		if err != nil {
			return err
		}
		ev := *prev
		ev.ID, ev.Seq, ev.Action, ev.HarvestID, ev.Reason = ids.New(), 0, "closed", uuid.Nil, ""
		ev.SupersedesID, ev.CorrectionReason = nil, nil
		ev.GPS, ev.LocationStatus, ev.DistanceM = in.GPS, loc, nil
		ev.OnBehalf, ev.Channel, ev.EventTime, ev.ActorRef, ev.DeviceID, ev.IdemKey = a.onBehalf(), in.Channel, now, a.p.ActorRef, nil, key
		raw, err := json.Marshal(summary.Data)
		if err != nil {
			return err
		}
		sum := store.SeasonSummaryEvent{ID: ids.New(), TenantID: a.p.TenantID, SeasonRef: cur.SeasonRef, CooperativeRef: cur.CooperativeRef,
			HouseholdRef: cur.HouseholdRef, SeasonEventID: ev.ID, ActorRef: a.p.ActorRef, Summary: raw, CostVND: cost, Note: note,
			EventTime: now, IdemKey: key}
		if err := store.InsertSeasonSummary(ctx, tx, &sum); err != nil {
			return err
		}
		if err := store.InsertSeasonEvent(ctx, tx, &ev); err != nil {
			return err
		}
		closed := store.ApplySeasonEvent(cur, ev)
		if err := store.SaveSeason(ctx, tx, a.p.TenantID, closed); err != nil {
			return err
		}
		if err := afterSeasonChange(ctx, tx, a.p.TenantID, closed, now); err != nil {
			return err
		}
		if err := enqueue(ctx, tx, a.p.TenantID, port.EventHouseholdSeasonClosed, now, port.HouseholdSeasonClosedV1{
			SeasonRef: ev.SeasonRef, EventID: ev.ID, HouseholdRef: ev.HouseholdRef, CooperativeRef: ev.CooperativeRef, HarvestID: closed.HarvestID,
			ClosedOn: domain.FormatDay(businessDay(now)), ActorRef: ev.ActorRef, EventTime: now.Format(time.RFC3339),
		}); err != nil {
			return err
		}
		fresh, err := store.SeasonByRef(ctx, tx, cur.SeasonRef, false)
		if err != nil {
			return err
		}
		out = SeasonResult{Season: *fresh, Event: ev}
		view, err = summaryFromEvent(sum, fresh.Status)
		return err
	})
	if err != nil {
		return SeasonResult{}, SeasonSummaryView{}, err
	}
	items := []SeasonResult{out}
	if err := s.attachEstimates(ctx, a.p.TenantID, items); err != nil {
		return SeasonResult{}, SeasonSummaryView{}, err
	}
	return items[0], view, nil
}

// completeSummaryStep ghi việc tổng kết vụ thành Xong hôm nay (bản ghi mới; bỏ qua nếu đã Xong).
func (s *Service) completeSummaryStep(ctx context.Context, tx pgx.Tx, a access, cur store.Season, seq int, in SeasonCloseInput, loc, key string, now time.Time) error {
	check, err := s.householdChecklistTx(ctx, tx, cur, false)
	if err != nil {
		return err
	}
	for _, st := range check.Progress.Stages {
		for _, t := range st.Tasks {
			if t.Seq == seq && t.DoneOn != nil {
				return nil
			}
		}
	}
	var snap domain.SeasonSnapshot
	if err := json.Unmarshal(cur.SOPSnapshot, &snap); err != nil {
		return err
	}
	step, ok := snap.Step(seq)
	if !ok {
		return ErrDependency
	}
	id, stepSeq := ids.New(), seq
	meta := domain.TaskRecord{StepSeq: seq, StageSeq: step.StageSeq, State: "done", DoneOn: domain.FormatDay(businessDay(now)),
		SOPVersionID: cur.SOPVersionID, SeasonEventID: cur.CurrentEventID, Step: step}
	l := store.FarmLog{ID: id, TenantID: a.p.TenantID, LogRef: id, SeasonRef: cur.SeasonRef, HouseholdRef: cur.HouseholdRef,
		CooperativeRef: cur.CooperativeRef, CommuneCode: cur.CommuneCode, ActivityCode: step.ActivityCode, SOPStepSeq: &stepSeq,
		GPS: in.GPS, LocationStatus: loc, Inputs: []domain.Input{}, BecameCurrent: true, OnBehalf: a.onBehalf(), Channel: in.Channel,
		EventTime: now, ActorRef: a.p.ActorRef, IdemKey: key + ":summary", EntryKind: "household", TaskCompletion: &meta}
	if err := store.InsertFarmLog(ctx, tx, &l); err != nil {
		return err
	}
	if err := store.SaveHouseholdTask(ctx, tx, l); err != nil {
		return err
	}
	return enqueue(ctx, tx, a.p.TenantID, port.EventHouseholdTaskCompleted, now, port.HouseholdTaskCompletedV1{LogID: l.ID,
		SeasonRef: l.SeasonRef, HouseholdRef: l.HouseholdRef, CooperativeRef: l.CooperativeRef, SOPVersionID: meta.SOPVersionID,
		StepSeq: meta.StepSeq, DoneOn: meta.DoneOn, GPSMissing: !l.GPS.HasPoint(), ActorRef: l.ActorRef, EventTime: now.Format(time.RFC3339)})
}
