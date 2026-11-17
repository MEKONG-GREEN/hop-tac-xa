package httpx_test

// ADR-0061: yếu tố thứ hai ngoài amr (phiên đã qua mã OTP email do pf/accounts ghi) — AuthenticateAttested đặt
// Principal.MFA; RequireMFAForRoles chặn vai OTP email chưa qua yếu tố thứ hai trên Application API.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/httpx"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/telemetry"
)

type fakeAttestor struct {
	sid   string
	err   error
	calls int
}

func (a *fakeAttestor) SecondFactorVerified(_ context.Context, p authz.Principal) (bool, error) {
	a.calls++
	return p.SessionID != "" && p.SessionID == a.sid, a.err
}

func TestRequireMFAForRoles_ChanAdminChuaQuaYeuToThuHai(t *testing.T) {
	iss := authztest.New(t)
	att := &fakeAttestor{sid: "phien-da-qua-ma"}
	eng := httpx.NewEngine(telemetry.NewLogger("test", nil), 1<<10)
	var sawMFA bool
	eng.GET("/x", httpx.AuthenticateAttested(iss.Verifier(), att), httpx.RequireMFAForRoles(authz.RolePlatformAdmin), func(c *gin.Context) {
		p, _ := authz.FromContext(c.Request.Context())
		sawMFA = p.MFA
		c.Status(http.StatusNoContent)
	})
	do := func(tk authztest.Token) int {
		sawMFA = false
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		req.Header.Set("Authorization", "Bearer "+iss.Sign(t, tk))
		rec := httptest.NewRecorder()
		eng.ServeHTTP(rec, req)
		return rec.Code
	}
	admin := func(sid string, mfa bool) authztest.Token {
		return authztest.Token{Subject: ids.New(), TenantID: ids.New(), Roles: []string{authz.RolePlatformAdmin}, MFA: mfa, SessionID: sid}
	}

	if code := do(admin("phien-khac", false)); code != http.StatusForbidden {
		t.Fatalf("Admin chỉ có mật khẩu, phiên chưa qua mã: %d", code)
	}
	if code := do(admin("", false)); code != http.StatusForbidden {
		t.Fatalf("Admin chỉ có mật khẩu, token không có sid: %d", code)
	}
	if code := do(admin("phien-da-qua-ma", false)); code != http.StatusNoContent || !sawMFA {
		t.Fatalf("Admin có phiên đã qua mã email: %d mfa=%v", code, sawMFA)
	}
	calls := att.calls
	if code := do(admin("", true)); code != http.StatusNoContent || !sawMFA || att.calls != calls {
		t.Fatalf("token có amr otp thì không cần tra phiên: %d (tra thêm %d lần)", code, att.calls-calls)
	}
	if code := do(authztest.Token{Subject: ids.New(), TenantID: ids.New(), Roles: []string{"coop_manager"}}); code != http.StatusNoContent || sawMFA {
		t.Fatalf("vai thường không bị chặn và không được đánh dấu MFA: %d mfa=%v", code, sawMFA)
	}
	// Tra phiên lỗi → 503, không cho qua.
	att.err = problem.New(http.StatusServiceUnavailable, "accounts.dependency_unavailable", "lỗi")
	if code := do(admin("phien-da-qua-ma", false)); code != http.StatusServiceUnavailable {
		t.Fatalf("tra phiên lỗi phải fail-closed: %d", code)
	}
}

// Authenticate (không có bộ xác nhận) giữ hành vi cũ: MFA chỉ theo amr.
func TestAuthenticate_KhongCoBoXacNhanThiChiTheoAMR(t *testing.T) {
	iss := authztest.New(t)
	eng := httpx.NewEngine(telemetry.NewLogger("test", nil), 1<<10)
	eng.GET("/x", httpx.Authenticate(iss.Verifier()), httpx.RequireMFA(), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Authorization", "Bearer "+iss.Sign(t, authztest.Token{Subject: ids.New(), TenantID: ids.New(),
		Roles: []string{authz.RolePlatformAdmin}, SessionID: "phien"}))
	rec := httptest.NewRecorder()
	eng.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("không có bộ xác nhận, token chỉ mật khẩu phải bị chặn: %d", rec.Code)
	}
}
