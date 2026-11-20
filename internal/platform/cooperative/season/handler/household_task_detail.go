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

func (h Handler) recordHouseholdTask(c *gin.Context) {
	coop, hh, season, ok := taskPath(c)
	if !ok {
		return
	}
	var req struct {
		StepSeq         int                       `json:"step_seq"`
		ExpectedEventID string                    `json:"expected_season_event_id"`
		State           string                    `json:"state"`
		DoneOn          string                    `json:"done_on"`
		Note            string                    `json:"note"`
		BackfillReason  string                    `json:"backfill_reason"`
		Materials       []app.ActualMaterialInput `json:"materials"`
		FileRefs        []uuid.UUID               `json:"file_refs"`
		ZeroConfirmed   bool                      `json:"zero_confirmed"`
		GPS             *gpsReq                   `json:"gps"`
		Channel         string                    `json:"channel"`
	}
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		httpx.Fail(c, problem.Validation.WithField("body", "Dữ liệu ghi việc không hợp lệ"))
		return
	}
	event, err := uuid.Parse(req.ExpectedEventID)
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("expected_season_event_id", "Phiên bản vụ không hợp lệ"))
		return
	}
	result, err := h.Svc.RecordHouseholdTask(c.Request.Context(), app.DetailedTaskInput{CooperativeRef: coop, HouseholdRef: hh, SeasonRef: season, ExpectedEventID: event,
		StepSeq: req.StepSeq, State: req.State, DoneOn: req.DoneOn, Note: req.Note, BackfillReason: req.BackfillReason, Materials: req.Materials, FileRefs: req.FileRefs,
		ZeroConfirmed: req.ZeroConfirmed, GPS: req.GPS.gps(), Channel: req.Channel, IdemKey: idem(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	if result.Replayed {
		c.Header("Idempotent-Replayed", "true")
	}
	c.JSON(http.StatusCreated, gin.H{"record": taskRecordOut(result.Record), "checklist": checklistOut(result.Checklist)})
}
