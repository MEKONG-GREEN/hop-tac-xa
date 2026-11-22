package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/httpx"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
)

const nativeRoleHeader = "X-HTX-Role"

// nativeRoleGuard chooses an authorization subset of signed IAM roles. It
// never changes realm claims, actor, tenant, organization or authentication.
// The selection is carried per request so separate browser tabs can work independently.
func (h Handler) nativeRoleGuard(c *gin.Context) bool {
	if len(h.Svc.NativeHTXKey) == 0 {
		return true
	}
	c.Header("Cache-Control", "no-store")
	p, ok := authz.FromContext(c.Request.Context())
	if !ok {
		httpx.Fail(c, problem.Unauthenticated)
		return false
	}
	roles := make([]string, 0, 2)
	for _, role := range []string{htxroles.Admin, htxroles.Representative, htxroles.Member} {
		if p.Has(role) {
			roles = append(roles, role)
		}
	}
	if !domain.NativeAccountRoles(roles) || (roles[0] != htxroles.Admin && p.OrgRef == uuid.Nil) {
		httpx.Fail(c, problem.Forbidden)
		return false
	}
	selected := strings.TrimSpace(c.GetHeader(nativeRoleHeader))
	if selected == "" && len(roles) == 1 {
		selected = roles[0]
	}
	if selected == "" {
		if c.Request.Method == http.MethodPost && c.FullPath() == "/v1/me/data-consent" {
			return true
		}
		// Identity/bootstrap responses confer no cooperative business-data access.
		if c.Request.Method == http.MethodGet {
			switch c.FullPath() {
			case "/v1/me", "/v1/me/contexts", "/v1/me/data-consent":
				return true
			}
		}
		httpx.Fail(c, problem.New(http.StatusConflict, "accounts.working_role_required", "Chọn vai trò làm việc"))
		return false
	}
	valid := false
	for _, role := range roles {
		if role == selected {
			valid = true
		}
	}
	if !valid {
		httpx.Fail(c, problem.Forbidden)
		return false
	}
	ctx := idempotency.WithAuthorizationScope(c.Request.Context(), []byte("htx-working-role/v1/"+selected))
	c.Request = c.Request.WithContext(authz.WithPrincipal(ctx, p.WithRoleCeiling(selected)))
	return true
}
