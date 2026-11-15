package handler

// Đồng ý thu thập ảnh gắn GPS và dữ liệu sản xuất (AH-23, REQ-011, KH-24):
//
//	GET  /v1/me/data-consent — người gọi có phải đồng ý không, nội dung và phiên bản hiện hành
//	POST /v1/me/data-consent — ghi bằng chứng đồng ý (cần Idempotency-Key)
//
// Middleware() chặn các route nhận ảnh/GPS (domain.IsDataCollectionRoute) bằng dataConsentGuard.

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/httpx"
)

// RegisterDataConsentApp gắn route đồng ý thu thập dữ liệu vào Application API.
func (h Handler) RegisterDataConsentApp(rg *gin.RouterGroup) {
	rg.GET("/me/data-consent", h.dataConsentStatus)
	rg.POST("/me/data-consent", h.recordDataConsent)
}

type dataConsentStatusDTO struct {
	Required    bool    `json:"required"`
	Version     *string `json:"version"`
	Text        *string `json:"text"`
	ConsentedAt *string `json:"consented_at"`
}

func (h Handler) dataConsentStatus(c *gin.Context) {
	v, err := h.Svc.DataConsentStatus(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, dataConsentStatusDTO{Required: v.Required, Version: v.Version, Text: v.Text, ConsentedAt: timeStr(v.ConsentedAt)})
}

type dataConsentReq struct {
	Version string `json:"version"`
}

type dataConsentRecordDTO struct {
	ID          string `json:"id"`
	Version     string `json:"version"`
	ConsentedAt string `json:"consented_at"`
}

// recordDataConsent trả 201 khi ghi mới, 200 khi đã đồng ý rồi hoặc phát lại.
func (h Handler) recordDataConsent(c *gin.Context) {
	var req dataConsentReq
	if !bindJSON(c, &req) {
		return
	}
	rec, created, err := h.Svc.RecordDataConsent(c.Request.Context(), req.Version, key(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	c.JSON(status, dataConsentRecordDTO{ID: rec.ID.String(), Version: rec.TextVersion,
		ConsentedAt: rec.EventTime.UTC().Format(time.RFC3339)})
}

// dataConsentGuard chặn route nhận ảnh/GPS khi tài khoản HTX chưa đồng ý phiên bản hiện hành (AC3). Trả false khi đã
// ghi lỗi vào phản hồi.
func (h Handler) dataConsentGuard(c *gin.Context) bool {
	if !domain.IsDataCollectionRoute(c.Request.Method, c.FullPath()) {
		return true
	}
	if err := h.Svc.RequireDataConsent(c.Request.Context()); err != nil {
		httpx.Fail(c, err)
		return false
	}
	return true
}
