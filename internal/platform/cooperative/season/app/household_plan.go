package app

// AH-113 (FN-03.3, FN-03.6, FN-03-EX-04, FN-03-BR-04, FN-03-BR-06): Thành viên HTX tùy chỉnh quy trình của riêng vụ
// theo hộ — khi mở vụ (danh sách thay đổi gửi kèm yêu cầu mở vụ/xem trước) và khi vụ đang canh tác (từng thay đổi một).
// Tùy chỉnh chỉ đổi bản chụp của vụ (domain.SeasonSnapshot), SOP chuẩn không đổi. Mỗi thay đổi khi đang chạy là một
// sự kiện "corrected" của sổ season_event (chỉ ghi thêm): người, thời điểm và câu mô tả thay đổi là lịch sử.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	port "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/store"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
)

// MaxPlanChanges là số thay đổi tối đa gửi kèm một lần mở vụ.
const MaxPlanChanges = 200

var (
	ErrPlanStepResolved = problem.New(http.StatusConflict, "cropcycle.plan_step_resolved", "Việc đã xong; không sửa, đổi chỗ hay bỏ được")
	ErrPlanStepSkipped  = problem.New(http.StatusConflict, "cropcycle.plan_step_skipped", "Việc đã bỏ qua khỏi quy trình của vụ")
	ErrPlanSeasonState  = problem.New(http.StatusConflict, "cropcycle.plan_season_closed", "Chỉ tùy chỉnh quy trình của vụ đang canh tác")
)

// planProblem đổi lỗi của domain.SeasonSnapshot.Apply thành problem; prefix gắn trước tên trường (vd "plan_changes[2].").
func planProblem(err error, prefix string) error {
	switch {
	case errors.Is(err, domain.ErrPlanStepResolved):
		return ErrPlanStepResolved
	case errors.Is(err, domain.ErrPlanStepSkipped):
		return ErrPlanStepSkipped
	case errors.Is(err, domain.ErrPlanStepUnknown):
		return problem.Validation.WithField(prefix+"step_seq", "việc không thuộc quy trình của vụ")
	}
	var fes domain.FieldErrors
	if prefix != "" && errors.As(err, &fes) {
		out := make(domain.FieldErrors, len(fes))
		for i, fe := range fes {
			out[i] = domain.FieldError{Field: prefix + fe.Field, Reason: fe.Reason}
		}
		return fieldProblem(out)
	}
	return fieldProblem(err)
}

// applyPlanChanges áp lần lượt các thay đổi lúc mở vụ/xem trước (chưa có việc nào xong).
func applyPlanChanges(snap domain.SeasonSnapshot, changes []domain.PlanChange) (domain.SeasonSnapshot, error) {
	if len(changes) > MaxPlanChanges {
		return snap, problem.Validation.WithField("plan_changes", "tối đa "+strconv.Itoa(MaxPlanChanges)+" thay đổi")
	}
	for i, ch := range changes {
		next, _, err := snap.Apply(ch, nil, nil)
		if err != nil {
			return snap, planProblem(err, "plan_changes["+strconv.Itoa(i)+"].")
		}
		snap = next
	}
	return snap, nil
}

// PlanChangeInput là một thay đổi quy trình của vụ đang canh tác.
type PlanChangeInput struct {
	CooperativeRef, HouseholdRef, SeasonRef uuid.UUID
	ExpectedEventID                         uuid.UUID
	Change                                  domain.PlanChange
	IdemKey                                 string
}

// PlanChangeResult trả vụ sau thay đổi, checklist tính lại và câu mô tả đã ghi lịch sử.
type PlanChangeResult struct {
	Season    store.Season
	Event     store.SeasonEvent
	Checklist HouseholdChecklist
	Summary   string
	Replayed  bool
}

