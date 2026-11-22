package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

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
	// AH-280 (FN-10): quên mật khẩu — gửi mã OTP qua SMS, đặt mật khẩu mới bằng mã. Giới hạn theo IP như đăng nhập
	// (bù cho EX-01 báo rõ không tìm thấy tài khoản).
	rg.POST("/auth/password-otp", httpx.RateLimitByIP(h.Svc.Clock, loginPerMinute, loginBurst), h.passwordOTP)
	rg.POST("/auth/password-otp/reset", httpx.RateLimitByIP(h.Svc.Clock, loginPerMinute, loginBurst), h.passwordOTPReset)
}

type passwordOTPReq struct {
	Identifier string `json:"identifier" pii:"true"`
}

type passwordOTPResetReq struct {
	Identifier      string `json:"identifier" pii:"true"`
	ChallengeID     string `json:"challenge_id"`
	OTP             string `json:"otp" pii:"true"`
	NewPassword     string `json:"new_password" pii:"true"`
	ConfirmPassword string `json:"confirm_password" pii:"true"`
}

func (h Handler) passwordOTP(c *gin.Context) {
	noStore(c)
	var req passwordOTPReq
	if !bindJSON(c, &req) {
		return
	}
	res, err := h.Svc.RequestPasswordOTP(c.Request.Context(), req.Identifier)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "otp_sent", "challenge_id": res.ChallengeID, "phone_hint": res.PhoneHint,
		"expires_in": res.ExpiresIn, "resend_after": res.ResendAfter})
}

func (h Handler) passwordOTPReset(c *gin.Context) {
	noStore(c)
	var req passwordOTPResetReq
	if !bindJSON(c, &req) {
		return
	}
	in := app.PasswordOTPReset{Identifier: req.Identifier, OTP: req.OTP, NewPassword: req.NewPassword, ConfirmPassword: req.ConfirmPassword}
	if req.ChallengeID != "" {
		id, err := uuid.Parse(req.ChallengeID)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("challenge_id", "Mã thử thách không hợp lệ"))
			return
		}
		in.ChallengeID = id
	}
	if err := h.Svc.ResetPasswordWithOTP(c.Request.Context(), in); err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "password_reset"})
}

type loginReq struct {
	Identifier string `json:"identifier" pii:"true"`
	Password   string `json:"password" pii:"true"`
	OTP        string `json:"otp" pii:"true"`
	// ChallengeID: bước 2 của đăng nhập bằng OTP email (ADR-0061) — id thử thách bước 1 trả về.
	ChallengeID string `json:"challenge_id"`
	// NewPassword, ConfirmPassword: bước đổi mật khẩu lần đầu (FN-01/AC-04, AH-269) — gửi kèm mật khẩu tạm.
	NewPassword     string `json:"new_password" pii:"true"`
	ConfirmPassword string `json:"confirm_password" pii:"true"`
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
	in := app.LoginInput{Identifier: req.Identifier, Password: req.Password, OTP: req.OTP, NewPassword: req.NewPassword, ConfirmPassword: req.ConfirmPassword}
	if req.ChallengeID != "" {
		id, err := uuid.Parse(req.ChallengeID)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("challenge_id", "Mã thử thách không hợp lệ"))
			return
		}
		in.ChallengeID = id
	}
	res, err := h.Svc.Login(c.Request.Context(), in)
	if err != nil {
		var p *problem.Problem
		if errors.As(err, &p) && (p.Code == app.ErrLoginTempLocked.Code || p.Code == app.ErrLoginOTPRateLimited.Code) {
			if m, ok := p.Extra["retry_after_minutes"].(int); ok {
				c.Header("Retry-After", strconv.Itoa(m*60))
			}
		}
		httpx.Fail(c, err)
		return
	}
	if res.OTP != nil {
		c.JSON(http.StatusOK, gin.H{
			"status": res.Status, "challenge_id": res.OTP.ChallengeID, "email_hint": res.OTP.EmailHint,
			"expires_in": res.OTP.ExpiresIn, "resend_after": res.OTP.ResendAfter,
		})
		return
	}
	if res.Status == app.LoginPasswordChangeRequired {
		c.JSON(http.StatusOK, gin.H{"status": res.Status})
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
