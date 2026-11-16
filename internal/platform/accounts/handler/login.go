package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/app"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/httpx"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
)

// Giới hạn tần suất theo địa chỉ gọi cho đăng nhập (FN-01, ngoài giới hạn chung của /v1/public): dồn 10, sau đó 10/phút.
// Trong bộ nhớ từng bản sao — bộ đếm lần sai và khóa tạm do máy chủ giữ (Keycloak và login_probe), không ở đây.
const (
	loginPerMinute = 10
	loginBurst     = 10
)

// registerLogin gắn route CÔNG KHAI của màn đăng nhập riêng (FN-01, ADR-0060). Không đòi và không đọc Idempotency-Key:
// nhóm /v1/public không có middleware idempotency nên thân chứa mật khẩu không vào kho idempotency.
func (h Handler) registerLogin(rg *gin.RouterGroup) {
	rg.POST("/auth/login", httpx.RateLimitByIP(h.Svc.Clock, loginPerMinute, loginBurst), h.login)
	rg.POST("/auth/refresh", h.refresh)
	rg.POST("/auth/logout", h.logout)
}

type loginReq struct {
	Identifier string `json:"identifier" pii:"true"`
	Password   string `json:"password" pii:"true"`
	OTP        string `json:"otp" pii:"true"`
}

type refreshReq struct {
	RefreshToken string `json:"refresh_token" pii:"true"`
}

// noStore chặn bộ nhớ đệm và rò Referer cho mọi phản hồi mang mật khẩu hoặc token.
func noStore(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	c.Header("Referrer-Policy", "no-referrer")
}

func tokenBody(status string, ts port.TokenSet) gin.H {
	return gin.H{
		"status": status, "access_token": ts.AccessToken, "refresh_token": ts.RefreshToken,
		"expires_in": ts.ExpiresIn, "refresh_expires_in": ts.RefreshExpiresIn, "token_type": "Bearer",
	}
}

func (h Handler) login(c *gin.Context) {
	noStore(c)
	var req loginReq
	if !bindJSON(c, &req) {
		return
	}
	res, err := h.Svc.Login(c.Request.Context(), app.LoginInput{Identifier: req.Identifier, Password: req.Password, OTP: req.OTP})
	if err != nil {
		var p *problem.Problem
		if errors.As(err, &p) && p.Code == app.ErrLoginTempLocked.Code {
			if m, ok := p.Extra["retry_after_minutes"].(int); ok {
				c.Header("Retry-After", strconv.Itoa(m*60))
			}
		}
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, tokenBody(res.Status, res.Tokens))
}

func (h Handler) refresh(c *gin.Context) {
	noStore(c)
	var req refreshReq
	if !bindJSON(c, &req) {
		return
	}
	ts, err := h.Svc.Refresh(c.Request.Context(), req.RefreshToken)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, tokenBody(app.LoginAuthenticated, ts))
}

func (h Handler) logout(c *gin.Context) {
	noStore(c)
	var req refreshReq
	if !bindJSON(c, &req) {
		return
	}
	if err := h.Svc.Logout(c.Request.Context(), req.RefreshToken); err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
