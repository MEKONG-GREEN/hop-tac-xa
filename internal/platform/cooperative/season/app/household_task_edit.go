package app

// AH-126 (FN-05.4, FN-05-BS-03, FN-05-BS-04, FN-05-BR-07): Thành viên HTX xem, sửa, xóa mềm nhật ký việc của vụ theo hộ
// và bỏ tích việc đã xong. Sửa và xóa đều là bản ghi bù chỉ ghi thêm của sổ farm_log (cùng log_ref, supersedes_id =
// phiên bản hiện hành, correction_reason là lý do bắt buộc); GPS, ảnh và thời điểm ghi giữ như bản đầu. Xóa mềm/bỏ tích
// ghi trạng thái "voided": bản ghi ẩn khỏi checklist và tiến độ, việc về chưa xong nếu không còn bản ghi "Đã xong"
// nào khác. Lịch sử đọc từ các phiên bản của sổ.

import (
	"context"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/store"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
)

// MinTaskEditReasonLen: lý do sửa/xóa tối thiểu (ràng buộc correction_reason của sổ).
const MinTaskEditReasonLen = 10

var (
	ErrTaskRecordNotFound = problem.New(http.StatusNotFound, "cropcycle.task_record_not_found", "Không tìm thấy bản ghi nhật ký của vụ")
	ErrTaskRecordChanged  = problem.New(http.StatusConflict, "cropcycle.task_record_changed", "Bản ghi đã được sửa ở nơi khác; tải lại rồi sửa tiếp")
	ErrTaskRecordVoided   = problem.New(http.StatusConflict, "cropcycle.task_record_voided", "Bản ghi đã bị xóa")
)

// TaskEditInput: sửa (Void=false: State, DoneOn, Materials, Note, ZeroConfirmed) hoặc xóa mềm/bỏ tích (Void=true).
type TaskEditInput struct {
	CooperativeRef, HouseholdRef, SeasonRef, LogRef uuid.UUID
	ExpectedID                                      uuid.UUID
	Void                                            bool
	Reason                                          string
	State, DoneOn, Note                             string
	Materials                                       []ActualMaterialInput
	ZeroConfirmed                                   bool
	Channel, IdemKey                                string
}

func normalizeTaskEditReason(reason string) (string, error) {
	reason, err := domain.NormalizeNote("reason", reason)
	if err != nil {
		return "", fieldProblem(err)
	}
	if utf8.RuneCountInString(strings.TrimSpace(reason)) < MinTaskEditReasonLen {
		return "", problem.Validation.WithField("reason", "Nhập lý do sửa hoặc xóa (ít nhất 10 ký tự)")
	}
	if utf8.RuneCountInString(reason) > 500 {
		return "", problem.Validation.WithField("reason", "Lý do tối đa 500 ký tự")
	}
	return strings.TrimSpace(reason), nil
}

// currentTaskRecord đọc phiên bản hiện hành của bản ghi và kiểm phạm vi vụ.
func currentTaskRecord(ctx context.Context, tx pgx.Tx, in TaskEditInput) (*store.FarmLog, error) {
	versions, err := store.FarmLogVersions(ctx, tx, in.LogRef)
	if err != nil {
		return nil, err
	}
	var cur *store.FarmLog
	for i := range versions {
		if versions[i].BecameCurrent {
			cur = &versions[i]
		}
	}
	if cur == nil || cur.EntryKind != "household" || cur.TaskCompletion == nil || cur.SeasonRef != in.SeasonRef ||
		cur.HouseholdRef != in.HouseholdRef || cur.CooperativeRef != in.CooperativeRef {
		return nil, ErrTaskRecordNotFound
	}
	return cur, nil
}

