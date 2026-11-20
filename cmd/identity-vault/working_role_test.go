package main

import (
	"context"
	"net/http/httptest"
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
