// Package handler: HTTP của pf/weather (contracts/openapi/modules/weather/{application,admin}.yaml).
package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/weather/app"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/weather/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/httpx"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/lunar"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
)

// Handler gói Service cho HTTP.
type Handler struct{ Svc *app.Service }

// RegisterApp gắn route Application API: thẻ hôm nay và lịch tháng cho ba vai App HTX (FN-TT-01, FN-TT-02).
func (h Handler) RegisterApp(rg *gin.RouterGroup) {
	rg.GET("/weather/today", h.today)
	rg.GET("/weather/calendar", h.calendar)
}

// RegisterAdmin gắn route Admin API (MFA bắt buộc): cấu hình kết nối (FN-TT-03).
func (h Handler) RegisterAdmin(rg *gin.RouterGroup) {
	rg.GET("/weather/config", h.config)
	rg.POST("/weather/config/revisions", h.saveConfig)
	rg.POST("/weather/config/test", h.testConfig)
}

func coopParam(c *gin.Context) (uuid.UUID, bool) {
	raw := c.Query("cooperative_ref")
	if raw == "" {
		return uuid.Nil, true
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("cooperative_ref", "Mã HTX không hợp lệ"))
		return uuid.Nil, false
	}
	return id, true
}

func (h Handler) today(c *gin.Context) {
	coop, ok := coopParam(c)
	if !ok {
		return
	}
	t, err := h.Svc.Today(c.Request.Context(), coop)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	out := todayDTO{CooperativeRef: t.CooperativeRef, Date: t.Day, Lunar: toLunar(t.Lunar), Status: t.Status, Message: message(t.Status),
		Source: toSource(t.Source)}
	if t.Current != nil {
		out.Current = toReading(*t.Current)
	}
	if t.Summary != nil {
		out.Today = toSummary(*t.Summary)
	}
	c.JSON(http.StatusOK, out)
}

func (h Handler) calendar(c *gin.Context) {
	coop, ok := coopParam(c)
	if !ok {
		return
	}
	month := c.Query("month")
	if month == "" {
		month = time.Now().In(domain.VN).Format("2006-01")
	}
	cal, err := h.Svc.Calendar(c.Request.Context(), coop, month)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	out := calendarDTO{CooperativeRef: cal.CooperativeRef, Month: cal.Month, Status: cal.Status, Message: message(cal.Status),
		ForecastDays: cal.ForecastDays, Source: toSource(cal.Source), Days: make([]calendarDayDTO, 0, len(cal.Days))}
	for _, d := range cal.Days {
		day := calendarDayDTO{Date: d.Day, Lunar: toLunar(d.Lunar)}
		if d.Kind != "" && d.Summary != nil {
			w := weatherDayDTO{Kind: d.Kind, Label: d.Label, HourlyAvailable: d.Hourly, summaryDTO: *toSummary(*d.Summary), Periods: []periodDTO{}}
			for _, p := range d.Periods {
				w.Periods = append(w.Periods, toPeriod(p))
			}
			day.Weather = &w
		}
		out.Days = append(out.Days, day)
	}
	c.JSON(http.StatusOK, out)
}

func message(status string) string {
	switch status {
	case app.StatusNoLocation:
		return app.MsgNoLocation
	case app.StatusNoData:
		return app.MsgNoData
	}
	return ""
}

func toLunar(d lunar.Date) lunarDTO {
	return lunarDTO{Day: d.Day, Month: d.Month, Year: d.Year, Leap: d.Leap, Label: d.String(), FirstDay: d.Day == 1, FullMoon: d.Day == 15}
}

func toSource(s app.Source) sourceDTO {
	return sourceDTO{Provider: s.Provider, Label: s.Label, UpdatedAt: s.UpdatedAt, Stale: s.Stale}
}

func (h Handler) config(c *gin.Context) {
	v, err := h.Svc.GetConfig(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, toConfig(v))
}

func decodeInput(c *gin.Context) (domain.ConfigInput, bool) {
	var in domain.ConfigInput
	dec := json.NewDecoder(c.Request.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		httpx.Fail(c, problem.BadRequest.WithDetail("Thân request phải là JSON hợp lệ"))
		return in, false
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		httpx.Fail(c, problem.BadRequest.WithDetail("Chỉ nhận một đối tượng JSON"))
		return in, false
	}
	return in, true
}

func (h Handler) saveConfig(c *gin.Context) {
	in, ok := decodeInput(c)
	if !ok {
		return
	}
	v, replayed, err := h.Svc.SaveConfig(c.Request.Context(), in, c.GetHeader("Idempotency-Key"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	status := http.StatusCreated
	if replayed {
		status = http.StatusOK
	}
	c.JSON(status, toConfig(v))
}

func (h Handler) testConfig(c *gin.Context) {
	in, ok := decodeInput(c)
	if !ok {
		return
	}
	r, err := h.Svc.TestConnection(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	out := testDTO{OK: true, Provider: r.Provider, Condition: r.Condition, Days: r.Days}
	if r.TemperatureC != nil {
		v := domain.RoundInt(*r.TemperatureC)
		out.TemperatureC = &v
	}
	c.JSON(http.StatusOK, out)
}
