package app

import (
	"context"
	"slices"
	"testing"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/keycloak"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/port"
	"github.com/google/uuid"
)

func TestBootstrapNativeFN07(t *testing.T) {
	ctx := context.Background()
	dir := keycloak.NewMemory()
	in := BootstrapAdminInput{NativeHTX: true, TenantID: uuid.New(), Username: "native-first-admin", Phone: "0912345678", Email: "first-admin@example.vn", FirstName: "Admin", LastName: "HTX"}
	id, password, err := BootstrapAdmin(ctx, dir, in, nil)
	if err != nil {
		t.Fatal(err)
	}
	u, err := dir.GetUser(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(password) != 8 || u.OrgRef != uuid.Nil || u.Phone != in.Phone || !u.EmailVerified || !slices.Contains(u.RequiredActions, port.ActionUpdatePassword) {
		t.Fatal("native first Admin contract")
	}
	in.Username = "another-admin"
	if _, _, err := BootstrapAdmin(ctx, dir, in, nil); err == nil {
		t.Fatal("duplicate phone")
	}
	in.Phone = "0912345679"
	in.OrgRef = uuid.New()
	if _, _, err := BootstrapAdmin(ctx, dir, in, nil); err == nil {
		t.Fatal("Admin must not have a cooperative")
	}
}
