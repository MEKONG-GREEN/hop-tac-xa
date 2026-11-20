package handler

import (
	"net/http"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/httpx"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/vault/identity/client"
	"github.com/gin-gonic/gin"
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
