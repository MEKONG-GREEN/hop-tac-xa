package app_test

import (
	"context"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/vault/identity/client"
	"github.com/MEKONG-GREEN/hop-tac-xa/migrations"
	"github.com/google/uuid"
	"sync"
	"testing"
)

func htxCaller(tid, org uuid.UUID, roles ...string) context.Context {
	return authz.WithPrincipal(context.Background(), authz.Principal{TenantID: tid, ActorRef: ids.New(), OrgRef: org, Roles: roles, MFA: true})
}

func TestAH283HTXContactIndependentAndScoped(t *testing.T) {
	s, env := newService(t)
	tid, first, other := ids.New(), ids.New(), ids.New()
	admin := htxCaller(tid, uuid.Nil, authz.RolePlatformAdmin)
	old, next := ids.New(), ids.New()
	input := client.HTXContact{Phone: "0907248360"}
	identityCaller := as(tid, true, client.RoleRegistrar)
	legacyIdentity, err := s.Create(identityCaller, client.PurposeRegistryAdmin, client.Identity{
		Kind: client.SubjectHousehold, FullName: "Chủ hộ cũ", Phone: input.Phone, AreaCode: "X-AH283"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.WriteHTXContact(admin, client.PurposeRegistryAdmin, first, old, input); err != nil {
		t.Fatal(err)
	}
	otherContact := client.HTXContact{FullName: "Đại diện HTX khác", Phone: input.Phone}
	if err := s.WriteHTXContact(admin, client.PurposeRegistryAdmin, other, ids.New(), otherContact); err != nil {
		t.Fatal(err)
	}
	input.FullName = "Đại diện HTX đầu"
	if err := s.WriteHTXContact(htxCaller(tid, first, "coop_manager"), client.PurposeRegistryAdmin, first, next, input); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteHTXContact(admin, client.PurposeRegistryAdmin, first, old, client.HTXContact{Phone: input.Phone}); err != nil {
		t.Fatal(err)
	}
	current, err := s.ReadHTXContact(htxCaller(tid, first, "coop_manager"), client.PurposeRegistryAdmin, first, false)
	if err != nil || current.FullName != input.FullName || current.Phone != "84907248360" {
		t.Fatalf("retry cũ/đọc theo vai thật: %v", err)
	}
	current, err = s.ReadHTXContact(admin, client.PurposeRegistryAdmin, other, false)
	if err != nil || current.FullName != otherContact.FullName {
		t.Fatalf("trùng SĐT không liên kết hai HTX: %v", err)
	}
	legacy, err := s.Resolve(identityCaller, client.PurposeRegistryAdmin, legacyIdentity.Ref)
	if err != nil || legacy.FullName != "Chủ hộ cũ" || legacy.Phone != "84907248360" {
		t.Fatalf("liên hệ HTX không sửa định danh hộ cùng SĐT: %v", err)
	}
	found, err := s.Lookup(identityCaller, client.PurposeDedup, input.Phone)
	if err != nil || found != legacyIdentity.Ref {
		t.Fatalf("liên hệ HTX không thay chỉ mục định danh người/hộ: %v", err)
	}
	if _, err := s.Resolve(admin, client.PurposeRegistryAdmin, legacyIdentity.Ref); code(err) != "forbidden" {
		t.Fatalf("vai HTX không được mở định danh người/hộ: %v", err)
	}
	member := htxCaller(tid, first, "htx_member")
	masked, err := s.ReadHTXContact(member, client.PurposeContact, first, true)
	if err != nil || !masked.Masked || masked.Phone == input.Phone || masked.Phone == "84907248360" {
		t.Fatalf("Member chỉ nhận bản che: %v", err)
	}
	if _, err := s.ReadHTXContact(member, client.PurposeRegistryAdmin, first, false); code(err) != "forbidden" {
		t.Fatalf("Member xin rõ: %v", err)
	}
	if err := s.WriteHTXContact(member, client.PurposeRegistryAdmin, first, ids.New(), input); code(err) != "forbidden" {
		t.Fatalf("Member ghi: %v", err)
	}
	if _, err := s.ReadHTXContact(htxCaller(tid, other, "coop_manager", client.RoleRegistrar), client.PurposeRegistryAdmin, first, false); code(err) != "vault.not_found" {
		t.Fatalf("vai kho không vượt scope HTX: %v", err)
	}
	if _, err := s.ReadHTXContact(htxCaller(ids.New(), uuid.Nil, authz.RolePlatformAdmin), client.PurposeRegistryAdmin, first, false); code(err) != "vault.not_found" {
		t.Fatalf("tenant khác: %v", err)
	}
	if err := s.WriteHTXContact(admin, client.PurposeRegistryAdmin, first, old, input); code(err) != "idempotency_key_reused" {
		t.Fatalf("đổi nội dung command: %v", err)
	}
	if err := s.WriteHTXContact(admin, client.PurposeRegistryAdmin, other, next, input); code(err) != "idempotency_key_reused" {
		t.Fatalf("đổi HTX của command: %v", err)
	}
	var count int
	if err := env.AdminPool(t, migrations.Vault).QueryRow(context.Background(), `SELECT count(*) FROM vault.htx_contact_command WHERE tenant_id=$1`, tid).Scan(&count); err != nil || count != 3 {
		t.Fatalf("receipt bền vững: count=%d err=%v", count, err)
	}
}

func TestAH283HTXContactConcurrentCommand(t *testing.T) {
	s, env := newService(t)
	tid, org, command := ids.New(), ids.New(), ids.New()
	ctx := htxCaller(tid, org, "coop_manager")
	input := client.HTXContact{Phone: "0907248361"}
	var group sync.WaitGroup
	failures := make(chan error, 6)
	for range 6 {
		group.Go(func() { failures <- s.WriteHTXContact(ctx, client.PurposeRegistryAdmin, org, command, input) })
	}
	group.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	var commands, audits int
	if err := env.AdminPool(t, migrations.Vault).QueryRow(context.Background(), `SELECT
 (SELECT count(*) FROM vault.htx_contact_command WHERE tenant_id=$1 AND cooperative_ref=$2),
 (SELECT count(*) FROM vault.access_log WHERE tenant_id=$1 AND subject_ref=$2 AND op='amend' AND outcome='ok')`, tid, org).Scan(&commands, &audits); err != nil {
		t.Fatal(err)
	}
	if commands != 1 || audits != 1 {
		t.Fatalf("apply đúng một lần: commands=%d audits=%d", commands, audits)
	}
}
