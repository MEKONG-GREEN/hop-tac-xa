package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/httpx"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tabular"
)

// exportSharedCatalog: Excel chỉ đọc của danh mục dùng chung Vật tư/Máy móc (VTKM-Q-07, AH-274).
func (h Handler) exportSharedCatalog(c *gin.Context) {
	catalog := c.Param("catalog")
	data, err := h.Svc.ExportSharedCatalog(c.Request.Context(), catalog)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Content-Disposition", `attachment; filename="danh-muc-`+catalog+`.xlsx"`)
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, tabular.ContentTypeXLSX, data)
}
