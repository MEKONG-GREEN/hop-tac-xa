package client

import (
	"context"
	"net/http"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
)

// Forward the current authorization subset, never an invented realm role.
// The receiving service independently validates it against the original JWT.
func forwardWorkingRole(ctx context.Context, request *http.Request) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return
	}
	var roles []string
	for _, role := range []string{htxroles.Admin, htxroles.Representative, htxroles.Member} {
		if p.Has(role) {
			roles = append(roles, role)
		}
	}
	if len(roles) == 1 {
		request.Header.Set(htxroles.WorkingRoleHeader, roles[0])
	}
}
