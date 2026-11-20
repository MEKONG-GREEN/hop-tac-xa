package app_test

import (
	"context"
	"testing"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/vault/identity/client"
	"github.com/MEKONG-GREEN/hop-tac-xa/migrations"
	"github.com/google/uuid"
)

func TestAH283HTXContactErasureIsPermanentAndScoped(t *testing.T) {
	s, env := newService(t)
	tid, org, other, command := ids.New(), ids.New(), ids.New(), ids.New()
	admin := htxCaller(tid, uuid.Nil, authz.RolePlatformAdmin)
	identityCaller := as(tid, true, client.RoleRegistrar)
	input := client.HTXContact{FullName: "Người đại diện", Phone: "0907248399"}
	identity, err := s.Create(identityCaller, client.PurposeRegistryAdmin, client.Identity{Kind: client.SubjectHousehold, FullName: "Chủ hộ độc lập", Phone: input.Phone, AreaCode: "X-AH283"})
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range []uuid.UUID{org, other} {
		if err := s.WriteHTXContact(admin, client.PurposeRegistryAdmin, ref, ids.New(), input); err != nil {
			t.Fatal(err)
		}
	}
	for _, role := range []string{"coop_manager", "htx_member"} {
		caller := htxCaller(tid, org, role)
		if err := s.ValidateHTXContactErasure(caller, client.PurposeRegistryAdmin, org); code(err) != "forbidden" {
			t.Fatalf("%s validate: %v", role, err)
		}
		if err := s.EraseHTXContact(caller, client.PurposeRegistryAdmin, org, command); code(err) != "forbidden" {
			t.Fatalf("%s erase: %v", role, err)
		}
	}
	if err := s.ValidateHTXContactErasure(admin, client.PurposeRegistryAdmin, org); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := s.EraseHTXContact(admin, client.PurposeRegistryAdmin, org, command); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.ReadHTXContact(admin, client.PurposeRegistryAdmin, org, false); code(err) != "vault.erased" {
		t.Fatalf("deleted read: %v", err)
	}
	if err := s.WriteHTXContact(admin, client.PurposeRegistryAdmin, org, ids.New(), input); code(err) != "vault.erased" {
		t.Fatalf("resurrection: %v", err)
	}
	if err := s.EraseHTXContact(admin, client.PurposeRegistryAdmin, other, command); code(err) != "idempotency_key_reused" {
		t.Fatalf("command binding: %v", err)
	}
	if got, err := s.ReadHTXContact(admin, client.PurposeRegistryAdmin, other, false); err != nil || got.FullName != input.FullName {
		t.Fatalf("other contact: %v", err)
	}
	if got, err := s.Resolve(identityCaller, client.PurposeRegistryAdmin, identity.Ref); err != nil || got.FullName != "Chủ hộ độc lập" {
		t.Fatalf("household unchanged: %v", err)
	}
	var pointers, commands, erased, audits int
	err = env.AdminPool(t, migrations.Vault).QueryRow(context.Background(), `SELECT
 (SELECT count(*) FROM vault.htx_contact WHERE tenant_id=$1 AND cooperative_ref=$2),
 (SELECT count(*) FROM vault.htx_contact_command WHERE tenant_id=$1 AND cooperative_ref=$2),
 (SELECT count(*) FROM vault.htx_contact_erasure WHERE tenant_id=$1 AND cooperative_ref=$2),
 (SELECT count(*) FROM vault.access_log WHERE tenant_id=$1 AND subject_ref=$2 AND op='erase' AND outcome='ok')`, tid, org).Scan(&pointers, &commands, &erased, &audits)
	if err != nil {
		t.Fatal(err)
	}
	if pointers != 0 || commands != 0 || erased != 1 || audits != 1 {
		t.Fatalf("erasure state: pointer=%d encrypted=%d tombstone=%d audit=%d", pointers, commands, erased, audits)
	}
}
