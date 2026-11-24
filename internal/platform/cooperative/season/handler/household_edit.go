package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/app"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/httpx"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
)

// editHouseholdSeason: AH-175 — sửa mốc gốc, diện tích, giống/SOP của vụ đang canh tác kèm lý do.
func (h Handler) editHouseholdSeason(c *gin.Context) {
	coop, hh, season, ok := taskPath(c)
	if !ok {
		return
	}
	var req struct {
		ExpectedEventID string     `json:"expected_season_event_id"`
		Reason          string     `json:"reason"`
		AnchorDate      *string    `json:"anchor_date"`
		AreaHa          *string    `json:"area_ha"`
		VarietyCode     *string    `json:"variety_code"`
		SOPVersionID    *uuid.UUID `json:"sop_version_id"`
		Channel         string     `json:"channel"`
	}
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		httpx.Fail(c, problem.Validation.WithField("body", "Dữ liệu sửa vụ không hợp lệ"))
		return
	}
	event, err := uuid.Parse(req.ExpectedEventID)
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("expected_season_event_id", "Phiên bản vụ không hợp lệ"))
		return
	}
	in := app.EditSeasonInput{CooperativeRef: coop, HouseholdRef: hh, SeasonRef: season, ExpectedEventID: event, Reason: req.Reason,
		AreaHa: req.AreaHa, VarietyCode: req.VarietyCode, SOPVersionID: req.SOPVersionID, Channel: req.Channel, IdemKey: idem(c)}
	if req.AnchorDate != nil {
		d, err := time.Parse(time.DateOnly, *req.AnchorDate)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("anchor_date", "ngày hợp lệ YYYY-MM-DD"))
			return
		}
		in.AnchorDate = &d
	}
	r, err := h.Svc.EditHouseholdSeason(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, householdSeasonOut(r))
}
