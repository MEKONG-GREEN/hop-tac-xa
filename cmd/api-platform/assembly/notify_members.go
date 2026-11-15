package assembly

import (
	"context"
	"errors"
	"slices"

	"github.com/google/uuid"

	accport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/port"
	partyport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/party/port"
)

// NotifyMembers hiện thực notify/port.MemberDirectory trên pf/accounts (Keycloak): Thành viên HTX là tài khoản
// đang bật, cùng tenant, org_ref bằng HTX và có vai coop_manager (AH-22).
type NotifyMembers struct{ Dir accport.Directory }

// IsActiveMember thực hiện notify/port.MemberDirectory.
func (m NotifyMembers) IsActiveMember(ctx context.Context, tid, coop, actor uuid.UUID) (bool, error) {
	u, err := m.Dir.GetUser(ctx, actor)
	if errors.Is(err, accport.ErrUserNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !u.Enabled || u.TenantID != tid || u.OrgRef != coop {
		return false, nil
	}
	roles, err := m.Dir.RealmRoles(ctx, actor)
	if err != nil {
		return false, err
	}
	return slices.Contains(roles, partyport.RoleCoopManager), nil
}
