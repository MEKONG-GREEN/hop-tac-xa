package handler

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/app"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/httpx"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
)

func (h Handler) nativeCreate(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var in domain.NativeAccountInput
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&in); err != nil {
		httpx.Fail(c, problem.BadRequest.WithDetail("Thân request FN-07 phải đúng cấu trúc JSON"))
		return
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		httpx.Fail(c, problem.BadRequest.WithDetail("Chỉ nhận một đối tượng JSON"))
		return
	}
	res, err := h.Svc.NativeCreate(c.Request.Context(), in, key(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	status := http.StatusCreated
	if res.Replayed {
		status = http.StatusOK
	}
	user := toUserDTO(res.User)
	// PF may retain references and state, never IAM profile PII or the credential.
	cachedUser := user
	cachedUser.Username = ""
	cachedUser.Email = ""
	cachedUser.FirstName = ""
	cachedUser.LastName = ""
	cachedUser.Phone = nil
	stored, err := json.Marshal(gin.H{"user": cachedUser, "temporary_password": nil})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	idempotency.SetStoredBody(c, stored)
	c.Header("Location", "/v1/accounts/htx-users/"+res.User.User.ID.String())
	c.JSON(status, gin.H{"user": user, "temporary_password": strPtr(res.TemporaryPassword)})
}

func (h Handler) nativeUsers(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	users, err := h.Svc.NativeUsers(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]userDTO, 0, len(users))
	for _, u := range users {
		items = append(items, toUserDTO(u))
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h Handler) nativeUser(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	id, ok := pathID(c)
	if !ok {
		return
	}
	u, err := h.Svc.NativeUser(c.Request.Context(), id.String())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toUserDTO(u))
}

// nativeUserReply trả tài khoản sau khi sửa/khóa; bản lưu cho phát lại (idempotency) không giữ PII của IAM.
func nativeUserReply(c *gin.Context, u app.UserView, replayed bool) {
	user := toUserDTO(u)
	cached := user
	cached.Username, cached.Email, cached.FirstName, cached.LastName, cached.Phone = "", "", "", "", nil
	stored, err := json.Marshal(gin.H{"user": cached, "replayed": replayed})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	idempotency.SetStoredBody(c, stored)
	c.JSON(http.StatusOK, gin.H{"user": user, "replayed": replayed})
}

func (h Handler) nativeUpdate(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	id, ok := pathID(c)
	if !ok {
		return
	}
	var in app.NativeUpdateInput
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&in); err != nil {
		httpx.Fail(c, problem.BadRequest.WithDetail("Thân request FN-07 phải đúng cấu trúc JSON"))
		return
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		httpx.Fail(c, problem.BadRequest.WithDetail("Chỉ nhận một đối tượng JSON"))
		return
	}
	u, replayed, err := h.Svc.NativeUpdate(c.Request.Context(), id, in, key(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	nativeUserReply(c, u, replayed)
}

func (h Handler) nativeLock(locked bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		id, ok := pathID(c)
		if !ok {
			return
		}
		u, replayed, err := h.Svc.NativeSetLocked(c.Request.Context(), id, locked, key(c))
		if err != nil {
			httpx.Fail(c, err)
			return
		}
		nativeUserReply(c, u, replayed)
	}
}
