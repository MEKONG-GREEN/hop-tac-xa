package authz_test

import (
	"slices"
	"testing"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/google/uuid"
)

func TestRoleCeilingNeverExpandsOrChangesAuthentication(t *testing.T) {
	original := authz.Principal{ActorRef: uuid.New(), TenantID: uuid.New(), OrgRef: uuid.New(), Roles: []string{"coop_manager", "htx_member"}, MFA: true}
	member := original.WithRoleCeiling("htx_member", "platform_admin")
	if member.Has("coop_manager") || member.Has("platform_admin") || !member.Has("htx_member") {
		t.Fatal("authorization union or injected role")
	}
	if !slices.Equal(member.Roles, original.Roles) || member.ActorRef != original.ActorRef || member.TenantID != original.TenantID || member.OrgRef != original.OrgRef || member.MFA != original.MFA {
		t.Fatal("authentication was changed")
	}
	if !original.Has("coop_manager") {
		t.Fatal("original principal mutated")
	}
	if member.WithRoleCeiling("coop_manager").Has("coop_manager") {
		t.Fatal("ceiling expanded")
	}
	if len(original.WithRoleCeiling().EffectiveRoles()) != 0 {
		t.Fatal("empty ceiling fell back")
	}
	roles := member.EffectiveRoles()
	roles[0] = "coop_manager"
	if member.Has("coop_manager") {
		t.Fatal("mutable effective role slice")
	}
}
