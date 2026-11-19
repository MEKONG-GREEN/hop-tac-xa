package assembly

import (
	"context"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts"
	accport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
)

func TestCalendarDirectoryTenantCoopRoleAndPagination(t *testing.T) {
	ctx := context.Background()
	dir := accounts.NewMemoryDirectory()
	tid, coop := ids.New(), ids.New()
	create := func(name string, tenant, org uuid.UUID, roles ...string) uuid.UUID {
		id, err := dir.CreateUser(ctx, accport.NewUser{Username: name, TenantID: tenant, OrgRef: org})
		if err != nil {
			t.Fatal(err)
		}
		if err = dir.AddRealmRoles(ctx, id, roles); err != nil {
			t.Fatal(err)
		}
		return id
	}
	member := create("a-member", tid, coop, htxroles.Member)
	both := create("b-both", tid, coop, htxroles.Member, htxroles.Representative)
	disabled := create("c-disabled", tid, coop, htxroles.Member)
	if err := dir.SetEnabled(ctx, disabled, false); err != nil {
		t.Fatal(err)
	}
	foreign := create("d-foreign", ids.New(), coop, htxroles.Member)
	other := create("e-other", tid, ids.New(), htxroles.Member)
	m := CalendarMembers{Dir: dir}
	got, err := m.Active(ctx, tid, coop, []uuid.UUID{member, both, disabled, foreign, other, ids.New()})
	if err != nil || !slices.Equal(got, []uuid.UUID{member}) {
		t.Fatalf("active=%v err=%v", got, err)
	}
	page, err := m.Page(ctx, tid, coop, 0, 1)
	if err != nil || !slices.Equal(page.Refs, []uuid.UUID{member}) || page.NextOffset != 1 {
		t.Fatalf("first page=%+v %v", page, err)
	}
	page, err = m.Page(ctx, tid, coop, 1, 1)
	if err != nil || len(page.Refs) != 0 || page.NextOffset != 2 {
		t.Fatalf("filtered page must retain cursor: %+v %v", page, err)
	}
	if _, err = (CalendarMembers{}).Active(ctx, tid, coop, []uuid.UUID{member}); err == nil {
		t.Fatal("missing directory accepted")
	}
}
