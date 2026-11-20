package handler

import (
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/httpx"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/vault/identity/client"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"net/http"
)

func (h Handler) readHTXContact(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	ref, ok := refOf(c)
	if !ok {
		return
	}
	var req client.HTXContactRequest
	if !bind(c, &req) {
		return
	}
	out, err := h.Svc.ReadHTXContact(c.Request.Context(), purposeOf(c, req.Purpose), ref, req.Masked)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h Handler) writeHTXContact(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	ref, ok := refOf(c)
	if !ok {
		return
	}
	command, err := uuid.Parse(c.Param("command"))
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("command", "Cần UUIDv7 của thao tác"))
		return
	}
	var req client.HTXContactRequest
	if !bind(c, &req) {
		return
	}
	if err := h.Svc.WriteHTXContact(c.Request.Context(), purposeOf(c, req.Purpose), ref, command, req.Contact); err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
