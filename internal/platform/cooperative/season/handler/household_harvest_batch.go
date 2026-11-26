package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/app"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/httpx"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tabular"
)

func harvestBatchOut(c *gin.Context, r app.HarvestBatchResult) {
	rows := make([]gin.H, 0, len(r.Rows))
	for _, x := range r.Rows {
		row := gin.H{"line": x.Line, "status": x.Status, "season_ref": x.SeasonRef, "code": nil, "reason": nil, "needs_confirm": x.NeedsConfirm, "harvest_id": x.HarvestID}
		if x.Code != "" {
			row["code"], row["reason"] = x.Code, x.Reason
		}
		rows = append(rows, row)
	}
	c.JSON(http.StatusOK, gin.H{"accepted": r.Accepted, "duplicate": r.Duplicate, "rejected": r.Rejected, "rows": rows})
}

// householdHarvestBatch: AH-177 — bảng nhập nhanh "Lưu tất cả" cho vụ nông hộ; kết quả theo dòng.
func (h Handler) householdHarvestBatch(c *gin.Context) {
	coop, ok := calendarRef(c, "ref")
	if !ok {
		return
	}
	var req struct {
		Rows []struct {
			Line            int    `json:"line"`
			SeasonRef       string `json:"season_ref"`
			ExpectedEventID string `json:"expected_season_event_id"`
			MassT           string `json:"mass_t"`
			HarvestDate     string `json:"harvest_date"`
			ZeroConfirmed   bool   `json:"zero_confirmed"`
		} `json:"rows"`
	}
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		httpx.Fail(c, problem.Validation.WithField("body", "Dữ liệu nhập hàng loạt không hợp lệ"))
		return
	}
	rows := make([]app.HarvestBatchRow, len(req.Rows))
	for i, r := range req.Rows {
		rows[i] = app.HarvestBatchRow{Line: r.Line, SeasonRef: r.SeasonRef, ExpectedEventID: r.ExpectedEventID, MassT: r.MassT,
			HarvestDate: r.HarvestDate, ZeroConfirmed: r.ZeroConfirmed}
	}
	res, err := h.Svc.HouseholdHarvestBatch(c.Request.Context(), coop, rows, app.SourceWeb, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	harvestBatchOut(c, res)
}

// householdHarvestImport: AH-177 — nhập tệp Excel mẫu (.xlsx hoặc CSV UTF-8, thân request là nội dung tệp).
func (h Handler) householdHarvestImport(c *gin.Context) {
	coop, ok := calendarRef(c, "ref")
	if !ok {
		return
	}
	res, err := h.Svc.ImportHouseholdHarvests(c.Request.Context(), coop, c.Request.Body, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	harvestBatchOut(c, res)
}

// householdHarvestTemplate: AH-177 — tệp mẫu .xlsx sinh sẵn các vụ chờ khai báo (scope=all: mọi vụ đang canh tác).
func (h Handler) householdHarvestTemplate(c *gin.Context) {
	coop, ok := calendarRef(c, "ref")
	if !ok {
		return
	}
	scope := c.DefaultQuery("scope", "harvest_recorded")
	if scope != "harvest_recorded" && scope != "all" {
		httpx.Fail(c, problem.Validation.WithField("scope", "harvest_recorded hoặc all"))
		return
	}
	list, err := h.Svc.HarvestTemplate(c.Request.Context(), coop, scope == "all")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	header := []string{"Mã vụ", "Mã hộ", "Mùa vụ", "Cây trồng", "Giống", "Diện tích (ha)", "Ngày thu hoạch dự kiến",
		"Sản lượng (tấn)", "Ngày thu hoạch cuối", "Xác nhận 0"}
	rows := make([][]string, 0, len(list))
	for _, r := range list {
		s := r.Season
		expected := ""
		if !s.ExpectedHarvestDate.IsZero() {
			expected = s.ExpectedHarvestDate.Format("02/01/2006")
		}
		rows = append(rows, []string{s.SeasonRef.String(), r.HouseholdCode, s.SeasonCode + " " + strconv.Itoa(s.SeasonYear), s.CropCode, s.VarietyCode,
			s.AreaHa.StringFixed(2), expected, "", "", ""})
	}
	var out bytes.Buffer
	if err := tabular.WriteXLSX(&out, "San luong", header, rows); err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Content-Disposition", `attachment; filename="mau-khai-bao-san-luong.xlsx"`)
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, tabular.ContentTypeXLSX, out.Bytes())
}
