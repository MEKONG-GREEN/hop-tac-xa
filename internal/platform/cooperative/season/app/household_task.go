package app

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/store"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
)

var (
	ErrTaskState   = problem.New(http.StatusUnprocessableEntity, "cropcycle.task_season_state", "Vụ đã dừng hoặc hoàn thành; không ghi thêm việc")
	ErrTaskDone    = problem.New(http.StatusConflict, "cropcycle.task_already_resolved", "Việc đã xong hoặc bỏ qua; tải lại checklist")
	ErrTaskChanged = problem.New(http.StatusConflict, "cropcycle.task_season_changed", "Vụ đã thay đổi; tải lại checklist trước khi xác nhận")
	ErrTaskLocked  = problem.New(http.StatusUnprocessableEntity, "cropcycle.task_stage_locked", "Vụ đã thu hoạch chỉ ghi việc chưa xong của giai đoạn cuối")
)

type HouseholdChecklist struct {
	Season   store.Season
	Progress domain.Checklist
	Records  []store.FarmLog
}
type CompleteTaskInput struct {
	CooperativeRef, HouseholdRef, SeasonRef, ExpectedEventID uuid.UUID
	StepSeq                                                  int
	GPS                                                      domain.GPS
	Channel, IdemKey                                         string
}
type CompleteTaskResult struct {
	Record    store.FarmLog
	Checklist HouseholdChecklist
	Replayed  bool
}

func householdTaskSeason(ctx context.Context, tx pgx.Tx, tid, coop, hh, season uuid.UUID, lock bool) (*store.Season, error) {
	cur, err := store.SeasonByRef(ctx, tx, season, lock)
	if err != nil {
		return nil, err
	}
	if cur == nil || cur.EntryKind != "household" || cur.CooperativeRef != coop || cur.HouseholdRef != hh {
		return nil, ErrSeasonNotFound
	}
	ev, err := store.SeasonEventByID(ctx, tx, cur.CurrentEventID)
	if err != nil {
		return nil, err
	}
	if ev == nil || ev.TenantID != tid {
		return nil, ErrSeasonNotFound
	}
	return cur, nil
}

func (s *Service) householdChecklistTx(ctx context.Context, tx pgx.Tx, cur store.Season, canWrite bool) (HouseholdChecklist, error) {
	var snap domain.SeasonSnapshot
	if err := json.Unmarshal(cur.SOPSnapshot, &snap); err != nil {
		return HouseholdChecklist{}, err
	}
	var actual *time.Time
	if cur.HarvestID != uuid.Nil {
		h, err := store.HarvestByID(ctx, tx, cur.HarvestID)
		if err != nil {
			return HouseholdChecklist{}, err
		}
		if h != nil && h.SeasonRef == cur.SeasonRef {
			d := h.HarvestDate
			actual = &d
		}
	}
	plan, err := snap.Schedule(cur.SowingDate, s.today(), actual)
	if err != nil {
		return HouseholdChecklist{}, fieldProblem(err)
	}
	logs, err := store.HouseholdTasks(ctx, tx, cur.SeasonRef)
	if err != nil {
		return HouseholdChecklist{}, err
	}
	records := []domain.TaskRecord{}
	for _, l := range logs {
		if l.TaskCompletion != nil {
			records = append(records, *l.TaskCompletion)
		}
	}
	progress, err := domain.BuildSeasonChecklist(plan, records, cur.Status, snap.SkippedSet())
	if err != nil {
		return HouseholdChecklist{}, fieldProblem(err)
	}
	if !canWrite {
		for i := range progress.Stages {
			for j := range progress.Stages[i].Tasks {
				progress.Stages[i].Tasks[j].CanComplete = false
			}
		}
	}
	return HouseholdChecklist{Season: cur, Progress: progress, Records: logs}, nil
}

func (s *Service) HouseholdChecklist(ctx context.Context, coop, hh, season uuid.UUID) (HouseholdChecklist, error) {
	if _, err := s.matrixAccess(ctx, fnLog, htxroles.Read); err != nil {
		return HouseholdChecklist{}, err
	}
	a, err := s.householdAccess(ctx, coop, false)
	if err != nil {
		return HouseholdChecklist{}, err
	}
	_, writeErr := s.matrixAccess(ctx, fnLog, htxroles.Create)
	var out HouseholdChecklist
	err = s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		cur, err := householdTaskSeason(ctx, tx, a.p.TenantID, coop, hh, season, false)
		if err != nil {
			return err
		}
		out, err = s.householdChecklistTx(ctx, tx, *cur, writeErr == nil)
		return err
	})
	return out, err
}

