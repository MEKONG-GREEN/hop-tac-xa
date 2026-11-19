package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/httpx"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/vault/identity/client"
)

func (h Handler) validateRegistryContact(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	ref, ok := refOf(c)
	if !ok {
		return
	}
	var req client.PurposeRequest
	if !bind(c, &req) {
		return
	}
	if err := h.Svc.ValidateRegistryContact(c.Request.Context(), purposeOf(c, req.Purpose), ref); err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h Handler) amendRegistryContact(c *gin.Context) {
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
	var req client.RegistryContactRequest
	if !bind(c, &req) {
		return
	}
	revision, err := h.Svc.AmendRegistryContact(c.Request.Context(), purposeOf(c, req.Purpose), ref, command,
		client.Identity{FullName: req.FullName, Phone: req.Phone})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, client.WriteResponse{Ref: ref.String(), RevisionRef: revision.String()})
}
