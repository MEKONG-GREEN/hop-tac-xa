package assembly

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/httpx"
	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
)

// FN-05/BR-03, TECH-01: máy chủ chặn mọi request ghi của tài khoản chỉ là Thành viên HTX; route trong
// htxroles.MemberWriteRoutes và tài khoản kiêm Đại diện HTX (BR-02) đi qua.
func TestMemberWriteGuard(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if roles := c.GetHeader("X-Test-Roles"); roles != "" {
			var rs []string
			_ = json.Unmarshal([]byte(roles), &rs)
			c.Request = c.Request.WithContext(authz.WithPrincipal(c.Request.Context(), authz.Principal{Roles: rs}))
		}
		c.Next()
	})
	r.Use(memberWriteGuard([]string{"POST /v1/seasons/:ref/journal"}))
	ok := func(c *gin.Context) { c.Status(http.StatusNoContent) }
	r.POST("/v1/households", ok)
	r.DELETE("/v1/households/:ref", ok)
	r.GET("/v1/households/:ref", ok)
	r.POST("/v1/seasons/:ref/journal", ok)

	for _, c := range []struct {
		name, method, path, roles string
		want                      int
	}{
		{"Thành viên HTX tạo → 403", http.MethodPost, "/v1/households", `["htx_member"]`, http.StatusForbidden},
		{"Thành viên HTX xóa → 403", http.MethodDelete, "/v1/households/x", `["htx_member","plot_viewer"]`, http.StatusForbidden},
		{"Thành viên HTX xem → qua", http.MethodGet, "/v1/households/x", `["htx_member"]`, http.StatusNoContent},
		{"kiêm Đại diện HTX → quyền Đại diện HTX (BR-02)", http.MethodPost, "/v1/households", `["htx_member","coop_manager"]`, http.StatusNoContent},
		{"htx_member + platform_admin → qua", http.MethodPost, "/v1/households", `["htx_member","platform_admin"]`, http.StatusNoContent},
		{"Đại diện HTX → qua", http.MethodPost, "/v1/households", `["coop_manager"]`, http.StatusNoContent},
		{"Admin → qua", http.MethodPost, "/v1/households", `["platform_admin"]`, http.StatusNoContent},
		{"không Principal → qua (xác thực đã chặn trước)", http.MethodPost, "/v1/households", "", http.StatusNoContent},
		{"route trong member_write_routes → qua", http.MethodPost, "/v1/seasons/abc/journal", `["htx_member"]`, http.StatusNoContent},
		{"không có route → 404 như cũ", http.MethodPost, "/v1/khong-co", `["htx_member"]`, http.StatusNotFound},
	} {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(c.method, c.path, nil)
			if c.roles != "" {
				req.Header.Set("X-Test-Roles", c.roles)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != c.want {
				t.Fatalf("%s %s = %d, cần %d", c.method, c.path, w.Code, c.want)
			}
			if c.want == http.StatusForbidden {
				var body struct {
					Code string `json:"code"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.Code != "forbidden" {
					t.Fatalf("thân 403 phải có code forbidden: %s", w.Body.String())
				}
			}
		})
	}
}

type selectedHTXRoleModule struct{}

func (selectedHTXRoleModule) RegisterApp(*gin.RouterGroup)   {}
func (selectedHTXRoleModule) RegisterAdmin(*gin.RouterGroup) {}
func (selectedHTXRoleModule) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		p, _ := authz.FromContext(c.Request.Context())
		c.Request = c.Request.WithContext(authz.WithPrincipal(c.Request.Context(), p.WithRoleCeiling(c.GetHeader("X-Test-Working-Role"))))
		c.Next()
	}
}

func TestMemberWriteGuardRechecksSelectedDualRoleBeforeHandler(t *testing.T) {
	router := gin.New()
	authenticate := func(c *gin.Context) {
		c.Request = c.Request.WithContext(authz.WithPrincipal(c.Request.Context(), authz.Principal{Roles: []string{"coop_manager", "htx_member"}}))
		c.Next()
	}
	router.Use(chain(Deps{Modules: []httpx.Module{selectedHTXRoleModule{}}}, authenticate)...)
	// PUT intentionally skips POST's idempotency store. This test proves the
	// global guard, independently of an individual handler's role checks.
	router.PUT("/v1/non-member-write", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	for _, test := range []struct {
		role string
		want int
	}{{"htx_member", http.StatusForbidden}, {"coop_manager", http.StatusNoContent}} {
		req := httptest.NewRequest(http.MethodPut, "/v1/non-member-write", nil)
		req.Header.Set("X-Test-Working-Role", test.role)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		if response.Code != test.want {
			t.Fatalf("selected %s: %d", test.role, response.Code)
		}
	}
}
