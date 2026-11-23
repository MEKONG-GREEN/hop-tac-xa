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

// recordAdhocTask: AH-163 — ghi việc phát sinh ngoài quy trình của vụ (nhãn "Ngoài SOP", không tính tiến độ).
func (h Handler) recordAdhocTask(c *gin.Context) {
	coop, hh, season, ok := taskPath(c)
	if !ok {
		return
	}
	var req struct {
		ExpectedEventID string                    `json:"expected_season_event_id"`
		StageSeq        int                       `json:"stage_seq"`
		Name            string                    `json:"name"`
		ActivityCode    string                    `json:"activity_code"`
		State           string                    `json:"state"`
		DoneOn          string                    `json:"done_on"`
		Note            string                    `json:"note"`
		BackfillReason  string                    `json:"backfill_reason"`
		Materials       []app.ActualMaterialInput `json:"materials"`
		ZeroConfirmed   bool                      `json:"zero_confirmed"`
		GPS             *gpsReq                   `json:"gps"`
		Channel         string                    `json:"channel"`
	}
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		httpx.Fail(c, problem.Validation.WithField("body", "Dữ liệu ghi việc phát sinh không hợp lệ"))
		return
	}
	event, err := uuid.Parse(req.ExpectedEventID)
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("expected_season_event_id", "Phiên bản vụ không hợp lệ"))
		return
	}
	r, err := h.Svc.RecordAdhocTask(c.Request.Context(), app.AdhocTaskInput{CooperativeRef: coop, HouseholdRef: hh, SeasonRef: season, ExpectedEventID: event,
		StageSeq: req.StageSeq, Name: req.Name, ActivityCode: req.ActivityCode, State: req.State, DoneOn: req.DoneOn, Note: req.Note,
		BackfillReason: req.BackfillReason, Materials: req.Materials, ZeroConfirmed: req.ZeroConfirmed, GPS: req.GPS.gps(), Channel: req.Channel, IdemKey: idem(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	if r.Replayed {
		c.Header("Idempotent-Replayed", "true")
	}
	c.JSON(http.StatusCreated, gin.H{"record": taskRecordOut(r.Record), "checklist": checklistOut(r.Checklist)})
}
