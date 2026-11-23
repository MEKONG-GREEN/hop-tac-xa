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

// changeHouseholdPlan: AH-113 — một thay đổi quy trình của vụ đang canh tác (thêm, sửa, đổi chỗ, bỏ việc).
func (h Handler) changeHouseholdPlan(c *gin.Context) {
	coop, hh, season, ok := taskPath(c)
	if !ok {
		return
	}
	var req struct {
		ExpectedEventID string            `json:"expected_season_event_id"`
		Change          domain.PlanChange `json:"change"`
	}
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		httpx.Fail(c, problem.Validation.WithField("body", "Dữ liệu thay đổi quy trình không hợp lệ"))
		return
	}
	event, err := uuid.Parse(req.ExpectedEventID)
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("expected_season_event_id", "Phiên bản vụ không hợp lệ"))
		return
	}
	r, err := h.Svc.ChangeHouseholdPlan(c.Request.Context(), app.PlanChangeInput{CooperativeRef: coop, HouseholdRef: hh, SeasonRef: season,
		ExpectedEventID: event, Change: req.Change, IdemKey: idem(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	if r.Replayed {
		c.Header("Idempotent-Replayed", "true")
	}
	c.JSON(http.StatusOK, gin.H{"season": householdSeasonOut(app.SeasonResult{Season: r.Season, Event: r.Event}),
		"checklist": checklistOut(r.Checklist), "summary": r.Summary})
}

// householdPlanHistory: lịch sử tùy chỉnh quy trình của vụ (ai, lúc nào, nội dung), mới nhất trước.
func (h Handler) householdPlanHistory(c *gin.Context) {
	coop, hh, season, ok := taskPath(c)
	if !ok {
		return
	}
	items, err := h.Svc.HouseholdPlanHistory(c.Request.Context(), coop, hh, season)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}
