package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/app"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/httpx"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
)

// householdTaskBatchCandidates: AH-176 — các vụ của HTX cùng SOP gốc với vụ đang mở và việc tương ứng còn ghi được không.
func (h Handler) householdTaskBatchCandidates(c *gin.Context) {
	coop, hh, season, ok := taskPath(c)
	if !ok {
		return
	}
	step, err := strconv.Atoi(c.Query("step_seq"))
	if err != nil || step < 1 {
		httpx.Fail(c, problem.Validation.WithField("step_seq", "Chọn việc của vụ"))
		return
	}
	rows, err := h.Svc.HouseholdTaskBatchCandidates(c.Request.Context(), coop, hh, season, step)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(rows))
	for _, r := range rows {
		item := gin.H{"household_ref": r.Season.HouseholdRef, "season_ref": r.Season.SeasonRef, "season_code": r.Season.SeasonCode,
			"season_year": r.Season.SeasonYear, "variety_code": r.Season.VarietyCode, "status": r.Season.Status,
			"current_event_id": r.Season.CurrentEventID, "eligible": r.Eligible, "is_source": r.Season.SeasonRef == season}
		if r.StepSeq > 0 {
			item["step_seq"] = r.StepSeq
		}
		if r.Code != "" {
			item["code"], item["reason"] = r.Code, r.Reason
		}
		items = append(items, item)
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// recordHouseholdTaskBatch: AH-176 — ghi một việc chung cho nhiều vụ; kết quả từng vụ (recorded/skipped kèm lý do).
func (h Handler) recordHouseholdTaskBatch(c *gin.Context) {
	coop, hh, season, ok := taskPath(c)
	if !ok {
		return
	}
	var req struct {
		StepSeq int `json:"step_seq"`
		Seasons []struct {
			HouseholdRef    uuid.UUID `json:"household_ref"`
			SeasonRef       uuid.UUID `json:"season_ref"`
			ExpectedEventID uuid.UUID `json:"expected_season_event_id"`
		} `json:"seasons"`
		State          string                    `json:"state"`
		DoneOn         string                    `json:"done_on"`
		Note           string                    `json:"note"`
		BackfillReason string                    `json:"backfill_reason"`
		Materials      []app.ActualMaterialInput `json:"materials"`
		FileRefs       []uuid.UUID               `json:"file_refs"`
		ZeroConfirmed  bool                      `json:"zero_confirmed"`
		GPS            *gpsReq                   `json:"gps"`
		Channel        string                    `json:"channel"`
	}
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		httpx.Fail(c, problem.Validation.WithField("body", "Dữ liệu ghi nhiều vụ không hợp lệ"))
		return
	}
	targets := make([]app.TaskBatchTarget, 0, len(req.Seasons))
	for _, s := range req.Seasons {
		targets = append(targets, app.TaskBatchTarget{HouseholdRef: s.HouseholdRef, SeasonRef: s.SeasonRef, ExpectedEventID: s.ExpectedEventID})
	}
	results, err := h.Svc.RecordHouseholdTaskBatch(c.Request.Context(), app.TaskBatchInput{CooperativeRef: coop, HouseholdRef: hh, SeasonRef: season,
		StepSeq: req.StepSeq, Targets: targets, State: req.State, DoneOn: req.DoneOn, Note: req.Note, BackfillReason: req.BackfillReason,
		Materials: req.Materials, FileRefs: req.FileRefs, ZeroConfirmed: req.ZeroConfirmed, GPS: req.GPS.gps(), Channel: req.Channel, IdemKey: idem(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(results))
	recorded := 0
	for _, r := range results {
		item := gin.H{"household_ref": r.HouseholdRef, "season_ref": r.SeasonRef, "status": r.Status}
		if r.StepSeq > 0 {
			item["step_seq"] = r.StepSeq
		}
		if r.Record != nil {
			item["record"], item["replayed"] = taskRecordOut(*r.Record), r.Replayed
			recorded++
		} else {
			item["code"], item["reason"] = r.Code, r.Reason
		}
		items = append(items, item)
	}
	c.JSON(http.StatusOK, gin.H{"results": items, "recorded": recorded, "skipped": len(results) - recorded})
}
