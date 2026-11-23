package handler

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/app"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/store"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/httpx"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
)

func taskRecordOut(l store.FarmLog) gin.H {
	out := gin.H{
		"id": l.ID, "log_ref": l.LogRef, "step_seq": l.TaskCompletion.StepSeq, "state": l.TaskCompletion.State, "done_on": l.TaskCompletion.DoneOn,
		"sop_version_id": l.TaskCompletion.SOPVersionID, "inputs": l.Inputs, "gps_missing": !l.GPS.HasPoint(), "gps": gpsOut(l.GPS),
		"actor_ref": l.ActorRef, "event_time": ts(l.EventTime), "recorded_at": ts(l.RecordedAt), "version": l.Version,
	}
	if l.CorrectionReason != nil {
		out["correction_reason"] = *l.CorrectionReason
	}
	if l.TaskCompletion.Adhoc {
		// AH-163: việc phát sinh ngoài quy trình — nhãn "Ngoài SOP", không tính tiến độ.
		out["adhoc"] = true
		out["adhoc_name"] = l.TaskCompletion.Step.Name
		out["stage_seq"] = l.TaskCompletion.StageSeq
		out["activity_code"] = l.TaskCompletion.Step.ActivityCode
	}
	if d := l.TaskCompletion.Details; d != nil {
		out["details"] = gin.H{"actual_materials": d.ActualMaterials, "backfilled": d.Backfilled, "backfill_reason": d.BackfillReason, "zero_confirmed": d.ZeroConfirmed}
		out["note"] = l.Note
		out["file_refs"] = l.FileRefs
	}
	return out
}
func checklistOut(c app.HouseholdChecklist) gin.H {
	records := []gin.H{}
	for _, r := range c.Records {
		records = append(records, taskRecordOut(r))
	}
	return gin.H{"season_ref": c.Season.SeasonRef, "current_event_id": c.Season.CurrentEventID, "progress": c.Progress, "records": records}
}
func taskPath(c *gin.Context) (uuid.UUID, uuid.UUID, uuid.UUID, bool) {
	coop, hh, ok := householdPath(c)
	if !ok {
		return coop, hh, uuid.Nil, false
	}
	season, err := uuid.Parse(c.Param("season_ref"))
	if err != nil || season == uuid.Nil {
		httpx.Fail(c, problem.Validation.WithField("season_ref", "mã vụ không hợp lệ"))
		return coop, hh, season, false
	}
	return coop, hh, season, true
}
func (h Handler) householdChecklist(c *gin.Context) {
	coop, hh, season, ok := taskPath(c)
	if !ok {
		return
	}
	r, err := h.Svc.HouseholdChecklist(c.Request.Context(), coop, hh, season)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, checklistOut(r))
}
func (h Handler) completeHouseholdTask(c *gin.Context) {
	coop, hh, season, ok := taskPath(c)
	if !ok {
		return
	}
	var req struct {
		StepSeq         int     `json:"step_seq"`
		ExpectedEventID string  `json:"expected_season_event_id"`
		GPS             *gpsReq `json:"gps"`
		Channel         string  `json:"channel"`
	}
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		httpx.Fail(c, problem.Validation.WithField("body", "dữ liệu xác nhận không hợp lệ"))
		return
	}
	event, err := uuid.Parse(req.ExpectedEventID)
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("expected_season_event_id", "phiên bản vụ không hợp lệ"))
		return
	}
	r, err := h.Svc.CompleteHouseholdTask(c.Request.Context(), app.CompleteTaskInput{CooperativeRef: coop, HouseholdRef: hh, SeasonRef: season, ExpectedEventID: event, StepSeq: req.StepSeq, GPS: req.GPS.gps(), Channel: req.Channel, IdemKey: idem(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	if r.Replayed {
		c.Header("Idempotent-Replayed", "true")
	}
	c.JSON(http.StatusCreated, gin.H{"record": taskRecordOut(r.Record), "checklist": checklistOut(r.Checklist)})
}
