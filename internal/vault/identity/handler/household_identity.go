package handler

import (
	"net/http"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/httpx"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/vault/identity/client"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func (h Handler) readHouseholdIdentity(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	ref, ok := refOf(c)
	if !ok {
		return
	}
	var req client.PurposeRequest
	if c.Request.ContentLength != 0 && !bind(c, &req) {
		return
	}
	out, err := h.Svc.ReadHouseholdIdentity(c.Request.Context(), purposeOf(c, req.Purpose), ref)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, client.IdentityResponse{Ref: ref.String(), Kind: string(out.Kind), FullName: out.FullName, Phone: out.Phone,
		NationalID: out.NationalID, Address: out.Address, AreaCode: out.AreaCode})
}

func (h Handler) lookupHouseholdPhone(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var req client.LookupRequest
	if !bind(c, &req) {
		return
	}
	ref, err := h.Svc.LookupHouseholdPhone(c.Request.Context(), purposeOf(c, req.Purpose), req.Value)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, client.LookupResponse{Ref: ref.String()})
}

func (h Handler) writeHouseholdIdentity(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	command, err := uuid.Parse(c.Param("command"))
	if err != nil || command.Version() != 7 {
		httpx.Fail(c, problem.Validation.WithField("command", "Cần UUIDv7 của thao tác"))
		return
	}
	var req client.HouseholdIdentityWriteRequest
	if !bind(c, &req) {
		return
	}
	err = h.Svc.WriteHouseholdIdentity(c.Request.Context(), purposeOf(c, req.Purpose), command, client.Identity{FullName: req.FullName, Phone: req.Phone, NationalID: req.NationalID, Address: req.Address})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
