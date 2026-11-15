package assembly

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts"
	accport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/port"
	partyport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/party/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
)

func TestAH22_AC4_NotifyMembers(t *testing.T) {
	ctx := context.Background()
	dir := accounts.NewMemoryDirectory()
	tid, coop := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	mk := func(name string, tenant, org uuid.UUID, roles ...string) uuid.UUID {
		id, err := dir.CreateUser(ctx, accport.NewUser{Username: name, Email: name + "@example.test", FirstName: "A", LastName: "B", TenantID: tenant, OrgRef: org})
		if err != nil {
			t.Fatal(err)
		}
		if len(roles) > 0 {
			if err := dir.AddRealmRoles(ctx, id, roles); err != nil {
				t.Fatal(err)
			}
		}
		return id
	}
	member := mk("tv-ok", tid, coop, partyport.RoleCoopManager)
	disabled := mk("tv-off", tid, coop, partyport.RoleCoopManager)
	if err := dir.SetEnabled(ctx, disabled, false); err != nil {
		t.Fatal(err)
	}
	otherTenant := mk("tv-tenant", uuid.Must(uuid.NewV7()), coop, partyport.RoleCoopManager)
	otherCoop := mk("tv-coop", tid, uuid.Must(uuid.NewV7()), partyport.RoleCoopManager)
	admin := mk("admin", tid, coop, authz.RolePlatformAdmin)

	m := NotifyMembers{Dir: dir}
	for name, c := range map[string]struct {
		actor uuid.UUID
		want  bool
	}{
		"Thành viên đang bật":   {member, true},
		"tài khoản bị tắt":      {disabled, false},
		"tenant khác":           {otherTenant, false},
		"HTX khác":              {otherCoop, false},
		"chỉ có platform_admin": {admin, false},
		"không tồn tại":         {uuid.Must(uuid.NewV7()), false},
	} {
		got, err := m.IsActiveMember(ctx, tid, coop, c.actor)
		if err != nil || got != c.want {
			t.Errorf("%s: got %v, err %v; muốn %v", name, got, err, c.want)
		}
	}
}
