package handler

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/member/app"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/httpx"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func (h Handler) nativeSharingList(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	out, err := h.Svc.NativeSharingList(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}
func (h Handler) nativeSharingCandidates(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	out, err := h.Svc.NativeSharingCandidates(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}
func sharingJSON(c *gin.Context, v any) bool {
	d := json.NewDecoder(c.Request.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		httpx.Fail(c, problem.BadRequest)
		return false
	}
	if err := d.Decode(new(any)); err != io.EOF {
		httpx.Fail(c, problem.BadRequest)
		return false
	}
	return true
}
func (h Handler) nativeSharingCreate(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var req struct {
		Owner uuid.UUID `json:"owner_cooperative_ref"`
	}
	if !sharingJSON(c, &req) {
		return
	}
	out, err := h.Svc.NativeChangeSharing(c.Request.Context(), uuid.Nil, req.Owner, "request", 0, c.GetHeader("Idempotency-Key"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, out)
}
func (h Handler) nativeSharingTransition(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	ref, err := uuid.Parse(c.Param("ref"))
	if err != nil {
		httpx.Fail(c, app.ErrSharingNotFound)
		return
	}
	var req struct {
		Action   string `json:"action"`
		Revision int64  `json:"expected_revision"`
	}
	if !sharingJSON(c, &req) {
		return
	}
	out, err := h.Svc.NativeChangeSharing(c.Request.Context(), ref, uuid.Nil, req.Action, req.Revision, c.GetHeader("Idempotency-Key"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}
