package handler

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/app"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/httpx"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
)

// stopHouseholdSeason: AH-70 — dừng (hủy) vụ nông hộ đang canh tác kèm lý do; diện tích được giải phóng.
func (h Handler) stopHouseholdSeason(c *gin.Context) {
	coop, hh, season, ok := taskPath(c)
	if !ok {
		return
	}
	var req struct {
		ExpectedEventID string `json:"expected_season_event_id"`
		Reason          string `json:"reason"`
		Channel         string `json:"channel"`
	}
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		httpx.Fail(c, problem.Validation.WithField("body", "Dữ liệu dừng vụ không hợp lệ"))
		return
	}
	event, err := uuid.Parse(req.ExpectedEventID)
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("expected_season_event_id", "Phiên bản vụ không hợp lệ"))
		return
	}
	r, err := h.Svc.StopHouseholdSeason(c.Request.Context(), app.StopSeasonInput{CooperativeRef: coop, HouseholdRef: hh, SeasonRef: season,
		ExpectedEventID: event, Reason: req.Reason, Channel: req.Channel, IdemKey: idem(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	if r.Replayed {
		c.Header("Idempotent-Replayed", "true")
	}
	c.JSON(http.StatusOK, householdSeasonOut(r))
}
