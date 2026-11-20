package handler

import (
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/httpx"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"net/http"
)

func (h Handler) householdPhotoKey(c *gin.Context) {
	ref, err := uuid.Parse(c.Param("ref"))
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("ref", "Mã hộ không hợp lệ"))
		return
	}
	key, err := h.Svc.HouseholdPhotoKey(c.Request.Context(), ref)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"ref": ref, "public_key": key})
}
