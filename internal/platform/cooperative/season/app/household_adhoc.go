package app

// AH-163 (FN-05.3, FN-05-BS-02): Thành viên HTX ghi việc phát sinh không có trong quy trình của vụ. Bản ghi dùng chung
// sổ farm_log của nhật ký việc (AH-99), gắn nhãn "Ngoài SOP" (task_completion.adhoc), mang một số việc riêng của vụ lớn
// hơn mọi số việc của quy trình (domain.AdhocStepSeqBase) nên checklist không tính vào % tiến độ. Kiểm như form chi tiết:
// nội dung, giai đoạn, loại việc, ngày bắt buộc; ngày không sau hôm nay; liều ≥ 0; ghi bù quá 3 ngày cần lý do.

import (
	"context"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	port "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/store"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
)

// AdhocTaskInput là một việc phát sinh ngoài quy trình.
type AdhocTaskInput struct {
	CooperativeRef, HouseholdRef, SeasonRef, ExpectedEventID uuid.UUID
	StageSeq                                                 int
	Name, ActivityCode                                       string
	State, DoneOn, Note, BackfillReason                      string
	Materials                                                []ActualMaterialInput
	ZeroConfirmed                                            bool
	GPS                                                      domain.GPS
	Channel, IdemKey                                         string
}

