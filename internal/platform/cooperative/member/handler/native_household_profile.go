package handler

import (
	"net/http"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/httpx"
	"github.com/gin-gonic/gin"
)

type nativeHouseholdIdentityDTO struct {
	FullName   string `json:"full_name" pii:"true"`
	Phone      string `json:"phone" pii:"true"`
	NationalID string `json:"national_id" pii:"true"`
	Address    string `json:"address" pii:"true"`
}

func (h Handler) nativeHouseholdProfile(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	ref, ok := householdRef(c)
	if !ok {
		return
	}
	out, err := h.Svc.NativeHouseholdProfile(c.Request.Context(), ref)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"household": toHouseholdDTO(out.Household), "contact_access": out.ContactAccess, "code": out.Household.Code, "revision": out.Household.Revision,
		"identity": nativeHouseholdIdentityDTO{FullName: out.Identity.FullName, Phone: out.Identity.Phone, NationalID: out.Identity.NationalID, Address: out.Identity.Address}})
}
