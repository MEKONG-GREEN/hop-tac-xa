package handler

import (
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/party/app"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/httpx"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/idempotency"
	"github.com/gin-gonic/gin"
	"net/http"
)

func (h Registry) cooperativeDeletion(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if _, err := authz.Require(c.Request.Context(), authz.RolePlatformAdmin); err != nil {
		httpx.Fail(c, err)
		return
	}
	ref, ok := pathRef(c)
	if !ok {
		return
	}
	check, err := h.Reg.CooperativeDeletion(c.Request.Context(), ref)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ref": ref.String(), "code": check.Code, "name": check.Name, "revision": check.Revision,
		"households": check.Households, "accounts": check.Accounts, "pending_accounts": check.PendingAccounts,
		"has_sharing": check.HasSharing, "has_business": check.HasBusiness, "can_delete": check.CanDelete()})
}

func (h Registry) deleteCooperative(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if _, err := authz.Require(c.Request.Context(), authz.RolePlatformAdmin); err != nil {
		httpx.Fail(c, err)
		return
	}
	ref, ok := pathRef(c)
	if !ok {
		return
	}
	var req struct {
		ExpectedRevision int64 `json:"expected_revision"`
	}
	if !bindHTXProfile(c, &req) {
		return
	}
	result, err := h.Reg.DeleteCooperative(c.Request.Context(), app.DeleteCooperativeInput{Ref: ref, ExpectedRevision: req.ExpectedRevision, IdemKey: idempotency.KeyFromContext(c.Request.Context())})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	if result.Replayed {
		c.Header("Idempotent-Replayed", "true")
	}
	c.JSON(http.StatusOK, gin.H{"ref": ref.String(), "code": result.Org.Code, "revision": result.Event.Seq, "deleted": true})
}
