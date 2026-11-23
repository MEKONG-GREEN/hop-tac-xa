package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
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

// FN-TK-01/EX-02, EX-03 (AH-222): Admin ngừng vai Thành viên HTX → token làm mới chỉ còn Đại diện HTX; thao tác kế tiếp
// còn gửi X-HTX-Role htx_member nhận 409 accounts.working_role_revoked để client đưa về màn Chọn vai trò. Tài khoản
// không còn vai nào nhận 403 accounts.no_active_role kèm câu EX-02.
func TestNativeRoleRevokedAndNoActiveRole(t *testing.T) {
	for _, tc := range []struct {
		name   string
		roles  []string
		header string
		status int
		code   string
	}{
		{"member removed", []string{"coop_manager"}, "htx_member", 409, "accounts.working_role_revoked"},
		{"representative removed", []string{"htx_member"}, "coop_manager", 409, "accounts.working_role_revoked"},
		{"forged admin", []string{"coop_manager"}, "platform_admin", 403, "forbidden"},
		{"no role", []string{"offline_access"}, "htx_member", 403, "accounts.no_active_role"},
		{"no role, no header", nil, "", 403, "accounts.no_active_role"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := authz.Principal{ActorRef: uuid.New(), TenantID: uuid.New(), OrgRef: uuid.New(), Roles: tc.roles}
			router := gin.New()
			h := Handler{Svc: &app.Service{NativeHTXKey: make([]byte, 32)}}
			router.GET("/v1/protected", func(c *gin.Context) {
				if h.nativeRoleGuard(c) {
					c.Status(200)
				}
			})
			req := httptest.NewRequest(http.MethodGet, "/v1/protected", nil).WithContext(authz.WithPrincipal(context.Background(), p))
			req.Header.Set(nativeRoleHeader, tc.header)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			if response.Code != tc.status || !strings.Contains(response.Body.String(), `"code":"`+tc.code+`"`) {
				t.Fatalf("got %d %s", response.Code, response.Body.String())
			}
		})
	}
}
