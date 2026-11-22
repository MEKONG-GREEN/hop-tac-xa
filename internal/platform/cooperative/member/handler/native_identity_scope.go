package handler

import (
	"net/http"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/member/app"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/httpx"
	"github.com/gin-gonic/gin"
)

func writeNativeIdentityScope(c *gin.Context, scope app.NativeIdentityScope) {
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"ref": scope.Ref.String(), "cooperative_ref": refPtr(scope.CooperativeRef),
		"active": scope.Active, "contact_access": scope.ContactAccess, "can_write": scope.CanWrite, "shared": scope.Shared})
}

func (h Handler) nativeHouseholdRegistrationScope(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	ref, ok := pathRef(c)
	if !ok {
		return
	}
	scope, err := h.Svc.NativeHouseholdRegistrationScope(c.Request.Context(), ref)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	writeNativeIdentityScope(c, scope)
}

func (h Handler) nativeHouseholdIdentityScope(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	ref, ok := householdRef(c)
	if !ok {
		return
	}
	scope, err := h.Svc.NativeHouseholdIdentityScope(c.Request.Context(), ref)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	writeNativeIdentityScope(c, scope)
}