// ChangeHouseholdPlan ghi một thay đổi quy trình (KHMV-FN-04 U). Không gọi port ngoài trong giao dịch; kiểm phạm vi,
// trạng thái vụ, phiên bản vụ và việc đã xong dưới khóa của vụ.
func (s *Service) ChangeHouseholdPlan(ctx context.Context, in PlanChangeInput) (PlanChangeResult, error) {
	if _, err := s.matrixAccess(ctx, fnPlan, htxroles.Update); err != nil {
		return PlanChangeResult{}, err
	}
	a, err := s.householdAccess(ctx, in.CooperativeRef, true)
	if err != nil {
		return PlanChangeResult{}, err
	}
	if in.IdemKey == "" {
		return PlanChangeResult{}, problem.IdempotencyRequired
	}
	if in.ExpectedEventID == uuid.Nil {
		return PlanChangeResult{}, problem.Validation.WithField("expected_season_event_id", "phiên bản vụ không hợp lệ")
	}
	key := "household-plan:" + a.p.ActorRef.String() + ":" + in.IdemKey
	var out PlanChangeResult
	err = s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		cur, err := householdTaskSeason(ctx, tx, a.p.TenantID, in.CooperativeRef, in.HouseholdRef, in.SeasonRef, true)
		if err != nil {
			return err
		}
		if old, err := store.SeasonEventByIdem(ctx, tx, key); err != nil {
			return err
		} else if old != nil {
			// Phát lại: cùng người, cùng vụ, cùng phiên bản gốc thì trả kết quả đã ghi.
			if old.SeasonRef != in.SeasonRef || old.ActorRef != a.p.ActorRef || old.SupersedesID == nil || *old.SupersedesID != in.ExpectedEventID {
				return problem.IdempotencyConflict
			}
			out.Event, out.Replayed = *old, true
			if old.CorrectionReason != nil {
				out.Summary = *old.CorrectionReason
			}
		} else {
			if cur.Status != port.SeasonOpen {
				return ErrPlanSeasonState
			}
			if cur.CurrentEventID != in.ExpectedEventID {
				return ErrTaskChanged.With("current_event_id", cur.CurrentEventID)
			}
			var snap domain.SeasonSnapshot
			if err := json.Unmarshal(cur.SOPSnapshot, &snap); err != nil {
				return err
			}
			logs, err := store.HouseholdTasks(ctx, tx, cur.SeasonRef)
			if err != nil {
				return err
			}
			resolved, logged := map[int]bool{}, map[int]bool{}
			for _, l := range logs {
				if r := l.TaskCompletion; r != nil {
					logged[r.StepSeq] = true
					if r.State == "done" {
						resolved[r.StepSeq] = true
					}
				}
			}
			next, summary, err := snap.Apply(in.Change, resolved, logged)
			if err != nil {
				return planProblem(err, "")
			}
			raw, err := mergeSnapshot(cur.SOPSnapshot, next)
			if err != nil {
				return err
			}
			prev, err := store.SeasonEventByID(ctx, tx, cur.CurrentEventID)
			if err != nil {
				return err
			}
			now := s.now()
			ev := *prev
			ev.ID, ev.Seq, ev.EventTime, ev.ActorRef, ev.DeviceID, ev.IdemKey = ids.New(), 0, now, a.p.ActorRef, nil, key
			ev.OnBehalf, ev.Channel, ev.HarvestID, ev.Reason = a.onBehalf(), ChannelWeb, uuid.Nil, ""
			sup := cur.CurrentEventID
			ev.SupersedesID, ev.CorrectionReason = &sup, &summary
			ev.Action, ev.SOPSnapshot = "corrected", raw
			if err := store.InsertSeasonEvent(ctx, tx, &ev); err != nil {
				return err
			}
			season := store.ApplySeasonEvent(cur, ev)
			if err := store.SaveSeason(ctx, tx, a.p.TenantID, season); err != nil {
				return err
			}
			if err := afterSeasonChange(ctx, tx, a.p.TenantID, season, now); err != nil {
				return err
			}
			if err := enqueue(ctx, tx, a.p.TenantID, port.EventSeasonCorrected, now, port.SeasonCorrectedV1{
				SeasonRef: ev.SeasonRef, EventID: ev.ID, Action: ev.Action, Status: season.Status, PlotRef: ev.PlotRef,
				HouseholdRef: uuidPtr(ev.HouseholdRef), CooperativeRef: ev.CooperativeRef, CommuneCode: ev.CommuneCode,
				SeasonCode: ev.SeasonCode, SeasonYear: ev.SeasonYear, VarietyCode: ev.VarietyCode, SOPVersionID: ev.SOPVersionID,
				SowingDate: domain.FormatDay(ev.SowingDate), ExpectedHarvestDate: domain.FormatDay(ev.ExpectedHarvestDate),
				SupersedesID: sup, CorrectionReason: summary, ActorRef: ev.ActorRef, EventTime: now.Format(time.RFC3339),
			}); err != nil {
				return err
			}
			out.Event, out.Summary = ev, summary
		}
		fresh, err := store.SeasonByRef(ctx, tx, in.SeasonRef, false)
		if err != nil {
			return err
		}
		out.Season = *fresh
		out.Checklist, err = s.householdChecklistTx(ctx, tx, *fresh, true)
		return err
	})
	return out, err
}

// mergeSnapshot giữ nguyên các khóa khác của bản chụp (định danh SOP, mốc gốc...) và thay phần quy trình.
func mergeSnapshot(raw json.RawMessage, snap domain.SeasonSnapshot) (json.RawMessage, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	for k, v := range map[string]any{"stages": snap.Stages, "steps": snap.Steps, "custom": snap.Custom} {
		b, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		m[k] = b
	}
	if snap.Custom == nil {
		delete(m, "custom")
	}
	return json.Marshal(m)
}

// PlanHistoryEntry là một thay đổi quy trình đã ghi (ai, lúc nào, nội dung).
type PlanHistoryEntry struct {
	EventID   uuid.UUID `json:"event_id"`
	ActorRef  uuid.UUID `json:"actor_ref"`
	EventTime time.Time `json:"event_time"`
	Summary   string    `json:"summary"`
}

// HouseholdPlanHistory liệt kê lịch sử tùy chỉnh quy trình của vụ, mới nhất trước (người xem được vụ đều xem được).
func (s *Service) HouseholdPlanHistory(ctx context.Context, coop, hh, season uuid.UUID) ([]PlanHistoryEntry, error) {
	a, err := s.householdAccess(ctx, coop, false)
	if err != nil {
		return nil, err
	}
	out := []PlanHistoryEntry{}
	err = s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		if _, err := householdTaskSeason(ctx, tx, a.p.TenantID, coop, hh, season, false); err != nil {
			return err
		}
		events, err := store.SeasonEvents(ctx, tx, season)
		if err != nil {
			return err
		}
		for i := len(events) - 1; i >= 0; i-- {
			e := events[i]
			if e.Action == "corrected" && e.CorrectionReason != nil {
				out = append(out, PlanHistoryEntry{EventID: e.ID, ActorRef: e.ActorRef, EventTime: e.EventTime, Summary: *e.CorrectionReason})
			}
		}
		return nil
	})
	return out, err
}
