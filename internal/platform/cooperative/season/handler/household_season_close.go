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

// householdSeasonSummary: AH-152 — tổng kết "Ghi lại vụ" (tự tính khi vụ chưa đóng; bản chụp cố định khi đã hoàn thành).
func (h Handler) householdSeasonSummary(c *gin.Context) {
	coop, hh, season, ok := taskPath(c)
	if !ok {
		return
	}
	v, err := h.Svc.HouseholdSeasonSummary(c.Request.Context(), coop, hh, season)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, seasonSummaryOut(v))
}

// closeHouseholdSeason: AH-152 — Thành viên HTX hoàn thành vụ "Đã thu hoạch" (chi phí, ghi chú tùy chọn).
func (h Handler) closeHouseholdSeason(c *gin.Context) {
	coop, hh, season, ok := taskPath(c)
	if !ok {
		return
	}
	var req struct {
		ExpectedEventID string  `json:"expected_season_event_id"`
		CostVND         string  `json:"cost_vnd"`
		Note            string  `json:"note"`
		GPS             *gpsReq `json:"gps"`
		Channel         string  `json:"channel"`
	}
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		httpx.Fail(c, problem.Validation.WithField("body", "Dữ liệu hoàn thành vụ không hợp lệ"))
		return
	}
	event, err := uuid.Parse(req.ExpectedEventID)
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("expected_season_event_id", "Phiên bản vụ không hợp lệ"))
		return
	}
	r, v, err := h.Svc.CloseHouseholdSeason(c.Request.Context(), app.SeasonCloseInput{CooperativeRef: coop, HouseholdRef: hh, SeasonRef: season,
		ExpectedEventID: event, CostVND: req.CostVND, Note: req.Note, GPS: req.GPS.gps(), Channel: req.Channel, IdemKey: idem(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	status := http.StatusCreated
	if r.Replayed {
		c.Header("Idempotent-Replayed", "true")
		status = http.StatusOK
	}
	c.JSON(status, gin.H{"season": householdSeasonOut(r), "summary": seasonSummaryOut(v)})
}

func seasonSummaryOut(v app.SeasonSummaryView) gin.H {
	materials := make([]gin.H, 0, len(v.Data.Materials))
	for _, m := range v.Data.Materials {
		materials = append(materials, gin.H{"name": m.Name, "unit": m.Unit, "qty": m.Qty.String()})
	}
	t := v.Data.Tasks
	var cost *string
	if v.CostVND != nil {
		s := v.CostVND.String()
		cost = &s
	}
	var closedAt *string
	if v.ClosedAt != nil {
		s := ts(*v.ClosedAt)
		closedAt = &s
	}
	return gin.H{"status": v.Status, "area_ha": v.Data.AreaHa, "mass_t": v.Data.MassT, "mass_kg": v.Data.MassKg,
		"yield_t_per_ha": v.Data.YieldTPerHa, "harvest_date": v.Data.HarvestDate, "materials": materials,
		"tasks":            gin.H{"early": t.Early, "on_time": t.OnTime, "late": t.Late, "skipped": t.Skipped, "not_done": t.NotDone, "total": t.Total},
		"summary_step_seq": v.Data.SummaryStepSeq, "can_close": v.CanClose, "blockers": v.Blockers, "unresolved_steps": v.UnresolvedSteps,
		"harvest_reminder": v.HarvestReminder, "cost_vnd": cost, "note": v.Note, "closed_at": closedAt, "closed_by": v.ClosedBy}
}
