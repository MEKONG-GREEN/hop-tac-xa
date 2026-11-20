package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/masterdata/app"
	partyport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/party/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/httpx"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tabular"
)

func (h Handler) registerUnitManagement(rg *gin.RouterGroup) {
	httpx.AllowBody(rg, http.MethodPost, "/masterdata/admin-unit-imports", app.MaxCatalogImportBytes)
	rg.POST("/masterdata/admin-unit-imports", h.importManagedUnits)
	rg.GET("/masterdata/admin-unit-template", h.managedUnitTemplate)
	rg.GET("/masterdata/admin-unit-profiles", h.managedUnits)
	rg.POST("/masterdata/admin-unit-profiles", h.createManagedUnit)
	rg.POST("/masterdata/admin-unit-profiles/:code/revisions", h.updateManagedUnit)
}

func (h Handler) importManagedUnits(c *gin.Context) {
	result, err := h.Svc.ImportManagedUnits(c.Request.Context(), c.Request.Body, idempotency.KeyFromContext(c.Request.Context()))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	if result.Replayed {
		c.Header("Idempotent-Replayed", "true")
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, result)
}

func (h Handler) managedUnitTemplate(c *gin.Context) {
	if _, err := authz.Require(c.Request.Context(), authz.RolePlatformAdmin); err != nil {
		httpx.Fail(c, err)
		return
	}
	var out bytes.Buffer
	if err := tabular.WriteXLSX(&out, "Đơn vị hành chính", []string{"Tên", "Cấp", "Tỉnh / thành phố trực thuộc"}, nil); err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Content-Disposition", `attachment; filename="don-vi-hanh-chinh.xlsx"`)
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", out.Bytes())
}

func strictUnitJSON(c *gin.Context, value any) bool {
	d := json.NewDecoder(c.Request.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		httpx.Fail(c, problem.BadRequest.WithDetail("Dữ liệu đơn vị hành chính không hợp lệ"))
		return false
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		httpx.Fail(c, problem.BadRequest.WithDetail("Chỉ gửi một object JSON"))
		return false
	}
	return true
}

func (h Handler) managedUnits(c *gin.Context) {
	count := 50
	if raw := c.Query("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 200 {
			httpx.Fail(c, problem.Validation.WithField("limit", "Từ 1 đến 200"))
			return
		}
		count = n
	}
	page, err := h.Svc.ManagedUnitList(c.Request.Context(), c.Query("parent"), c.Query("q"), c.Query("state"), c.Query("after"), count)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"items": page.Items, "next_cursor": page.Next})
}

func (h Handler) createManagedUnit(c *gin.Context) {
	var req struct {
		Name   string          `json:"name"`
		Level  partyport.Level `json:"level"`
		Parent string          `json:"parent_code"`
		Active *bool           `json:"active"`
	}
	if !strictUnitJSON(c, &req) {
		return
	}
	active := true
	if req.Active != nil {
		active = *req.Active
	}
	result, replayed, err := h.Svc.CreateManagedUnit(c.Request.Context(), app.ManagedUnitInput{Name: req.Name, Level: req.Level, ParentCode: req.Parent, Active: active}, idempotency.KeyFromContext(c.Request.Context()))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	if replayed {
		c.Header("Idempotent-Replayed", "true")
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusCreated, result)
}

func (h Handler) updateManagedUnit(c *gin.Context) {
	var req struct {
		Name     *string `json:"name"`
		Active   *bool   `json:"active"`
		Expected *int64  `json:"expected_revision"`
	}
	if !strictUnitJSON(c, &req) {
		return
	}
	if req.Name == nil || req.Active == nil || req.Expected == nil {
		httpx.Fail(c, problem.Validation.WithField("body", "Tên, trạng thái và phiên bản là bắt buộc"))
		return
	}
	result, replayed, err := h.Svc.UpdateManagedUnit(c.Request.Context(), c.Param("code"), *req.Name, *req.Active, *req.Expected, idempotency.KeyFromContext(c.Request.Context()))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	if replayed {
		c.Header("Idempotent-Replayed", "true")
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, result)
}
