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

func taskRecordPath(c *gin.Context) (uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID, bool) {
	coop, hh, season, ok := taskPath(c)
	if !ok {
		return coop, hh, season, uuid.Nil, false
	}
	ref, err := uuid.Parse(c.Param("log_ref"))
	if err != nil || ref == uuid.Nil {
		httpx.Fail(c, problem.Validation.WithField("log_ref", "Mã bản ghi không hợp lệ"))
		return coop, hh, season, ref, false
	}
	return coop, hh, season, ref, true
}

// editHouseholdTask: AH-126 — sửa (void=false) hoặc xóa mềm/bỏ tích (void=true) một nhật ký việc, kèm lý do.
func (h Handler) editHouseholdTask(void bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		coop, hh, season, ref, ok := taskRecordPath(c)
		if !ok {
			return
		}
		var req struct {
			ExpectedID    string                    `json:"expected_record_id"`
			Reason        string                    `json:"reason"`
			State         string                    `json:"state"`
			DoneOn        string                    `json:"done_on"`
			Note          string                    `json:"note"`
			Materials     []app.ActualMaterialInput `json:"materials"`
			ZeroConfirmed bool                      `json:"zero_confirmed"`
			Channel       string                    `json:"channel"`
		}
		decoder := json.NewDecoder(c.Request.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&req); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
			httpx.Fail(c, problem.Validation.WithField("body", "Dữ liệu sửa nhật ký không hợp lệ"))
			return
		}
		if void && (req.State != "" || req.DoneOn != "" || req.Note != "" || req.Materials != nil || req.ZeroConfirmed) {
			httpx.Fail(c, problem.Validation.WithField("body", "Xóa bản ghi chỉ nhận phiên bản và lý do"))
			return
		}
		expected, err := uuid.Parse(req.ExpectedID)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("expected_record_id", "Phiên bản bản ghi không hợp lệ"))
			return
		}
		r, err := h.Svc.EditHouseholdTask(c.Request.Context(), app.TaskEditInput{CooperativeRef: coop, HouseholdRef: hh, SeasonRef: season, LogRef: ref,
			ExpectedID: expected, Void: void, Reason: req.Reason, State: req.State, DoneOn: req.DoneOn, Note: req.Note, Materials: req.Materials,
			ZeroConfirmed: req.ZeroConfirmed, Channel: req.Channel, IdemKey: idem(c)})
		if err != nil {
			httpx.Fail(c, err)
			return
		}
		if r.Replayed {
			c.Header("Idempotent-Replayed", "true")
		}
		c.JSON(http.StatusOK, gin.H{"record": taskRecordOut(r.Record), "checklist": checklistOut(r.Checklist)})
	}
}

// householdTaskHistory: các phiên bản của nhật ký việc (giá trị cũ – mới, người, thời điểm, lý do).
func (h Handler) householdTaskHistory(c *gin.Context) {
	coop, hh, season, ref, ok := taskRecordPath(c)
	if !ok {
		return
	}
	versions, err := h.Svc.HouseholdTaskHistory(c.Request.Context(), coop, hh, season, ref)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(versions))
	for _, v := range versions {
		items = append(items, taskRecordOut(v))
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}
