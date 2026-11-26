package app

// AH-175 (FN-03.9, FN-03-EX-08, FN-03-BR-10..13, FN-04-BS-01): Thành viên HTX sửa mốc gốc, giống hoặc diện tích của vụ
// nông hộ đang canh tác kèm lý do. Mỗi lần sửa là một sự kiện "corrected" trên sổ vụ (có lịch sử).
//
//   - Mốc gốc: không ràng buộc khoảng ngày (Q-13); ngày dự kiến của việc chưa xong tính lại theo mốc mới (lịch tính từ
//     bản chụp mỗi lần đọc), việc đã xong giữ ngày làm và khóa.
//   - Diện tích: > 0, hai chữ số thập phân; không vượt diện tích đất trừ các vụ đang canh tác khác (không tính chính vụ
//     đang sửa) — vượt thì từ chối kèm diện tích tối đa.
//   - Giống/SOP: chỉ khi vụ chưa có nhật ký (KH-10); đổi giống bắt buộc chọn SOP đang áp dụng theo giống mới, thay bản
//     chụp; giữ sản lượng dự kiến. Vụ quy trình tự tạo đổi giống thì giữ quy trình. Không gửi thông báo riêng (Q-50).

import (
	"context"
	"encoding/json"
	"net/http"
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

// ErrSeasonHasLogs: vụ đã có nhật ký không đổi giống/SOP (KH-10).
var ErrSeasonHasLogs = problem.New(http.StatusConflict, "cropcycle.season_has_logs", "Vụ đã có nhật ký: không đổi giống, SOP; muốn đổi thì dừng vụ và mở vụ mới")

// EditSeasonInput là một lần sửa vụ nông hộ; trường nil là giữ nguyên.
type EditSeasonInput struct {
	CooperativeRef, HouseholdRef, SeasonRef, ExpectedEventID uuid.UUID
	Reason                                                   string
	AnchorDate                                               *time.Time
	AreaHa                                                   *string
	VarietyCode                                              *string
	SOPVersionID                                             *uuid.UUID
	Channel, IdemKey                                         string
}

// EditHouseholdSeason sửa vụ đang canh tác (KHMV-FN-03 U). Danh mục giống kiểm trước giao dịch; trạng thái, phiên bản,
// diện tích, nhật ký kiểm dưới khóa hộ và khóa vụ.
func (s *Service) EditHouseholdSeason(ctx context.Context, in EditSeasonInput) (SeasonResult, error) {
	if _, err := s.matrixAccess(ctx, fnSeason, htxroles.Update); err != nil {
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
		return SeasonResult{}, problem.Validation.WithField("reason", "Nhập lý do sửa vụ (10–500 ký tự)")
	case domain.ContainsPII(in.Reason):
		return SeasonResult{}, problem.Validation.WithField("reason", "Lý do không được chứa SĐT hoặc CCCD")
	case in.ExpectedEventID == uuid.Nil:
		return SeasonResult{}, problem.Validation.WithField("expected_season_event_id", "Phiên bản vụ không hợp lệ")
	case in.AnchorDate == nil && in.AreaHa == nil && in.VarietyCode == nil && in.SOPVersionID == nil:
		return SeasonResult{}, problem.Validation.WithField("body", "Chọn ít nhất một nội dung cần sửa")
	}
	if in.AnchorDate != nil && (in.AnchorDate.Year() < 2000 || in.AnchorDate.Year() > 2100) {
		return SeasonResult{}, problem.Validation.WithField("anchor_date", "ngày hợp lệ YYYY-MM-DD")
	}
	var area decimal.Decimal
	if in.AreaHa != nil {
		if area, err = domain.ParseHouseholdSeasonArea(*in.AreaHa); err != nil {
			return SeasonResult{}, problem.Validation.WithField("area_ha", err.Error())
		}
	}
	if in.Channel == "" {
		in.Channel = ChannelWeb
	}
	if in.Channel != ChannelApp && in.Channel != ChannelWeb {
		return SeasonResult{}, problem.Validation.WithField("channel", "app hoặc web")
	}
	// Đọc vụ trước để kiểm danh mục giống ngoài giao dịch (port pf/masterdata).
	var pre *store.Season
	if err := s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		var e error
		pre, e = householdTaskSeason(ctx, tx, a.p.TenantID, in.CooperativeRef, in.HouseholdRef, in.SeasonRef, false)
		return e
	}); err != nil {
		return SeasonResult{}, err
	}
	if in.VarietyCode != nil {
		v := strings.TrimSpace(*in.VarietyCode)
		in.VarietyCode = &v
		if v == "" {
			return SeasonResult{}, problem.Validation.WithField("variety_code", "Chọn giống")
		}
		if s.dependencyTxOpen(ctx) || s.Catalogs == nil {
			return SeasonResult{}, ErrDependency
		}
		if err := s.checkCatalogScope(ctx, pre.CropCode, v); err != nil {
			return SeasonResult{}, err
		}
	}
	key := "household-edit:" + a.p.ActorRef.String() + ":" + in.IdemKey
	now := s.now()
	r, err := s.writeSeason(ctx, a.p.TenantID, key, func(tx pgx.Tx) (SeasonResult, error) {
		if err := memberstore.LockHousehold(ctx, tx, in.HouseholdRef); err != nil {
			return SeasonResult{}, err
		}
		cur, err := householdTaskSeason(ctx, tx, a.p.TenantID, in.CooperativeRef, in.HouseholdRef, in.SeasonRef, true)
		if err != nil {
			return SeasonResult{}, err
		}
		if cur.Status != port.SeasonOpen {
			return SeasonResult{}, ErrSeasonState.WithDetail("Chỉ sửa được vụ đang canh tác")
		}
		if cur.CurrentEventID != in.ExpectedEventID {
			return SeasonResult{}, ErrConcurrentEdit.With("current_event_id", cur.CurrentEventID.String())
		}
		prev, err := store.SeasonEventByID(ctx, tx, cur.CurrentEventID)
		if err != nil {
			return SeasonResult{}, err
		}
		ev := *prev
		ev.ID, ev.Seq, ev.EventTime, ev.ActorRef, ev.DeviceID, ev.IdemKey = ids.New(), 0, now, a.p.ActorRef, nil, key
		ev.OnBehalf, ev.Channel, ev.HarvestID, ev.Reason = a.onBehalf(), in.Channel, uuid.Nil, ""
		sup, reason := cur.CurrentEventID, in.Reason
		ev.SupersedesID, ev.CorrectionReason, ev.Action = &sup, &reason, "corrected"

		if in.AreaHa != nil && !area.Equal(cur.AreaHa) {
			h, err := memberstore.HouseholdByRef(ctx, tx, in.HouseholdRef)
			if err != nil {
				return SeasonResult{}, err
			}
			if h == nil || h.CultivatedAreaHa == nil {
				return SeasonResult{}, ErrHouseholdLand
			}
			used, err := memberstore.ActiveHouseholdArea(ctx, tx, h.Ref)
			if err != nil {
				return SeasonResult{}, err
			}
			// Diện tích tối đa không tính chính vụ đang sửa (BR-11).
			others := decimal.Max(decimal.Zero, used.Sub(cur.AreaHa))
			maxArea := decimal.Max(decimal.Zero, h.CultivatedAreaHa.Sub(others))
			if area.GreaterThan(maxArea) {
				return SeasonResult{}, ErrHouseholdArea.With("remaining_area_ha", maxArea.String()).With("active_area_ha", others.String()).With("land_area_ha", h.CultivatedAreaHa.String())
			}
			ev.AreaHa = area
		}

		changeVariety := in.VarietyCode != nil && *in.VarietyCode != cur.VarietyCode
		changeSOP := in.SOPVersionID != nil && *in.SOPVersionID != cur.SOPVersionID
		selfMade := selfMadeSnapshot(cur.SOPSnapshot)
		if changeVariety || changeSOP {
			logs, err := store.HouseholdTasks(ctx, tx, cur.SeasonRef)
			if err != nil {
				return SeasonResult{}, err
			}
			if len(logs) > 0 {
				return SeasonResult{}, ErrSeasonHasLogs
			}
			if changeVariety {
				ev.VarietyCode = *in.VarietyCode
			}
			if !selfMade || changeSOP {
				selected := uuid.Nil
				if in.SOPVersionID != nil {
					selected = *in.SOPVersionID
				}
				if changeVariety && selected == uuid.Nil {
					return SeasonResult{}, problem.Validation.WithField("sop_version_id", "Đổi giống phải chọn SOP đang áp dụng theo giống mới")
				}
				t, v, err := householdSOP(ctx, tx, in.CooperativeRef, cur.CropCode, ev.VarietyCode, selected)
				if err != nil {
					return SeasonResult{}, err
				}
				stages, steps := v.Stages, v.Steps
				if stages == nil {
					stages = []domain.Stage{}
				}
				if steps == nil {
					steps = []domain.Step{}
				}
				raw, err := json.Marshal(map[string]any{"template_ref": t.Ref, "version_id": v.ID, "version_no": v.VersionNo,
					"crop_code": cur.CropCode, "name": t.Name, "anchor_name": t.AnchorName, "stages": stages, "steps": steps})
				if err != nil {
					return SeasonResult{}, err
				}
				ev.SOPSnapshot, ev.SOPTemplateRef, ev.SOPVersionID, ev.SOPVersionNo = raw, t.Ref, v.ID, v.VersionNo
			}
		}
		if in.AnchorDate != nil {
			ev.SowingDate = *in.AnchorDate
		}
		if in.AnchorDate != nil || changeVariety || changeSOP {
			if err := householdHarvestDates(ctx, tx, &ev); err != nil {
				return SeasonResult{}, err
			}
		}
		if ev.AreaHa.Equal(prev.AreaHa) && ev.VarietyCode == prev.VarietyCode && ev.SOPVersionID == prev.SOPVersionID &&
			domain.FormatDay(ev.SowingDate) == domain.FormatDay(prev.SowingDate) {
			return SeasonResult{}, problem.Validation.WithField("body", "Không có nội dung thay đổi")
		}
		if err := store.InsertSeasonEvent(ctx, tx, &ev); err != nil {
			return SeasonResult{}, err
		}
		season := store.ApplySeasonEvent(cur, ev)
		if err := store.SaveSeason(ctx, tx, a.p.TenantID, season); err != nil {
			return SeasonResult{}, err
		}
		// AH-87: đổi diện tích hoặc giống thì tính lại sản lượng dự kiến hệ thống (tham số đã chụp, giữ giá trị nhập).
		if err := recomputeEstimateOnEditTx(ctx, tx, a.p.TenantID, *cur, season, a.p.ActorRef, now, key); err != nil {
			return SeasonResult{}, err
		}
		if err := afterSeasonChange(ctx, tx, a.p.TenantID, season, now); err != nil {
			return SeasonResult{}, err
		}
		if err := enqueue(ctx, tx, a.p.TenantID, port.EventSeasonCorrected, now, port.SeasonCorrectedV1{
			SeasonRef: ev.SeasonRef, EventID: ev.ID, Action: ev.Action, Status: season.Status, PlotRef: ev.PlotRef,
			HouseholdRef: uuidPtr(ev.HouseholdRef), CooperativeRef: ev.CooperativeRef, CommuneCode: ev.CommuneCode,
			SeasonCode: ev.SeasonCode, SeasonYear: ev.SeasonYear, VarietyCode: ev.VarietyCode, SOPVersionID: ev.SOPVersionID,
			SowingDate: domain.FormatDay(ev.SowingDate), ExpectedHarvestDate: domain.FormatDay(ev.ExpectedHarvestDate),
			SupersedesID: sup, CorrectionReason: reason, ActorRef: ev.ActorRef, EventTime: now.Format(time.RFC3339),
		}); err != nil {
			return SeasonResult{}, err
		}
		return s.reloadSeason(ctx, tx, ev)
	})
	if err != nil {
		return SeasonResult{}, err
	}
	// Phát lại sau khi biên nhận HTTP hết hạn vẫn phải cùng người, cùng vụ.
	if r.Event.ActorRef != a.p.ActorRef || r.Event.SeasonRef != in.SeasonRef || r.Event.CorrectionReason == nil || *r.Event.CorrectionReason != in.Reason {
		return SeasonResult{}, problem.IdempotencyConflict
	}
	items := []SeasonResult{r}
	if err := s.attachEstimates(ctx, a.p.TenantID, items); err != nil {
		return SeasonResult{}, err
	}
	return items[0], nil
}

