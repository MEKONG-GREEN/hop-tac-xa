package client

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
)

func TestForwardWorkingRoleKeepsSelectedMemberForOwnerProofs(t *testing.T) {
	p := authz.Principal{Roles: []string{"coop_manager", "htx_member"}}
	req := httptest.NewRequest("GET", "http://vault.example/v1/proof", nil)
	forwardWorkingRole(authz.WithPrincipal(context.Background(), p.WithRoleCeiling("htx_member")), req)
	if req.Header.Get("X-HTX-Role") != "htx_member" {
		t.Fatal("selected Member lost across Vault boundary")
	}
	if len(p.Roles) != 2 {
		t.Fatal("signed roles modified")
	}
	other := httptest.NewRequest("GET", "http://vault.example/v1/proof", nil)
	forwardWorkingRole(authz.WithPrincipal(context.Background(), p), other)
	if other.Header.Get("X-HTX-Role") != "" {
		t.Fatal("dual-role defaulted to Representative")
	}
}
