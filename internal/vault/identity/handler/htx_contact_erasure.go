package handler

import (
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/httpx"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/vault/identity/client"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"net/http"
)

func (h Handler) validateHTXContactErasure(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	ref, ok := refOf(c)
	if !ok {
		return
	}
	var req client.PurposeRequest
	if !bind(c, &req) {
		return
	}
	if err := h.Svc.ValidateHTXContactErasure(c.Request.Context(), purposeOf(c, req.Purpose), ref); err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h Handler) eraseHTXContact(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	ref, ok := refOf(c)
	if !ok {
		return
	}
	command, err := uuid.Parse(c.Param("command"))
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("command", "Cần UUIDv7 của thao tác xóa"))
		return
	}
	var req client.PurposeRequest
	if !bind(c, &req) {
		return
	}
	if err := h.Svc.EraseHTXContact(c.Request.Context(), purposeOf(c, req.Purpose), ref, command); err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