// householdHarvestDates tính lại ngày thu hoạch dự kiến theo mốc gốc và phiên bản SOP (hoặc việc Thu hoạch cuối của
// quy trình tự tạo); không có việc Thu hoạch thì không có ngày dự kiến.
func householdHarvestDates(ctx context.Context, tx pgx.Tx, ev *store.SeasonEvent) error {
	ev.ExpectedHarvestDate, ev.HarvestOpenDate = time.Time{}, time.Time{}
	var offset, window *int
	v, err := store.VersionByID(ctx, tx, ev.SOPVersionID, false)
	if err != nil {
		return err
	}
	if v != nil {
		offset, window = v.HarvestDayOffset, v.HarvestWindowDays
	} else {
		var snap domain.SeasonSnapshot
		if err := json.Unmarshal(ev.SOPSnapshot, &snap); err != nil {
			return err
		}
		if h, ok := domain.HarvestStep(snap.Steps); ok {
			o, w := h.DayOffset, h.WindowDays
			offset, window = &o, &w
		}
	}
	if offset == nil || window == nil {
		return nil
	}
	ev.ExpectedHarvestDate = ev.SowingDate.AddDate(0, 0, *offset)
	ev.HarvestOpenDate = ev.ExpectedHarvestDate.AddDate(0, 0, -*window)
	return nil
}
