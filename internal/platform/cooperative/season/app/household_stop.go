package app

// AH-70 (FN-03.10, FN-03-EX-07, FN-03-BR-05): Thành viên HTX dừng (hủy) vụ nông hộ đang canh tác kèm lý do. Vụ chuyển
// "Đã dừng vụ" (cancelled) bằng một sự kiện bù trên sổ vụ; diện tích được giải phóng qua bản chiếu
// household_season_capacity (trigger trên season_current); nhật ký và kế hoạch giữ nguyên, chỉ đọc. Vụ đã dừng không
// quay lại "Đang canh tác" và không ghi nhật ký được (các luồng ghi kiểm trạng thái open).

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/store"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
)

// StopSeasonInput là yêu cầu dừng một vụ nông hộ.
type StopSeasonInput struct {
	CooperativeRef, HouseholdRef, SeasonRef, ExpectedEventID uuid.UUID
	Reason, Channel, IdemKey                                 string
}

// StopHouseholdSeason dừng vụ đang canh tác (KHMV-FN-03 D). Lý do bắt buộc (≥ 10 ký tự, không SĐT/CCCD); chỉ vụ
// "Đang canh tác"; phiên bản vụ phải khớp. Phát lại cùng Idempotency-Key trả kết quả đã ghi.
func (s *Service) StopHouseholdSeason(ctx context.Context, in StopSeasonInput) (SeasonResult, error) {
	if _, err := s.matrixAccess(ctx, fnSeason, htxroles.Delete); err != nil {
		return SeasonResult{}, err
	}
	a, err := s.householdAccess(ctx, in.CooperativeRef, true)
	if err != nil {
		return SeasonResult{}, err
	}
	if in.IdemKey == "" {
		return SeasonResult{}, problem.IdempotencyRequired
	}
	in.Reason = strings.Join(strings.Fields(in.Reason), " ")
	switch {
	case !domain.ValidReason(in.Reason) || len([]rune(in.Reason)) > 500:
		return SeasonResult{}, problem.Validation.WithField("reason", "Nhập lý do dừng vụ (10–500 ký tự)")
	case domain.ContainsPII(in.Reason):
		return SeasonResult{}, problem.Validation.WithField("reason", "Lý do không được chứa SĐT hoặc CCCD")
	case in.ExpectedEventID == uuid.Nil:
		return SeasonResult{}, problem.Validation.WithField("expected_season_event_id", "Phiên bản vụ không hợp lệ")
	}
	if in.Channel == "" {
		in.Channel = ChannelWeb
	}
	if in.Channel != ChannelApp && in.Channel != ChannelWeb {
		return SeasonResult{}, problem.Validation.WithField("channel", "app hoặc web")
	}
	key := "household-stop:" + a.p.ActorRef.String() + ":" + in.IdemKey
	var out SeasonResult
	err = s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		cur, err := householdTaskSeason(ctx, tx, a.p.TenantID, in.CooperativeRef, in.HouseholdRef, in.SeasonRef, true)
		if err != nil {
			return err
		}
		if old, err := store.SeasonEventByIdem(ctx, tx, key); err != nil {
			return err
		} else if old != nil {
			if old.SeasonRef != in.SeasonRef || old.ActorRef != a.p.ActorRef || old.Action != "cancelled" || old.Reason != in.Reason ||
				old.SupersedesID == nil || *old.SupersedesID != in.ExpectedEventID {
				return problem.IdempotencyConflict
			}
			out = SeasonResult{Season: *cur, Event: *old, Replayed: true}
			return nil
		}
		if cur.Status != port.SeasonOpen {
			return ErrSeasonState.WithDetail("Chỉ dừng được vụ đang canh tác; vụ đã thu hoạch thì dùng Sửa sản lượng")
		}
		if cur.CurrentEventID != in.ExpectedEventID {
			return ErrTaskChanged.With("current_event_id", cur.CurrentEventID)
		}
		prev, err := store.SeasonEventByID(ctx, tx, cur.CurrentEventID)
		if err != nil {
			return err
		}
		now := s.now()
		ev := *prev
		ev.ID, ev.Seq, ev.EventTime, ev.ActorRef, ev.DeviceID, ev.IdemKey = ids.New(), 0, now, a.p.ActorRef, nil, key
		ev.OnBehalf, ev.Channel, ev.HarvestID = a.onBehalf(), in.Channel, uuid.Nil
		sup, reason := cur.CurrentEventID, in.Reason
		ev.SupersedesID, ev.CorrectionReason = &sup, &reason
		ev.Action, ev.Reason = "cancelled", reason
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
			SupersedesID: sup, CorrectionReason: reason, ActorRef: ev.ActorRef, EventTime: now.Format(time.RFC3339),
		}); err != nil {
			return err
		}
		fresh, err := store.SeasonByRef(ctx, tx, in.SeasonRef, false)
		if err != nil {
			return err
		}
		out = SeasonResult{Season: *fresh, Event: ev}
		return nil
	})
	return out, err
}