// CompleteHouseholdTask does not call external ports. Scope and state are checked
// again under the season lock, including a ledger retry after HTTP receipt expiry.
func (s *Service) CompleteHouseholdTask(ctx context.Context, in CompleteTaskInput) (CompleteTaskResult, error) {
	if _, err := s.matrixAccess(ctx, fnLog, htxroles.Create); err != nil {
		return CompleteTaskResult{}, err
	}
	a, err := s.householdAccess(ctx, in.CooperativeRef, true)
	if err != nil {
		return CompleteTaskResult{}, err
	}
	if in.IdemKey == "" {
		return CompleteTaskResult{}, problem.IdempotencyRequired
	}
	if in.StepSeq < 1 || in.StepSeq > domain.MaxPlanStepSeq || in.ExpectedEventID == uuid.Nil {
		return CompleteTaskResult{}, problem.Validation.WithField("step_seq", "chọn việc và phiên bản vụ hợp lệ")
	}
	if in.Channel == "" {
		in.Channel = ChannelApp
	}
	if in.Channel != ChannelApp && in.Channel != ChannelWeb {
		return CompleteTaskResult{}, problem.Validation.WithField("channel", "app hoặc web")
	}
	if err := in.GPS.Normalize(); err != nil {
		return CompleteTaskResult{}, fieldProblem(err)
	}
	if in.GPS.Source != "device" && in.GPS.Source != "denied" && in.GPS.Source != "no_device" {
		return CompleteTaskResult{}, problem.Validation.WithField("gps.source", "tích nhanh chỉ lấy GPS thiết bị hoặc nhãn thiếu GPS")
	}
	if in.GPS.HasPoint() {
		lon, lat := in.GPS.Lon.Round(7), in.GPS.Lat.Round(7)
		in.GPS.Lon, in.GPS.Lat = &lon, &lat
	}
	if in.GPS.AccuracyM != nil {
		if in.GPS.AccuracyM.GreaterThan(decimal.RequireFromString("999999.99")) {
			return CompleteTaskResult{}, problem.Validation.WithField("gps.accuracy_m", "tối đa 999999.99")
		}
		n := in.GPS.AccuracyM.Round(2)
		in.GPS.AccuracyM = &n
	}
	key := "household-task:" + a.p.ActorRef.String() + ":" + in.IdemKey
	var out CompleteTaskResult
	err = s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		cur, err := householdTaskSeason(ctx, tx, a.p.TenantID, in.CooperativeRef, in.HouseholdRef, in.SeasonRef, true)
		if err != nil {
			return err
		}
		old, err := store.FarmLogByIdem(ctx, tx, key)
		if err != nil {
			return err
		}
		if old != nil {
			m := old.TaskCompletion
			if old.EntryKind != "household" || m == nil || old.ActorRef != a.p.ActorRef || old.SeasonRef != in.SeasonRef || old.CooperativeRef != in.CooperativeRef || old.HouseholdRef != in.HouseholdRef || m.StepSeq != in.StepSeq || m.SeasonEventID != in.ExpectedEventID || old.Channel != in.Channel || !sameHouseholdGPS(old.GPS, in.GPS) {
				return problem.IdempotencyConflict
			}
			out.Record, out.Replayed = *old, true
		} else {
			if cur.Status != "open" && cur.Status != "completed" {
				return ErrTaskState
			}
			if cur.CurrentEventID != in.ExpectedEventID {
				return ErrTaskChanged.With("current_event_id", cur.CurrentEventID)
			}
			check, err := s.householdChecklistTx(ctx, tx, *cur, true)
			if err != nil {
				return err
			}
			var selected *domain.TaskProgress
			for i := range check.Progress.Stages {
				for j := range check.Progress.Stages[i].Tasks {
					t := &check.Progress.Stages[i].Tasks[j]
					if t.Seq == in.StepSeq {
						selected = t
					}
				}
			}
			if selected == nil {
				return problem.Validation.WithField("step_seq", "việc không thuộc snapshot của vụ")
			}
			if selected.DoneOn != nil || selected.CompletionStatus == "skipped" {
				return ErrTaskDone
			}
			if !selected.CanComplete {
				return ErrTaskLocked
			}
			var snapshot domain.SeasonSnapshot
			if err := json.Unmarshal(cur.SOPSnapshot, &snapshot); err != nil {
				return err
			}
			step, ok := snapshot.Step(in.StepSeq)
			if !ok {
				return ErrDependency
			}
			inputs := []domain.Input{}
			for _, m := range step.Materials {
				name := m.Name
				if name == "" {
					name = m.MaterialCode
				}
				inputs = append(inputs, domain.Input{Name: name, Qty: m.DosePerHa.Mul(cur.AreaHa), Unit: m.Unit})
			}
			now := s.now()
			id := ids.New()
			location := "missing"
			if in.GPS.HasPoint() {
				location = "unverified"
			}
			meta := domain.TaskRecord{StepSeq: in.StepSeq, StageSeq: step.StageSeq, State: "done", DoneOn: domain.FormatDay(businessDay(now)), SOPVersionID: cur.SOPVersionID, SeasonEventID: cur.CurrentEventID, Step: step}
			l := store.FarmLog{ID: id, TenantID: a.p.TenantID, LogRef: id, SeasonRef: cur.SeasonRef, HouseholdRef: cur.HouseholdRef, CooperativeRef: cur.CooperativeRef, CommuneCode: cur.CommuneCode, ActivityCode: step.ActivityCode, SOPStepSeq: &in.StepSeq,
				GPS: in.GPS, LocationStatus: location, Inputs: inputs, BecameCurrent: true, OnBehalf: a.onBehalf(), Channel: in.Channel, EventTime: now, ActorRef: a.p.ActorRef, IdemKey: key, EntryKind: "household", TaskCompletion: &meta}
			if err := store.InsertFarmLog(ctx, tx, &l); err != nil {
				return err
			}
			if err := store.SaveHouseholdTask(ctx, tx, l); err != nil {
				return err
			}
			if err := store.RefreshYield(ctx, tx, l.SeasonRef); err != nil {
				return err
			}
			if err := enqueue(ctx, tx, a.p.TenantID, port.EventHouseholdTaskCompleted, now, port.HouseholdTaskCompletedV1{LogID: l.ID, SeasonRef: l.SeasonRef, HouseholdRef: l.HouseholdRef, CooperativeRef: l.CooperativeRef, SOPVersionID: meta.SOPVersionID, StepSeq: meta.StepSeq, DoneOn: meta.DoneOn, GPSMissing: !l.GPS.HasPoint(), ActorRef: l.ActorRef, EventTime: now.Format(time.RFC3339)}); err != nil {
				return err
			}
			out.Record = l
		}
		out.Checklist, err = s.householdChecklistTx(ctx, tx, *cur, true)
		return err
	})
	return out, err
}
