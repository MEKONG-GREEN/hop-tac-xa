package app

// AH-176 (FN-05-BS-01, KH-17): Thành viên HTX chọn nhiều vụ cùng làm một việc và ghi một lần. Chỉ các vụ của HTX
// mình, dùng cùng một SOP gốc (sop_template_ref) và có cùng việc đó (cùng tên và loại việc) chưa xong. Mỗi vụ có một
// bản ghi riêng qua đúng luồng ghi chi tiết (AH-99) với ngày, liều, ghi chú, GPS chung; vụ không ghi được bị bỏ qua
// kèm mã và lý do. Idempotency-Key của thao tác sinh khóa riêng cho từng vụ nên thử lại không tạo bản ghi trùng.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/store"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
)

// MaxBatchSeasons giới hạn số vụ của một lần ghi nhiều vụ.
const MaxBatchSeasons = 50

var ErrBatchTaskMismatch = problem.New(http.StatusConflict, "cropcycle.batch_task_mismatch", "Vụ không dùng cùng SOP gốc hoặc không có việc này")

// TaskBatchTarget là một vụ được chọn để ghi chung.
type TaskBatchTarget struct {
	HouseholdRef, SeasonRef, ExpectedEventID uuid.UUID
}

// TaskBatchInput là một việc chung ghi cho nhiều vụ. Vụ nguồn (đường dẫn) xác định SOP gốc và việc.
type TaskBatchInput struct {
	CooperativeRef, HouseholdRef, SeasonRef uuid.UUID
	StepSeq                                 int
	Targets                                 []TaskBatchTarget
	State, DoneOn, Note, BackfillReason     string
	Materials                               []ActualMaterialInput
	FileRefs                                []uuid.UUID
	ZeroConfirmed                           bool
	GPS                                     domain.GPS
	Channel, IdemKey                        string
}

// TaskBatchResult là kết quả của một vụ: recorded (kèm bản ghi) hoặc skipped (kèm mã và lý do).
type TaskBatchResult struct {
	HouseholdRef, SeasonRef uuid.UUID
	StepSeq                 int
	Status, Code, Reason    string
	Record                  *store.FarmLog
	Replayed                bool
}

// TaskBatchCandidate là một vụ có thể chọn cùng vụ nguồn.
type TaskBatchCandidate struct {
	Season   store.Season
	StepSeq  int
	Eligible bool
	Code     string
	Reason   string
}

type batchStep struct {
	template uuid.UUID
	name     string
	activity string
}

// sourceBatchStep đọc SOP gốc và việc của vụ nguồn.
func sourceBatchStep(cur store.Season, stepSeq int) (batchStep, error) {
	var snap domain.SeasonSnapshot
	if err := json.Unmarshal(cur.SOPSnapshot, &snap); err != nil {
		return batchStep{}, err
	}
	step, ok := snap.Step(stepSeq)
	if !ok || stepSeq > domain.MaxPlanStepSeq {
		return batchStep{}, problem.Validation.WithField("step_seq", "Việc không thuộc quy trình của vụ")
	}
	return batchStep{template: cur.SOPTemplateRef, name: normName(step.Name), activity: step.ActivityCode}, nil
}

func normName(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }

// matchBatchStep tìm việc cùng tên và loại trong vụ đích (số việc có thể khác giữa các phiên bản của cùng SOP gốc).
func matchBatchStep(cur store.Season, src batchStep, hint int) (int, bool) {
	if cur.SOPTemplateRef != src.template {
		return 0, false
	}
	var snap domain.SeasonSnapshot
	if err := json.Unmarshal(cur.SOPSnapshot, &snap); err != nil {
		return 0, false
	}
	same := func(st domain.Step) bool { return normName(st.Name) == src.name && st.ActivityCode == src.activity }
	if st, ok := snap.Step(hint); ok && same(st) {
		return hint, true
	}
	content, ids, err := snap.Effective()
	if err != nil {
		return 0, false
	}
	found := 0
	for i, st := range content.Steps {
		if same(st) {
			if found != 0 {
				return 0, false // trùng tên trong vụ đích: không đoán
			}
			found = ids[i]
		}
	}
	return found, found != 0
}

