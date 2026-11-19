package app_test

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/vault/identity/client"
	"github.com/MEKONG-GREEN/hop-tac-xa/migrations"
)

func TestAH283RegistryContactDurableCommand(t *testing.T) {
	s, env := newService(t)
	tid := ids.New()
	ctx := as(tid, true, client.RoleRegistrar)
	contact := client.Identity{Kind: client.SubjectPerson, FullName: "Người đại diện ban đầu", Phone: "0907248340", AreaCode: "X-AH283"}
	created, err := s.Create(ctx, client.PurposeRegistryAdmin, contact)
	if err != nil {
		t.Fatal(err)
	}
	first, second := ids.New(), ids.New()
	old := client.Identity{FullName: "Người đại diện lần một", Phone: "0907248340"}
	next := client.Identity{FullName: "Người đại diện mới nhất", Phone: "0907248341"}
	for _, command := range []struct {
		ref uuid.UUID
		id  client.Identity
	}{{first, old}, {second, next}, {first, old}} {
		revision, err := s.AmendRegistryContact(ctx, client.PurposeRegistryAdmin, created.Ref, command.ref, command.id)
		if err != nil || revision != command.ref {
			t.Fatalf("command: %v %v", revision, err)
		}
	}
	got, err := s.Resolve(ctx, client.PurposeRegistryAdmin, created.Ref)
	if err != nil || got.FullName != next.FullName || got.Phone != "84907248341" {
		t.Fatalf("retry cũ đã ghi đè liên hệ mới: %v", err)
	}
	if _, err := s.AmendRegistryContact(ctx, client.PurposeRegistryAdmin, created.Ref, first, next); code(err) != "idempotency_key_reused" {
		t.Fatalf("command cũ khác nội dung: %v", err)
	}
	var revisions, amendments int
	if err := env.AdminPool(t, migrations.Vault).QueryRow(context.Background(), `SELECT
(SELECT count(*) FROM vault.identity_revision WHERE tenant_id=$1 AND subject_ref=$2),
(SELECT count(*) FROM vault.access_log WHERE tenant_id=$1 AND subject_ref=$2 AND op='amend' AND outcome='ok')`, tid, created.Ref).Scan(&revisions, &amendments); err != nil {
		t.Fatal(err)
	}
	if revisions != 2 || amendments != 2 {
		t.Fatalf("chỉ áp hai lần, nhận revision=%d amend=%d", revisions, amendments)
	}
	other, err := s.Create(ctx, client.PurposeRegistryAdmin, client.Identity{Kind: client.SubjectPerson, Phone: "0907248342", AreaCode: "X-AH283"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AmendRegistryContact(ctx, client.PurposeRegistryAdmin, other.Ref, first, old); code(err) != "idempotency_key_reused" {
		t.Fatalf("command không được chuyển sang chủ thể khác: %v", err)
	}
}

func TestAH283RegistryContactConcurrentCommand(t *testing.T) {
	s, env := newService(t)
	tid := ids.New()
	ctx := as(tid, true, client.RoleRegistrar)
	created, err := s.Create(ctx, client.PurposeRegistryAdmin, client.Identity{Kind: client.SubjectPerson, Phone: "0907248343", AreaCode: "X-AH283"})
	if err != nil {
		t.Fatal(err)
	}
	const workers = 6
	command := ids.New()
	var done sync.WaitGroup
	results := make(chan error, workers)
	for range workers {
		done.Go(func() {
			_, err := s.AmendRegistryContact(ctx, client.PurposeRegistryAdmin, created.Ref, command, client.Identity{FullName: "Đại diện chung", Phone: "0907248343"})
			results <- err
		})
	}
	done.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	var n int
	if err := env.AdminPool(t, migrations.Vault).QueryRow(context.Background(), `SELECT count(*) FROM vault.access_log
WHERE tenant_id=$1 AND subject_ref=$2 AND op='amend' AND outcome='ok'`, tid, created.Ref).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("cùng command bị áp %d lần", n)
	}
}
