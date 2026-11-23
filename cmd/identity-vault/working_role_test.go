package main

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/gin-gonic/gin"
)

func TestVaultWorkingRoleDeniesUnionAndMissingChoice(t *testing.T) {
	for _, tc := range []struct {
		role    string
		status  int
		manager bool
	}{{"", 409, false}, {"htx_member", 200, false}, {"coop_manager", 200, true}, {"platform_admin", 403, false}} {
		r := gin.New()
		r.Use(func(c *gin.Context) {
			c.Request = c.Request.WithContext(authz.WithPrincipal(context.Background(), authz.Principal{Roles: []string{"coop_manager", "htx_member"}}))
			c.Next()
		}, vaultWorkingRole(true))
		r.GET("/v1/proof", func(c *gin.Context) {
			p, _ := authz.FromContext(c.Request.Context())
			if p.Has("coop_manager") != tc.manager {
				t.Error("Vault authorization union")
			}
			c.Status(200)
		})
		req := httptest.NewRequest("GET", "/v1/proof", nil)
		req.Header.Set("X-HTX-Role", tc.role)
		response := httptest.NewRecorder()
		r.ServeHTTP(response, req)
		if response.Code != tc.status {
			t.Fatalf("%q: %d", tc.role, response.Code)
		}
	}
}

// FN-TK-01/EX-02, EX-03 (AH-222): token đã làm mới không còn vai đang chọn → 409 để chọn lại; không còn vai nào → 403
// mã riêng. Lựa chọn Admin giả vẫn là 403.
func TestVaultWorkingRoleRevokedAndNoRole(t *testing.T) {
	for _, tc := range []struct {
		name   string
		roles  []string
		header string
		status int
		code   string
	}{
		{"member removed", []string{"coop_manager"}, "htx_member", 409, "accounts.working_role_revoked"},
		{"forged admin", []string{"coop_manager"}, "platform_admin", 403, "forbidden"},
		{"no role", nil, "htx_member", 403, "accounts.no_active_role"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := gin.New()
			r.Use(func(c *gin.Context) {
				c.Request = c.Request.WithContext(authz.WithPrincipal(context.Background(), authz.Principal{Roles: tc.roles}))
				c.Next()
			}, vaultWorkingRole(true))
			r.GET("/v1/proof", func(c *gin.Context) { c.Status(200) })
			req := httptest.NewRequest("GET", "/v1/proof", nil)
			req.Header.Set("X-HTX-Role", tc.header)
			response := httptest.NewRecorder()
			r.ServeHTTP(response, req)
			if response.Code != tc.status || !strings.Contains(response.Body.String(), `"code":"`+tc.code+`"`) {
				t.Fatalf("got %d %s", response.Code, response.Body.String())
			}
		})
	}
}
