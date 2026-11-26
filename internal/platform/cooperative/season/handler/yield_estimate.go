package handler

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/app"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/httpx"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
)

// saveYieldEstimate: AH-87 — Thành viên HTX nhập, sửa hoặc xóa (null) sản lượng lúa, rơm dự kiến của vụ theo hộ đang
// canh tác. Đại diện HTX chỉ xem.
func (h Handler) saveYieldEstimate(c *gin.Context) {
	coop, hh, season, ok := taskPath(c)
	if !ok {
		return
	}
	var req struct {
		ExpectedSeq *int64  `json:"expected_seq"`
		RiceT       *string `json:"rice_t"`
		StrawT      *string `json:"straw_t"`
	}
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		httpx.Fail(c, problem.Validation.WithField("body", "Dữ liệu sản lượng dự kiến không hợp lệ"))
		return
	}
	if req.ExpectedSeq == nil {
		httpx.Fail(c, problem.Validation.WithField("expected_seq", "Bắt buộc"))
		return
	}
	r, err := h.Svc.SaveYieldEstimate(c.Request.Context(), app.YieldEstimateInput{CooperativeRef: coop, HouseholdRef: hh, SeasonRef: season,
		ExpectedSeq: *req.ExpectedSeq, RiceT: req.RiceT, StrawT: req.StrawT, IdemKey: idem(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	if r.Replayed {
		c.Header("Idempotent-Replayed", "true")
	}
	c.JSON(http.StatusOK, gin.H{"yield_estimate": yieldEstimateOut(&r.Estimate)})
}

// yieldEstimateOut: tấn có 2 chữ số lẻ; rơm null với vụ không phải lúa; seq 0 = chưa có dòng trong sổ (tính tại chỗ).
func yieldEstimateOut(e *app.YieldEstimateView) gin.H {
	if e == nil {
		return nil
	}
	ton := func(d *decimal.Decimal) *string {
		if d == nil {
			return nil
		}
		v := d.StringFixed(2)
		return &v
	}
	x := e.YieldEstimate
	rice := x.Rice()
	var strawPct *string
	if x.StrawPct != nil {
		v := x.StrawPct.String()
		strawPct = &v
	}
	return gin.H{"seq": e.Seq, "is_rice": x.IsRice, "area_ha": x.AreaHa.StringFixed(2),
		"conventional_yield_t_per_ha": x.BaseYield.String(), "straw_pct": strawPct,
		"computed_rice_t": ton(&x.ComputedRice), "computed_straw_t": ton(x.ComputedStraw),
		"entered_rice_t": ton(x.EnteredRice), "entered_straw_t": ton(x.EnteredStraw),
		"rice_t": ton(&rice), "straw_t": ton(x.Straw()), "updated_at": tsPtr(e.UpdatedAt), "updated_by": e.UpdatedBy}
}
