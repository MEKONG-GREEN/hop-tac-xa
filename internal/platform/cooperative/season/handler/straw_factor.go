package handler

import (
	"net/http"
	"strconv"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/store"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/httpx"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
	"github.com/gin-gonic/gin"
)

func factorDTO(e store.StrawFactorEvent) gin.H {
	var previous any
	if e.Previous != nil {
		previous = e.Previous.String()
	}
	return gin.H{"id": e.ID, "variety_code": e.VarietyCode, "percentage": e.Percentage.String(), "previous_percentage": previous,
		"revision": e.Seq, "effective_at": e.EffectiveAt, "recorded_at": e.RecordedAt, "actor_ref": e.ActorRef}
}
func (h Handler) listStrawFactors(c *gin.Context) {
	rows, err := h.Svc.ListStrawFactors(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": rows})
}
func (h Handler) writeStrawFactor(create bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			VarietyCode      string `json:"variety_code"`
			Percentage       string `json:"percentage"`
			ExpectedRevision int64  `json:"expected_revision"`
		}
		if !bindJSON(c, &req) {
			return
		}
		code := c.Param("variety_code")
		if create {
			code = req.VarietyCode
		}
		e, err := h.Svc.SaveStrawFactor(c.Request.Context(), code, req.Percentage, c.GetHeader("Idempotency-Key"), req.ExpectedRevision, create)
		if err != nil {
			httpx.Fail(c, err)
			return
		}
		status := http.StatusOK
		if create {
			status = http.StatusCreated
		}
		c.JSON(status, factorDTO(e))
	}
}
func (h Handler) strawFactorHistory(c *gin.Context) {
	after, err := strconv.ParseInt(c.DefaultQuery("before_revision", "0"), 10, 64)
	if err != nil {
		httpx.Fail(c, problem.BadRequest)
		return
	}
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if err != nil {
		httpx.Fail(c, problem.BadRequest)
		return
	}
	if limit < 1 || limit > 99 {
		limit = 20
	}
	rows, err := h.Svc.StrawFactorHistory(c.Request.Context(), c.Param("variety_code"), after, limit+1)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	var next any
	if len(rows) > limit {
		next = rows[limit-1].Seq
		rows = rows[:limit]
	}
	items := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		items = append(items, factorDTO(row))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_before_revision": next})
}