// batchTaskStatus trả lý do vụ không ghi được việc (rỗng nếu ghi được).
func (s *Service) batchTaskStatus(ctx context.Context, tx pgx.Tx, cur store.Season, seq int) (string, string, error) {
	if cur.Status != "open" && cur.Status != "completed" {
		return ErrTaskState.Code, ErrTaskState.Title, nil
	}
	list, err := s.householdChecklistTx(ctx, tx, cur, true)
	if err != nil {
		return "", "", err
	}
	for _, st := range list.Progress.Stages {
		for _, t := range st.Tasks {
			if t.Seq != seq {
				continue
			}
			switch {
			case t.DoneOn != nil || t.CompletionStatus == "skipped":
				return ErrTaskDone.Code, ErrTaskDone.Title, nil
			case !t.CanComplete:
				return ErrTaskLocked.Code, ErrTaskLocked.Title, nil
			}
			return "", "", nil
		}
	}
	return ErrBatchTaskMismatch.Code, ErrBatchTaskMismatch.Title, nil
}

// HouseholdTaskBatchCandidates liệt kê các vụ của HTX dùng cùng SOP gốc với vụ nguồn, kèm việc tương ứng và việc đó
// còn ghi được không (KHMV-FN-05 C).
func (s *Service) HouseholdTaskBatchCandidates(ctx context.Context, coop, hh, season uuid.UUID, stepSeq int) ([]TaskBatchCandidate, error) {
	if _, err := s.matrixAccess(ctx, fnLog, htxroles.Create); err != nil {
		return nil, err
	}
	a, err := s.householdAccess(ctx, coop, true)
	if err != nil {
		return nil, err
	}
	var out []TaskBatchCandidate
	err = s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		cur, err := householdTaskSeason(ctx, tx, a.p.TenantID, coop, hh, season, false)
		if err != nil {
			return err
		}
		src, err := sourceBatchStep(*cur, stepSeq)
		if err != nil {
			return err
		}
		rows, err := store.HouseholdSeasonsByTemplate(ctx, tx, coop, cur.SOPTemplateRef, 200)
		if err != nil {
			return err
		}
		out = make([]TaskBatchCandidate, 0, len(rows))
		for _, r := range rows {
			c := TaskBatchCandidate{Season: r}
			seq, ok := matchBatchStep(r, src, stepSeq)
			if !ok {
				c.Code, c.Reason = ErrBatchTaskMismatch.Code, ErrBatchTaskMismatch.Title
			} else {
				c.StepSeq = seq
				if c.Code, c.Reason, err = s.batchTaskStatus(ctx, tx, r, seq); err != nil {
					return err
				}
			}
			c.Eligible = c.Code == ""
			out = append(out, c)
		}
		return nil
	})
	return out, err
}

