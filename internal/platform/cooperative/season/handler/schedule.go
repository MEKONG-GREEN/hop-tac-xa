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

func (h Handler) previewHouseholdSchedule(c *gin.Context) {
	coop, hh, ok := householdPath(c)
	if !ok {
		return
	}
	var req struct {
		CropCode     string `json:"crop_code"`
		VarietyCode  string `json:"variety_code"`
		AreaHa       string `json:"area_ha"`
		SOPVersionID string `json:"sop_version_id"`
		AnchorDate   string `json:"anchor_date"`
	}
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		httpx.Fail(c, problem.Validation.WithField("body", "dữ liệu kế hoạch không hợp lệ"))
		return
	}
	anchor, err := domain.ParseDay(req.AnchorDate)
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("anchor_date", "ngày hợp lệ YYYY-MM-DD"))
		return
	}
	version, err := uuid.Parse(req.SOPVersionID)
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("sop_version_id", "chọn phiên bản SOP hợp lệ"))
		return
	}
	plan, err := h.Svc.PreviewHouseholdSchedule(c.Request.Context(), app.SchedulePreviewInput{
		CooperativeRef: coop, HouseholdRef: hh, CropCode: req.CropCode, VarietyCode: req.VarietyCode,
		AreaHa: req.AreaHa, SOPVersionID: version, AnchorDate: anchor,
	})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, plan)
}

func (h Handler) householdSchedule(c *gin.Context) {
	coop, hh, ok := householdPath(c)
	if !ok {
		return
	}
	ref, err := uuid.Parse(c.Param("season_ref"))
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("season_ref", "mã vụ không hợp lệ"))
		return
	}
	plan, err := h.Svc.HouseholdSchedule(c.Request.Context(), coop, hh, ref)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, plan)
}
