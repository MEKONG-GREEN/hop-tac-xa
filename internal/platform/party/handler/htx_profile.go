package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/party/app"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/party/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/httpx"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
)

type htxProfileDTO struct {
	coopItemDTO
	Revision           int64  `json:"revision"`
	RepresentativeName string `json:"representative_name" pii:"true"`
	Phone              string `json:"phone" pii:"true"`
	ContactMasked      bool   `json:"contact_masked"`
	CanEdit            bool   `json:"can_edit"`
}

func (h Registry) getHTXProfile(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	ref, ok := pathRef(c)
	if !ok {
		return
	}
	profile, err := h.Reg.HTXProfile(c.Request.Context(), ref)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items, err := h.coopItems(c.Request.Context(), []app.Cooperative{profile.Cooperative})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, htxProfileDTO{coopItemDTO: items[0], Revision: profile.LastSeq,
		RepresentativeName: profile.RepresentativeName, Phone: profile.Phone, ContactMasked: profile.ContactMasked, CanEdit: profile.CanEdit})
}

type htxProfileRequest struct {
	Name               string `json:"name"`
	CommuneCode        string `json:"commune_code"`
	HeadOffice         string `json:"head_office"`
	RepresentativeName string `json:"representative_name" pii:"true"`
	Phone              string `json:"phone" pii:"true"`
}

func failHTXProfile(c *gin.Context, err error) {
	if p := problem.From(err); p.Code == "party.cooperative_name_taken" {
		err = p.WithDetail("Tên HTX đã tồn tại")
	}
	httpx.Fail(c, err)
}

func bindHTXProfile(c *gin.Context, req any) bool {
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(req); err != nil {
		httpx.Fail(c, problem.BadRequest.WithDetail("Nội dung hồ sơ không hợp lệ hoặc có trường không được sửa"))
		return false
	}
	var extra any
	if !errors.Is(decoder.Decode(&extra), io.EOF) {
		httpx.Fail(c, problem.BadRequest.WithDetail("Chỉ gửi một hồ sơ JSON"))
		return false
	}
	return true
}

func (h Registry) createHTXProfile(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if _, err := authz.Require(c.Request.Context(), authz.RolePlatformAdmin); err != nil {
		httpx.Fail(c, err)
		return
	}
	var req htxProfileRequest
	if !bindHTXProfile(c, &req) {
		return
	}
	result, err := h.Reg.CreateHTXProfile(c.Request.Context(), app.HTXProfileInput{Name: req.Name, CommuneCode: req.CommuneCode,
		HeadOffice: req.HeadOffice, RepresentativeName: req.RepresentativeName, Phone: req.Phone, IdemKey: idempotency.KeyFromContext(c.Request.Context())})
	if err != nil {
		failHTXProfile(c, err)
		return
	}
	status := http.StatusCreated
	if result.Replayed {
		status = http.StatusOK
		c.Header("Idempotent-Replayed", "true")
	}
	// Phản hồi ghi không chứa PII để receipt idempotency trong PF không lưu SĐT/tên.
	c.JSON(status, gin.H{"ref": result.Org.Ref.String(), "code": result.Org.Code, "revision": result.Event.Seq})
}

type htxProfileUpdateRequest struct {
	htxProfileRequest
	ExpectedRevision int64 `json:"expected_revision"`
}

func (h Registry) updateHTXProfile(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if _, err := authz.Require(c.Request.Context(), authz.RolePlatformAdmin, port.RoleCoopManager); err != nil {
		httpx.Fail(c, err)
		return
	}
	ref, ok := pathRef(c)
	if !ok {
		return
	}
	var req htxProfileUpdateRequest
	if !bindHTXProfile(c, &req) {
		return
	}
	result, err := h.Reg.UpdateHTXProfile(c.Request.Context(), app.UpdateHTXProfileInput{Ref: ref, ExpectedRevision: req.ExpectedRevision,
		HTXProfileInput: app.HTXProfileInput{Name: req.Name, CommuneCode: req.CommuneCode, HeadOffice: req.HeadOffice,
			RepresentativeName: req.RepresentativeName, Phone: req.Phone, IdemKey: idempotency.KeyFromContext(c.Request.Context())}})
	if err != nil {
		failHTXProfile(c, err)
		return
	}
	if result.Replayed {
		c.Header("Idempotent-Replayed", "true")
	}
	c.JSON(http.StatusOK, gin.H{"ref": result.Org.Ref.String(), "code": result.Org.Code, "revision": result.Event.Seq})
}
