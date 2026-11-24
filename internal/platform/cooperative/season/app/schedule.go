package app

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	memberstore "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/member/store"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
)

type SchedulePreviewInput struct {
	CooperativeRef, HouseholdRef  uuid.UUID
	CropCode, VarietyCode, AreaHa string
	SOPVersionID                  uuid.UUID
	AnchorDate                    time.Time
	// PlanChanges: tùy chỉnh quy trình trước khi lưu vụ (AH-113 AC1), áp theo thứ tự.
	PlanChanges []domain.PlanChange
	// SelfMade: quy trình tự tạo đang soạn (AH-162) thay cho SOP.
	SelfMade *domain.Content
}

type ScheduleResult struct {
	domain.SchedulePlan
	SOPVersionID   uuid.UUID  `json:"sop_version_id"`
	CurrentEventID *uuid.UUID `json:"current_event_id"`
}

// PreviewHouseholdSchedule checks master selection but never writes a season or
// yield. External catalog/organization calls finish before opening the DB tx.
func (s *Service) PreviewHouseholdSchedule(ctx context.Context, in SchedulePreviewInput) (ScheduleResult, error) {
	if _, err := s.matrixAccess(ctx, fnPlan, htxroles.Create); err != nil {
		return ScheduleResult{}, err
	}
	a, err := s.householdAccess(ctx, in.CooperativeRef, true)
	if err != nil {
		return ScheduleResult{}, err
	}
	if in.SelfMade != nil {
		probe := HouseholdOpenInput{SelfMade: in.SelfMade, SOPVersionID: in.SOPVersionID}
		if err := normalizeSelfMade(&probe); err != nil {
			return ScheduleResult{}, err
		}
		in.SelfMade = probe.SelfMade
	}
	if in.VarietyCode == "" || (in.SOPVersionID == uuid.Nil && in.SelfMade == nil) {
		return ScheduleResult{}, problem.Validation.WithField("sop_version_id", "chọn giống và SOP trước khi lập kế hoạch")
	}
	if _, err := domain.ParseHouseholdSeasonArea(in.AreaHa); err != nil {
		return ScheduleResult{}, problem.Validation.WithField("area_ha", err.Error())
	}
	if s.dependencyTxOpen(ctx) || s.Orgs == nil || s.Catalogs == nil {
		return ScheduleResult{}, ErrDependency
	}
	if err := s.checkCoop(ctx, in.CooperativeRef); err != nil {
		return ScheduleResult{}, err
	}
	if err := s.checkCatalogScope(ctx, in.CropCode, in.VarietyCode); err != nil {
		return ScheduleResult{}, err
	}
	var result ScheduleResult
	err = s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		h, err := memberstore.HouseholdByRef(ctx, tx, in.HouseholdRef)
		if err != nil {
			return err
		}
		if h == nil || h.TenantID != a.p.TenantID || h.CooperativeRef != in.CooperativeRef {
			return ErrSeasonHousehold
		}
		if !h.Active {
			return ErrHouseholdInactive
		}
		base := domain.SeasonSnapshot{}
		versionID := uuid.Nil
		if in.SelfMade != nil {
			// AH-162: chỉ khi chưa có SOP đang áp dụng cho loại cây/giống.
			if _, _, err := householdSOP(ctx, tx, in.CooperativeRef, in.CropCode, in.VarietyCode, uuid.Nil); err == nil {
				return ErrSOPAvailable
			} else if !errors.Is(err, ErrHouseholdSOP) {
				return err
			}
			base.Stages, base.Steps = in.SelfMade.Stages, in.SelfMade.Steps
		} else {
			_, v, err := householdSOP(ctx, tx, in.CooperativeRef, in.CropCode, in.VarietyCode, in.SOPVersionID)
			if err != nil {
				return err
			}
			base.Stages, base.Steps, versionID = v.Stages, v.Steps, v.ID
		}
		snap, err := applyPlanChanges(base, in.PlanChanges)
		if err != nil {
			return err
		}
		plan, err := snap.Schedule(in.AnchorDate, s.today(), nil)
		if err != nil {
			return fieldProblem(err)
		}
		result = ScheduleResult{SchedulePlan: plan, SOPVersionID: versionID}
		return nil
	})
	return result, err
}

// HouseholdSchedule derives from the pinned content, even if the master SOP is
// renamed, retired or republished. Read permission includes representatives.
func (s *Service) HouseholdSchedule(ctx context.Context, coop, hh, season uuid.UUID) (ScheduleResult, error) {
	if _, err := s.matrixAccess(ctx, fnPlan, htxroles.Read); err != nil {
		return ScheduleResult{}, err
	}
	r, err := s.GetHouseholdSeason(ctx, coop, hh, season)
	if err != nil {
		return ScheduleResult{}, err
	}
	if r.Season.VarietyCode == "" {
		return ScheduleResult{}, problem.Validation.WithField("variety_code", "chưa chọn giống")
	}
	var snapshot domain.SeasonSnapshot
	if err := json.Unmarshal(r.Season.SOPSnapshot, &snapshot); err != nil {
		return ScheduleResult{}, problem.Internal.WithCause(err)
	}
	plan, err := snapshot.Schedule(r.Season.SowingDate, s.today(), nil)
	if err != nil {
		return ScheduleResult{}, fieldProblem(err)
	}
	return ScheduleResult{SchedulePlan: plan, SOPVersionID: r.Season.SOPVersionID, CurrentEventID: &r.Season.CurrentEventID}, nil
}
