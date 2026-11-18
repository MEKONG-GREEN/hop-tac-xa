package app

import (
	"context"
	"encoding/json"
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
	if in.VarietyCode == "" || in.SOPVersionID == uuid.Nil {
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
		_, v, err := householdSOP(ctx, tx, in.CooperativeRef, in.CropCode, in.VarietyCode, in.SOPVersionID)
		if err != nil {
			return err
		}
		plan, err := domain.CalculateSchedule(domain.Content{Stages: v.Stages, Steps: v.Steps}, in.AnchorDate, s.today(), nil)
		if err != nil {
			return fieldProblem(err)
		}
		result = ScheduleResult{SchedulePlan: plan, SOPVersionID: v.ID}
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
	var snapshot struct {
		Stages []domain.Stage `json:"stages"`
		Steps  []domain.Step  `json:"steps"`
	}
	if err := json.Unmarshal(r.Season.SOPSnapshot, &snapshot); err != nil {
		return ScheduleResult{}, problem.Internal.WithCause(err)
	}
	plan, err := domain.CalculateSchedule(domain.Content{Stages: snapshot.Stages, Steps: snapshot.Steps}, r.Season.SowingDate, s.today(), nil)
	if err != nil {
		return ScheduleResult{}, fieldProblem(err)
	}
	return ScheduleResult{SchedulePlan: plan, SOPVersionID: r.Season.SOPVersionID, CurrentEventID: &r.Season.CurrentEventID}, nil
}
