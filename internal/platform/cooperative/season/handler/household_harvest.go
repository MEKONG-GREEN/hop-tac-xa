package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/app"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/store"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/httpx"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
)

// declareHouseholdHarvest: AH-100 — Thành viên HTX khai báo tổng sản lượng (tấn) và ngày thu hoạch cuối của vụ theo hộ
// đang canh tác; vụ chuyển "Đã thu hoạch", việc Thu hoạch tự Xong.
func (h Handler) declareHouseholdHarvest(c *gin.Context) {
	coop, hh, season, ok := taskPath(c)
	if !ok {
		return
	}
	var req struct {
		ExpectedEventID string  `json:"expected_season_event_id"`
		MassT           string  `json:"mass_t"`
		HarvestDate     string  `json:"harvest_date"`
		ZeroConfirmed   bool    `json:"zero_confirmed"`
		GPS             *gpsReq `json:"gps"`
		Channel         string  `json:"channel"`
	}
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		httpx.Fail(c, problem.Validation.WithField("body", "Dữ liệu khai báo sản lượng không hợp lệ"))
		return
	}
	event, err := uuid.Parse(req.ExpectedEventID)
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("expected_season_event_id", "Phiên bản vụ không hợp lệ"))
		return
	}
	var day time.Time
	if req.HarvestDate != "" {
		if day, err = domain.ParseDay(req.HarvestDate); err != nil {
			httpx.Fail(c, problem.Validation.WithField("harvest_date", "Ngày thu hoạch dạng YYYY-MM-DD"))
			return
		}
	}
	r, err := h.Svc.DeclareHouseholdHarvest(c.Request.Context(), app.HouseholdHarvestInput{CooperativeRef: coop, HouseholdRef: hh, SeasonRef: season,
		ExpectedEventID: event, MassT: req.MassT, HarvestDate: day, ZeroConfirmed: req.ZeroConfirmed, GPS: req.GPS.gps(), Channel: req.Channel, IdemKey: idem(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	status := http.StatusCreated
	if r.Replayed {
		c.Header("Idempotent-Replayed", "true")
		status = http.StatusOK
	}
	auto := r.AutoCompleted
	if auto == nil {
		auto = []int{}
	}
	c.JSON(status, gin.H{"season": householdSeasonOut(r.Season), "harvest": householdHarvestOut(&r.Harvest), "auto_completed_steps": auto})
}

// householdHarvestOut: sản lượng theo tấn (2 chữ số lẻ, KH-26) kèm kg gốc.
func householdHarvestOut(h *store.Harvest) gin.H {
	if h == nil {
		return nil
	}
	return gin.H{"harvest_id": h.ID, "mass_t": h.MassKg.Decimal().Div(decimal.NewFromInt(1000)).StringFixed(2), "mass_kg": h.MassKg.String(),
		"harvest_date": domain.FormatDay(h.HarvestDate), "yield_t_per_ha": h.YieldTPerHa.StringFixed(2), "zero_confirmed": h.AnomalyKind == domain.AnomalyZero,
		"declared_by": h.ActorRef, "declared_at": ts(h.EventTime)}
}