// RecordAdhocTask ghi việc phát sinh (KHMV-FN-05 C). Danh mục vật tư kiểm trước giao dịch; trạng thái vụ, giai đoạn và
// số việc cấp dưới khóa của vụ.
func (s *Service) RecordAdhocTask(ctx context.Context, in AdhocTaskInput) (CompleteTaskResult, error) {
	if _, err := s.matrixAccess(ctx, fnLog, htxroles.Create); err != nil {
		return CompleteTaskResult{}, err
	}
	a, err := s.householdAccess(ctx, in.CooperativeRef, true)
	if err != nil {
		return CompleteTaskResult{}, err
	}
	in.Name = strings.Join(strings.Fields(in.Name), " ")
	in.ActivityCode = strings.TrimSpace(in.ActivityCode)
	switch {
	case in.Name == "" || utf8.RuneCountInString(in.Name) > domain.MaxStepNameLen:
		return CompleteTaskResult{}, problem.Validation.WithField("name", "Nhập nội dung việc (tối đa 200 ký tự)")
	case domain.ContainsPII(in.Name):
		return CompleteTaskResult{}, problem.Validation.WithField("name", "Nội dung không được chứa SĐT hoặc CCCD")
	case !domain.KnownActivity(in.ActivityCode):
		return CompleteTaskResult{}, problem.Validation.WithField("activity_code", domain.MsgActivityChoice)
	case in.StageSeq < 1:
		return CompleteTaskResult{}, problem.Validation.WithField("stage_seq", "Chọn giai đoạn của vụ")
	}
	// Kiểm chung với form chi tiết (kết quả, ngày, ghi chú, vật tư, GPS); số việc tạm là 1 vì máy chủ cấp số riêng.
	detail, hash, err := normalizeDetailedTask(DetailedTaskInput{StepSeq: 1, ExpectedEventID: in.ExpectedEventID, State: in.State, DoneOn: in.DoneOn,
		Note: in.Note, BackfillReason: in.BackfillReason, Materials: in.Materials, ZeroConfirmed: in.ZeroConfirmed, GPS: in.GPS,
		Channel: in.Channel, IdemKey: in.IdemKey}, s.now())
	if err != nil {
		return CompleteTaskResult{}, err
	}
	if s.dependencyTxOpen(ctx) {
		return CompleteTaskResult{}, ErrDependency.WithDetail("Danh mục phải được kiểm trước giao dịch nhật ký")
	}
	key := "household-adhoc:" + a.p.ActorRef.String() + ":" + in.IdemKey
	var preflight *store.Season
	if err := s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		var e error
		preflight, e = householdTaskSeason(ctx, tx, a.p.TenantID, in.CooperativeRef, in.HouseholdRef, in.SeasonRef, false)
		return e
	}); err != nil {
		return CompleteTaskResult{}, err
	}
	actual, inputs, err := s.detailedMaterials(ctx, *preflight, domain.Step{}, detail.Materials)
	if err != nil {
		return CompleteTaskResult{}, err
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
			if old.ActorRef != a.p.ActorRef || old.SeasonRef != in.SeasonRef || old.TaskCompletion == nil || !old.TaskCompletion.Adhoc ||
				old.TaskCompletion.Details == nil || old.TaskCompletion.Details.RequestHash != adhocHash(hash, in) || !sameHouseholdGPS(old.GPS, detail.GPS) {
				return problem.IdempotencyConflict
			}
			out.Record, out.Replayed = *old, true
		} else {
			if cur.CurrentEventID != in.ExpectedEventID {
				return ErrTaskChanged.With("current_event_id", cur.CurrentEventID)
			}
			var snap domain.SeasonSnapshot
			if err := json.Unmarshal(cur.SOPSnapshot, &snap); err != nil {
				return err
			}
			if in.StageSeq > len(snap.Stages) {
				return problem.Validation.WithField("stage_seq", "Chọn giai đoạn của vụ")
			}
			// Vụ đang canh tác ghi mọi giai đoạn; vụ đã thu hoạch chỉ giai đoạn cuối (Q-18); dừng/hủy thì không ghi.
			switch {
			case cur.Status == port.SeasonOpen:
			case cur.Status == "completed" && in.StageSeq == len(snap.Stages):
			case cur.Status == "completed":
				return ErrTaskLocked
			default:
				return ErrTaskState
			}
			now := s.now()
			done, _ := domain.ParseDay(detail.DoneOn)
			backfilled := done.Before(businessDay(now).AddDate(0, 0, -3))
			if backfilled && detail.BackfillReason == "" {
				return problem.Validation.WithField("backfill_reason", "Bắt buộc nhập lý do khi ghi bù quá 3 ngày")
			}
			seq, err := store.NextAdhocStepSeq(ctx, tx, cur.SeasonRef, domain.AdhocStepSeqBase)
			if err != nil {
				return err
			}
			step := domain.Step{Seq: seq, StageSeq: in.StageSeq, TaskSeq: 1, Name: in.Name, ActivityCode: in.ActivityCode, Window: &domain.Window{Kind: domain.WindowNone}}
			meta := domain.TaskRecord{StepSeq: seq, StageSeq: in.StageSeq, State: detail.State, DoneOn: detail.DoneOn, SOPVersionID: cur.SOPVersionID,
				SeasonEventID: cur.CurrentEventID, Step: step, Adhoc: true,
				Details: &domain.TaskDetails{ActualMaterials: actual, Backfilled: backfilled, BackfillReason: detail.BackfillReason, ZeroConfirmed: detail.ZeroConfirmed, RequestHash: adhocHash(hash, in)}}
			location := "missing"
			if detail.GPS.HasPoint() {
				location = "unverified"
			}
			id := ids.New()
			l := store.FarmLog{ID: id, TenantID: a.p.TenantID, LogRef: id, SeasonRef: cur.SeasonRef, HouseholdRef: cur.HouseholdRef, CooperativeRef: cur.CooperativeRef,
				CommuneCode: cur.CommuneCode, ActivityCode: in.ActivityCode, SOPStepSeq: &seq, GPS: detail.GPS, LocationStatus: location, Inputs: inputs, Note: detail.Note,
				FileRefs: []uuid.UUID{}, BecameCurrent: true, OnBehalf: a.onBehalf(), Channel: detail.Channel, EventTime: now, ActorRef: a.p.ActorRef, IdemKey: key,
				EntryKind: "household", TaskCompletion: &meta}
			if err := store.InsertFarmLog(ctx, tx, &l); err != nil {
				return err
			}
			if err := store.SaveHouseholdTask(ctx, tx, l); err != nil {
				return err
			}
			if err := store.RefreshYield(ctx, tx, l.SeasonRef); err != nil {
				return err
			}
			out.Record = l
		}
		out.Checklist, err = s.householdChecklistTx(ctx, tx, *cur, true)
		return err
	})
	return out, err
}

// adhocHash gắn nội dung riêng của việc phát sinh vào băm của form chi tiết để phát lại khác nội dung bị từ chối.
func adhocHash(detailHash string, in AdhocTaskInput) string {
	raw, _ := json.Marshal([]any{detailHash, in.StageSeq, in.Name, in.ActivityCode, in.SeasonRef})
	return string(raw)
}
