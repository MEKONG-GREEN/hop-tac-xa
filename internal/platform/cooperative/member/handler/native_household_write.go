package handler

import (
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/member/app"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/httpx"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"net/http"
)

type nativeHouseholdRequest struct {
	app.NativeHouseholdMetadata
	Identity         nativeHouseholdIdentityDTO `json:"identity"`
	ExpectedRevision int64                      `json:"expected_revision"`
}

func (h Handler) nativeCreateHousehold(c *gin.Context) { h.nativeSaveHousehold(c, uuid.Nil) }
func (h Handler) nativeUpdateHousehold(c *gin.Context) {
	ref, ok := householdRef(c)
	if ok {
		h.nativeSaveHousehold(c, ref)
	}
}
func (h Handler) nativeSaveHousehold(c *gin.Context, ref uuid.UUID) {
	c.Header("Cache-Control", "no-store")
	req := nativeHouseholdRequest{NativeHouseholdMetadata: app.NativeHouseholdMetadata{Active: true}}
	if !bindJSON(c, &req) {
		return
	}
	out, err := h.Svc.NativeSaveHousehold(c.Request.Context(), ref, req.ExpectedRevision, c.GetHeader("Idempotency-Key"), req.NativeHouseholdMetadata,
		port.HouseholdIdentity{FullName: req.Identity.FullName, Phone: req.Identity.Phone, NationalID: req.Identity.NationalID, Address: req.Identity.Address})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	status := http.StatusOK
	if ref == uuid.Nil {
		status = http.StatusCreated
	}
	c.JSON(status, out)
}
func (h Handler) nativeDeleteHousehold(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	ref, ok := householdRef(c)
	if !ok {
		return
	}
	var req struct {
		ExpectedRevision int64 `json:"expected_revision"`
	}
	if !bindJSON(c, &req) {
		return
	}
	out, err := h.Svc.NativeDeleteHousehold(c.Request.Context(), ref, req.ExpectedRevision, c.GetHeader("Idempotency-Key"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}
func (h Handler) nativeHouseholdCommandScope(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	ref, err := uuid.Parse(c.Param("command"))
	if err != nil {
		httpx.Fail(c, problem.NotFound)
		return
	}
	out, err := h.Svc.NativeHouseholdCommandScope(c.Request.Context(), ref)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}
