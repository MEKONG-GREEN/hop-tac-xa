package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/app"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestNativeRoleChoiceIsRequiredAndCannotUnion(t *testing.T) {
	for _, tc := range []struct {
		name, role string
		status     int
		manager    bool
	}{
		{"missing", "", 409, false}, {"member", "htx_member", 200, false}, {"representative", "coop_manager", 200, true}, {"forged admin", "platform_admin", 403, false}, {"union", "coop_manager,htx_member", 403, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := authz.Principal{ActorRef: uuid.New(), TenantID: uuid.New(), OrgRef: uuid.New(), Roles: []string{"coop_manager", "htx_member"}}
			router := gin.New()
			h := Handler{Svc: &app.Service{NativeHTXKey: make([]byte, 32)}}
			router.GET("/v1/protected", func(c *gin.Context) {
				if !h.nativeRoleGuard(c) {
					return
				}
				next, _ := authz.FromContext(c.Request.Context())
				if next.Has("coop_manager") != tc.manager {
					t.Error("wrong effective permissions")
				}
				if next.ActorRef != p.ActorRef || next.OrgRef != p.OrgRef || len(next.Roles) != 2 {
					t.Error("authentication changed")
				}
				c.Status(200)
			})
			req := httptest.NewRequest(http.MethodGet, "/v1/protected", nil).WithContext(authz.WithPrincipal(context.Background(), p))
			req.Header.Set(nativeRoleHeader, tc.role)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			if response.Code != tc.status {
				t.Fatalf("got %d", response.Code)
			}
		})
	}
}
