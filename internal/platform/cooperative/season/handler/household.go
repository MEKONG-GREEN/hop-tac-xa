package handler

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/app"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/httpx"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
)

type householdSeasonRequest struct {
	CropCode     string  `json:"crop_code"`
	SeasonCode   string  `json:"season_code"`
	SeasonYear   int     `json:"season_year"`
	VarietyCode  string  `json:"variety_code"`
	AreaHa       string  `json:"area_ha"`
	AnchorDate   string  `json:"anchor_date"`
	SOPVersionID *string `json:"sop_version_id"`
	GPS          *gpsReq `json:"gps"`
	Channel      string  `json:"channel"`
	// PlanChanges: tùy chỉnh quy trình trước khi lưu vụ (AH-113).
	PlanChanges []domain.PlanChange `json:"plan_changes"`
	// CopyFromSeasonRef: AH-139 — sao chép giống và quy trình (kèm tùy chỉnh) từ vụ trước của hộ.
	CopyFromSeasonRef *string `json:"copy_from_season_ref"`
	// SelfMadePlan, SelfMadeAnchor: AH-162 — quy trình tự tạo khi loại cây chưa có SOP đang áp dụng.
	SelfMadePlan   *selfMadePlanReq `json:"self_made_plan"`
	SelfMadeAnchor string           `json:"self_made_anchor"`
}

type selfMadePlanReq struct {
	Stages []domain.Stage `json:"stages"`
	Steps  []domain.Step  `json:"steps"`
}

func (p *selfMadePlanReq) content() *domain.Content {
	if p == nil {
		return nil
	}
	return &domain.Content{Stages: p.Stages, Steps: p.Steps}
}

func householdPath(c *gin.Context) (uuid.UUID, uuid.UUID, bool) {
	coop, e1 := uuid.Parse(c.Param("ref"))
	hh, e2 := uuid.Parse(c.Param("household_ref"))
	if e1 != nil || e2 != nil || coop == uuid.Nil || hh == uuid.Nil {
		httpx.Fail(c, problem.Validation.WithField("household_ref", "mã HTX/nông hộ không hợp lệ"))
		return uuid.Nil, uuid.Nil, false
	}
	return coop, hh, true
}

func (h Handler) openHouseholdSeason(c *gin.Context) {
	coop, hh, ok := householdPath(c)
	if !ok {
		return
	}
	var req householdSeasonRequest
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		httpx.Fail(c, problem.Validation.WithField("body", "dữ liệu không hợp lệ"))
		return
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		httpx.Fail(c, problem.Validation.WithField("body", "chỉ nhận một đối tượng JSON"))
		return
	}
	anchor, err := domain.ParseDay(req.AnchorDate)
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("anchor_date", "ngày hợp lệ YYYY-MM-DD"))
		return
	}
	var version uuid.UUID
	if req.SOPVersionID != nil {
		version, err = uuid.Parse(*req.SOPVersionID)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("sop_version_id", "mã phiên bản không hợp lệ"))
			return
		}
	}
	var copyFrom uuid.UUID
	if req.CopyFromSeasonRef != nil {
		if copyFrom, err = uuid.Parse(*req.CopyFromSeasonRef); err != nil || copyFrom == uuid.Nil {
			httpx.Fail(c, problem.Validation.WithField("copy_from_season_ref", "mã vụ nguồn không hợp lệ"))
			return
		}
	}
	r, err := h.Svc.OpenHouseholdSeason(c.Request.Context(), app.HouseholdOpenInput{CooperativeRef: coop, HouseholdRef: hh,
		CropCode: req.CropCode, SeasonCode: req.SeasonCode, SeasonYear: req.SeasonYear, VarietyCode: req.VarietyCode,
		AreaHa: req.AreaHa, AnchorDate: anchor, SOPVersionID: version, GPS: req.GPS.gps(), Channel: req.Channel, IdemKey: idem(c), PlanChanges: req.PlanChanges, CopyFrom: copyFrom,
		SelfMade: req.SelfMadePlan.content(), SelfMadeAnchor: req.SelfMadeAnchor})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	if r.Replayed {
		c.Header("Idempotent-Replayed", "true")
	}
	c.Header("Location", "/v1/cooperatives/"+coop.String()+"/households/"+hh.String()+"/seasons/"+r.Season.SeasonRef.String())
	c.JSON(http.StatusCreated, gin.H{"season": householdSeasonOut(r)})
}

func (h Handler) listHouseholdSeasons(c *gin.Context) {
	coop, hh, ok := householdPath(c)
	if !ok {
		return
	}
	after, limit, ok := cursor(c)
	if !ok {
		return
	}
	page, err := h.Svc.ListHouseholdSeasons(c.Request.Context(), coop, hh, after, c.Query("status"), limit)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(page.Items))
	for _, r := range page.Items {
		items = append(items, householdSeasonOut(r))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": next(page.Next)})
}

func (h Handler) getHouseholdSeason(c *gin.Context) {
	coop, hh, ok := householdPath(c)
	if !ok {
		return
	}
	season, err := uuid.Parse(c.Param("season_ref"))
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("season_ref", "mã vụ không hợp lệ"))
		return
	}
	r, err := h.Svc.GetHouseholdSeason(c.Request.Context(), coop, hh, season)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, householdSeasonOut(r))
}

func householdSeasonOut(r app.SeasonResult) gin.H {
	s := r.Season
	var forecast, harvest *string
	if !s.ExpectedHarvestDate.IsZero() {
		v := domain.FormatDay(s.ExpectedHarvestDate)
		forecast = &v
	}
	if !s.HarvestOpenDate.IsZero() {
		v := domain.FormatDay(s.HarvestOpenDate)
		harvest = &v
	}
	return gin.H{"season_ref": s.SeasonRef, "entry_kind": "household", "household_ref": s.HouseholdRef,
		"cooperative_ref": s.CooperativeRef, "commune_code": s.CommuneCode, "crop_code": s.CropCode,
		"season_code": s.SeasonCode, "season_year": s.SeasonYear, "variety_code": s.VarietyCode, "area_ha": s.AreaHa.String(),
		"anchor_date": domain.FormatDay(s.SowingDate), "expected_harvest_date": forecast, "harvest_open_date": harvest,
		"sop_snapshot": s.SOPSnapshot, "status": s.Status, "current_event_id": s.CurrentEventID, "version": s.Version,
		"gps_missing": !r.Event.GPS.HasPoint(), "gps": gpsOut(r.Event.GPS), "opened_by": s.OpenedBy, "opened_at": ts(s.OpenedAt), "copied_from": uuidOrNil(s.CopiedFrom)}
}

func uuidOrNil(u uuid.UUID) *uuid.UUID {
	if u == uuid.Nil {
		return nil
	}
	return &u
}
