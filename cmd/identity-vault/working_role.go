package main

import (
	"net/http"
	"strings"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/httpx"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
	"github.com/gin-gonic/gin"
)

// HTX selections only narrow authenticated permissions; callback proofs remain authoritative.
func vaultWorkingRole(enabled bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !enabled {
			c.Next()
			return
		}
		p, ok := authz.FromContext(c.Request.Context())
		if !ok {
			httpx.Fail(c, problem.Unauthenticated)
			return
		}
		var roles []string
		for _, role := range []string{htxroles.Admin, htxroles.Representative, htxroles.Member} {
			if p.Has(role) {
				roles = append(roles, role)
			}
		}
		if len(roles) == 0 {
			httpx.Fail(c, problem.New(http.StatusForbidden, htxroles.CodeNoActiveRole, htxroles.MsgNoActiveRole))
			return
		}
		if !htxroles.ValidAccountRoles(roles) {
			httpx.Fail(c, problem.Forbidden)
			return
		}
		selected := strings.TrimSpace(c.GetHeader(htxroles.WorkingRoleHeader))
		if selected == "" && len(roles) == 1 {
			selected = roles[0]
		}
		if selected == "" {
			httpx.Fail(c, problem.New(http.StatusConflict, "accounts.working_role_required", "Chọn vai trò làm việc"))
			return
		}
		valid := false
		for _, r := range roles {
			if selected == r {
				valid = true
			}
		}
		if !valid && htxroles.Revocable(selected) {
			// FN-TK-01/EX-03: vai đã bị ngừng sau khi token được làm mới — client chọn lại vai.
			httpx.Fail(c, problem.New(http.StatusConflict, htxroles.CodeWorkingRoleRevoked, htxroles.MsgWorkingRoleRevoked))
			return
		}
		if !valid {
			httpx.Fail(c, problem.Forbidden)
			return
		}
		c.Request = c.Request.WithContext(authz.WithPrincipal(c.Request.Context(), p.WithRoleCeiling(selected)))
		c.Next()
	}
}