// RecordHouseholdTaskBatch ghi cùng một việc cho nhiều vụ. Mỗi vụ đi qua RecordHouseholdTask (kiểm quyền, trạng thái,
// liều, ghi bù dưới khóa vụ); lỗi nghiệp vụ của một vụ thành kết quả skipped, lỗi hệ thống dừng thao tác (thử lại an
// toàn vì khóa idempotency riêng từng vụ).
func (s *Service) RecordHouseholdTaskBatch(ctx context.Context, in TaskBatchInput) ([]TaskBatchResult, error) {
	if _, err := s.matrixAccess(ctx, fnLog, htxroles.Create); err != nil {
		return nil, err
	}
	a, err := s.householdAccess(ctx, in.CooperativeRef, true)
	if err != nil {
		return nil, err
	}
	if len(in.Targets) == 0 || len(in.Targets) > MaxBatchSeasons {
		return nil, problem.Validation.WithField("seasons", "Chọn từ 1 đến 50 vụ")
	}
	households, seen := map[uuid.UUID]bool{}, map[uuid.UUID]bool{}
	for _, t := range in.Targets {
		if t.SeasonRef == uuid.Nil || t.HouseholdRef == uuid.Nil || t.ExpectedEventID == uuid.Nil || seen[t.SeasonRef] {
			return nil, problem.Validation.WithField("seasons", "Vụ không hợp lệ hoặc bị trùng")
		}
		seen[t.SeasonRef], households[t.HouseholdRef] = true, true
	}
	// Ảnh mã hóa theo khóa của từng hộ (AH-99) nên chỉ dùng chung khi mọi vụ cùng một hộ.
	if len(in.FileRefs) > 0 && (len(households) > 1 || !households[in.HouseholdRef]) {
		return nil, problem.Validation.WithField("file_refs", "Ảnh mã hóa theo hộ; chỉ đính kèm khi các vụ cùng một hộ với vụ đang mở")
	}
	// Kiểm chung một lần (ngày, kết quả, vật tư, GPS) để lỗi nhập không lặp lại ở từng vụ.
	if _, _, err := normalizeDetailedTask(DetailedTaskInput{StepSeq: 1, ExpectedEventID: in.Targets[0].ExpectedEventID, State: in.State, DoneOn: in.DoneOn,
		Note: in.Note, BackfillReason: in.BackfillReason, Materials: in.Materials, FileRefs: in.FileRefs, ZeroConfirmed: in.ZeroConfirmed, GPS: in.GPS,
		Channel: in.Channel, IdemKey: in.IdemKey}, s.now()); err != nil {
		return nil, err
	}
	var src batchStep
	steps := map[uuid.UUID]int{}
	if err := s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		cur, err := householdTaskSeason(ctx, tx, a.p.TenantID, in.CooperativeRef, in.HouseholdRef, in.SeasonRef, false)
		if err != nil {
			return err
		}
		if src, err = sourceBatchStep(*cur, in.StepSeq); err != nil {
			return err
		}
		for _, t := range in.Targets {
			tc, err := householdTaskSeason(ctx, tx, a.p.TenantID, in.CooperativeRef, t.HouseholdRef, t.SeasonRef, false)
			if errors.Is(err, ErrSeasonNotFound) {
				continue
			}
			if err != nil {
				return err
			}
			if seq, ok := matchBatchStep(*tc, src, in.StepSeq); ok {
				steps[t.SeasonRef] = seq
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	out := make([]TaskBatchResult, 0, len(in.Targets))
	for _, t := range in.Targets {
		r := TaskBatchResult{HouseholdRef: t.HouseholdRef, SeasonRef: t.SeasonRef, StepSeq: steps[t.SeasonRef]}
		if r.StepSeq == 0 {
			r.Status, r.Code, r.Reason = "skipped", ErrBatchTaskMismatch.Code, ErrBatchTaskMismatch.Title
			out = append(out, r)
			continue
		}
		res, err := s.RecordHouseholdTask(ctx, DetailedTaskInput{CooperativeRef: in.CooperativeRef, HouseholdRef: t.HouseholdRef, SeasonRef: t.SeasonRef,
			ExpectedEventID: t.ExpectedEventID, StepSeq: r.StepSeq, State: in.State, DoneOn: in.DoneOn, Note: in.Note, BackfillReason: in.BackfillReason,
			Materials: in.Materials, FileRefs: in.FileRefs, ZeroConfirmed: in.ZeroConfirmed, GPS: in.GPS, Channel: in.Channel,
			IdemKey: "batch:" + in.IdemKey + ":" + t.SeasonRef.String()})
		var p *problem.Problem
		switch {
		case err == nil:
			rec := res.Record
			r.Status, r.Record, r.Replayed = "recorded", &rec, res.Replayed
		case errors.As(err, &p) && p.Status < http.StatusInternalServerError && p.Status != http.StatusUnauthorized && p.Status != http.StatusForbidden:
			r.Status, r.Code, r.Reason = "skipped", p.Code, p.Title
			if p.Detail != "" {
				r.Reason = p.Detail
			}
			if len(p.Errors) > 0 {
				r.Reason = p.Errors[0].Reason
			}
		default:
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}