// EditHouseholdTask ghi bản ghi bù cho một nhật ký việc. Danh mục vật tư được kiểm trước giao dịch (không gọi port
// trong giao dịch); phạm vi, trạng thái vụ, phiên bản và "một bản Đã xong cho mỗi việc" kiểm lại dưới khóa của vụ.
func (s *Service) EditHouseholdTask(ctx context.Context, in TaskEditInput) (CompleteTaskResult, error) {
	action := htxroles.Update
	if in.Void {
		action = htxroles.Delete
	}
	if _, err := s.matrixAccess(ctx, fnLog, action); err != nil {
		return CompleteTaskResult{}, err
	}
	a, err := s.householdAccess(ctx, in.CooperativeRef, true)
	if err != nil {
		return CompleteTaskResult{}, err
	}
	if in.IdemKey == "" {
		return CompleteTaskResult{}, problem.IdempotencyRequired
	}
	if in.LogRef == uuid.Nil || in.ExpectedID == uuid.Nil {
		return CompleteTaskResult{}, problem.Validation.WithField("expected_record_id", "Chọn bản ghi và phiên bản hợp lệ")
	}
	if in.Reason, err = normalizeTaskEditReason(in.Reason); err != nil {
		return CompleteTaskResult{}, err
	}
	if in.Channel == "" {
		in.Channel = ChannelApp
	}
	if in.Channel != ChannelApp && in.Channel != ChannelWeb {
		return CompleteTaskResult{}, problem.Validation.WithField("channel", "app hoặc web")
	}
	if s.dependencyTxOpen(ctx) {
		return CompleteTaskResult{}, ErrDependency.WithDetail("Danh mục phải được kiểm trước giao dịch nhật ký")
	}
	key := "household-edit:" + a.p.ActorRef.String() + ":" + in.IdemKey
	// Đọc bản hiện hành để kiểm dữ liệu sửa như khi ghi (việc, vật tư khuyến cáo được giữ trong bản ghi).
	var prev *store.FarmLog
	var season *store.Season
	err = s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		var e error
		if season, e = householdTaskSeason(ctx, tx, a.p.TenantID, in.CooperativeRef, in.HouseholdRef, in.SeasonRef, false); e != nil {
			return e
		}
		prev, e = currentTaskRecord(ctx, tx, in)
		return e
	})
	if err != nil {
		return CompleteTaskResult{}, err
	}
	var detail DetailedTaskInput
	var actual []domain.ActualMaterial
	var inputs []domain.Input
	if !in.Void {
		detail, _, err = normalizeDetailedTask(DetailedTaskInput{StepSeq: prev.TaskCompletion.StepSeq, ExpectedEventID: season.CurrentEventID,
			State: in.State, DoneOn: in.DoneOn, Note: in.Note, Materials: in.Materials, ZeroConfirmed: in.ZeroConfirmed,
			GPS: domain.GPS{Source: domain.GPSNoDevice}, Channel: in.Channel, IdemKey: in.IdemKey}, s.now())
		if err != nil {
			return CompleteTaskResult{}, err
		}
		// Ngày làm xong không sau ngày ghi bản đầu (thời điểm ghi giữ nguyên).
		if day, _ := domain.ParseDay(detail.DoneOn); day.After(businessDay(prev.EventTime)) {
			return CompleteTaskResult{}, problem.Validation.WithField("done_on", "Ngày làm xong không được sau ngày ghi")
		}
		if actual, inputs, err = s.detailedMaterials(ctx, *season, prev.TaskCompletion.Step, detail.Materials); err != nil {
			return CompleteTaskResult{}, err
		}
	}
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
			if old.LogRef != in.LogRef || old.ActorRef != a.p.ActorRef || old.SupersedesID == nil || *old.SupersedesID != in.ExpectedID ||
				(old.TaskCompletion.State == "voided") != in.Void {
				return problem.IdempotencyConflict
			}
			out.Record, out.Replayed = *old, true
		} else {
			if cur.Status != "open" && cur.Status != "completed" {
				return ErrTaskState
			}
			last, err := currentTaskRecord(ctx, tx, in)
			if err != nil {
				return err
			}
			if last.TaskCompletion.State == "voided" {
				return ErrTaskRecordVoided
			}
			if last.ID != in.ExpectedID {
				return ErrTaskRecordChanged.With("current_record_id", last.ID)
			}
			meta := *last.TaskCompletion
			next := *last
			if in.Void {
				meta.State = "voided"
			} else {
				if detail.State == "done" && meta.State != "done" {
					// Mỗi việc chỉ một bản "Đã xong" hiện hành.
					logs, err := store.HouseholdTasks(ctx, tx, cur.SeasonRef)
					if err != nil {
						return err
					}
					for _, l := range logs {
						if l.LogRef != last.LogRef && l.TaskCompletion != nil && l.TaskCompletion.StepSeq == meta.StepSeq && l.TaskCompletion.State == "done" {
							return ErrTaskDone
						}
					}
				}
				details := domain.TaskDetails{ActualMaterials: actual, ZeroConfirmed: detail.ZeroConfirmed}
				if meta.Details != nil {
					details.Backfilled, details.BackfillReason, details.RequestHash = meta.Details.Backfilled, meta.Details.BackfillReason, meta.Details.RequestHash
				}
				meta.State, meta.DoneOn, meta.Details = detail.State, detail.DoneOn, &details
				next.Inputs, next.Note = inputs, detail.Note
			}
			sup, reason := last.ID, in.Reason
			next.ID, next.Version, next.TaskCompletion = ids.New(), 0, &meta
			next.SupersedesID, next.CorrectionReason = &sup, &reason
			next.ActorRef, next.OnBehalf, next.Channel, next.DeviceID, next.IdemKey = a.p.ActorRef, a.onBehalf(), in.Channel, nil, key
			next.BecameCurrent, next.RecordedAt = true, time.Time{}
			if err := store.InsertFarmLog(ctx, tx, &next); err != nil {
				return err
			}
			if err := store.SaveHouseholdTask(ctx, tx, next); err != nil {
				return err
			}
			if err := store.RefreshYield(ctx, tx, next.SeasonRef); err != nil {
				return err
			}
			out.Record = next
		}
		out.Checklist, err = s.householdChecklistTx(ctx, tx, *cur, true)
		return err
	})
	return out, err
}

// HouseholdTaskHistory trả mọi phiên bản của một nhật ký việc theo thứ tự ghi (người xem được vụ đều xem được).
func (s *Service) HouseholdTaskHistory(ctx context.Context, coop, hh, season, logRef uuid.UUID) ([]store.FarmLog, error) {
	if _, err := s.matrixAccess(ctx, fnLog, htxroles.Read); err != nil {
		return nil, err
	}
	a, err := s.householdAccess(ctx, coop, false)
	if err != nil {
		return nil, err
	}
	var out []store.FarmLog
	err = s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		if _, err := householdTaskSeason(ctx, tx, a.p.TenantID, coop, hh, season, false); err != nil {
			return err
		}
		if _, err := currentTaskRecord(ctx, tx, TaskEditInput{CooperativeRef: coop, HouseholdRef: hh, SeasonRef: season, LogRef: logRef}); err != nil {
			return err
		}
		versions, err := store.FarmLogVersions(ctx, tx, logRef)
		out = versions
		return err
	})
	return out, err
}
