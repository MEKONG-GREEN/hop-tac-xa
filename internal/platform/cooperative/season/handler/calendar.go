package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/app"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/httpx"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
)

func calendarOut(v app.CalendarView) gin.H {
	e := v.Event
	status := "published"
	if e.Action == "cancelled" {
		status = "cancelled"
	}
	out := gin.H{"calendar_ref": e.CalendarRef, "cooperative_ref": e.CooperativeRef, "current_event_id": e.ID, "version": e.Seq, "status": status,
		"crop_code": e.Draft.CropCode, "season_code": e.Draft.SeasonCode, "season_year": e.Draft.SeasonYear, "sow_from": domain.FormatDay(e.Draft.SowFrom), "sow_to": domain.FormatDay(e.Draft.SowTo), "can_manage": v.CanManage, "updated_at": ts(e.RecordedAt)}
	if v.CanManage {
		out["recipients"] = e.Draft.Recipients
	}
	return out
}
func calendarRef(c *gin.Context, name string) (uuid.UUID, bool) {
	ref, err := uuid.Parse(c.Param(name))
	if err != nil || ref == uuid.Nil {
		httpx.Fail(c, problem.Validation.WithField(name, "mã không hợp lệ"))
		return uuid.Nil, false
	}
	return ref, true
}
func (h Handler) calendar(c *gin.Context) {
	ref, ok := calendarRef(c, "calendar_ref")
	if !ok {
		return
	}
	v, err := h.Svc.Calendar(c.Request.Context(), ref)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, calendarOut(v))
}
func (h Handler) calendars(c *gin.Context) {
	coop, ok := calendarRef(c, "ref")
	if !ok {
		return
	}
	var after uuid.UUID
	if c.Query("after") != "" {
		var err error
		after, err = uuid.Parse(c.Query("after"))
		if err != nil || after == uuid.Nil {
			httpx.Fail(c, problem.Validation.WithField("after", "con trỏ không hợp lệ"))
			return
		}
	}
	limit := 50
	if c.Query("limit") != "" {
		var err error
		limit, err = strconv.Atoi(c.Query("limit"))
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("limit", "số nguyên từ 1 đến 100"))
			return
		}
	}
	list, next, err := h.Svc.Calendars(c.Request.Context(), coop, after, limit)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := []gin.H{}
	for _, v := range list {
		items = append(items, calendarOut(v))
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": next})
}
func (h Handler) calendarRecipients(c *gin.Context) {
	coop, ok := calendarRef(c, "ref")
	if !ok {
		return
	}
	offset, err := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("offset", "số nguyên không âm"))
		return
	}
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "50"))
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("limit", "số nguyên từ 1 đến 100"))
		return
	}
	page, err := h.Svc.CalendarRecipients(c.Request.Context(), coop, offset, limit)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, page)
}
func (h Handler) writeCalendar(action string) gin.HandlerFunc {
	return func(c *gin.Context) {
		coop, ok := calendarRef(c, "ref")
		if !ok {
			return
		}
		in := app.CalendarWriteInput{CooperativeRef: coop, Action: action, IdemKey: idem(c)}
		if action != "published" {
			in.CalendarRef, ok = calendarRef(c, "calendar_ref")
			if !ok {
				return
			}
		}
		var req struct {
			ExpectedEventID *uuid.UUID   `json:"expected_event_id"`
			CropCode        *string      `json:"crop_code"`
			SeasonCode      *string      `json:"season_code"`
			SeasonYear      *int         `json:"season_year"`
			SowFrom         *string      `json:"sow_from"`
			SowTo           *string      `json:"sow_to"`
			Recipients      *[]uuid.UUID `json:"recipients"`
		}
		dec := json.NewDecoder(c.Request.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil || dec.Decode(&struct{}{}) != io.EOF {
			httpx.Fail(c, problem.Validation.WithField("body", "dữ liệu lịch không hợp lệ"))
			return
		}
		if req.ExpectedEventID != nil {
			if action == "published" {
				httpx.Fail(c, problem.Validation.WithField("expected_event_id", "lịch mới không có phiên bản trước"))
				return
			}
			in.ExpectedEventID = *req.ExpectedEventID
		}
		if action == "cancelled" {
			if req.CropCode != nil || req.SeasonCode != nil || req.SeasonYear != nil || req.SowFrom != nil || req.SowTo != nil || req.Recipients != nil {
				httpx.Fail(c, problem.Validation.WithField("body", "hủy chỉ cần phiên bản lịch"))
				return
			}
		} else {
			if req.CropCode == nil || req.SeasonCode == nil || req.SeasonYear == nil || req.SowFrom == nil || req.SowTo == nil || req.Recipients == nil {
				httpx.Fail(c, problem.Validation.WithField("body", "loại cây, mùa vụ/năm, khung ngày và người nhận bắt buộc"))
				return
			}
			from, err := time.Parse(time.DateOnly, *req.SowFrom)
			if err != nil {
				httpx.Fail(c, problem.Validation.WithField("sow_from", "ngày YYYY-MM-DD"))
				return
			}
			to, err := time.Parse(time.DateOnly, *req.SowTo)
			if err != nil {
				httpx.Fail(c, problem.Validation.WithField("sow_to", "ngày YYYY-MM-DD"))
				return
			}
			in.Draft = domain.CalendarDraft{CropCode: *req.CropCode, SeasonCode: *req.SeasonCode, SeasonYear: *req.SeasonYear, SowFrom: from, SowTo: to, Recipients: *req.Recipients}
		}
		result, err := h.Svc.WriteCalendar(c.Request.Context(), in)
		if err != nil {
			httpx.Fail(c, err)
			return
		}
		if result.Replayed {
			c.Header("Idempotent-Replayed", "true")
		}
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusCreated, calendarOut(result.CalendarView))
	}
}
