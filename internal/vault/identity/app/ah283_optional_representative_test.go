package app_test

import (
	"testing"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/vault/identity/client"
)

func TestAH283RegistryContactWithoutRepresentativeName(t *testing.T) {
	s, _ := newService(t)
	ctx := as(ids.New(), true, client.RoleRegistrar)
	contact := client.Identity{Kind: client.SubjectPerson, Phone: "0907248310", AreaCode: "X-AH283"}
	created, err := s.Create(ctx, client.PurposeRegistryAdmin, contact)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Resolve(ctx, client.PurposeRegistryAdmin, created.Ref)
	if err != nil || got.FullName != "" || got.Phone != "84907248310" {
		t.Fatalf("không bịa tên người đại diện: %+v %v", got, err)
	}
	contact.FullName = "Nguyễn Văn Đại Diện"
	if err := s.Amend(ctx, client.PurposeRegistryAdmin, created.Ref, contact); err != nil {
		t.Fatal(err)
	}
	contact.FullName = ""
	if err := s.Amend(ctx, client.PurposeRegistryAdmin, created.Ref, contact); err != nil {
		t.Fatal(err)
	}
	for _, purpose := range []client.Purpose{client.PurposeHRMProfile, client.PurposeContact} {
		if _, err := s.Create(ctx, purpose, contact); code(err) != "validation_failed" {
			t.Fatalf("không nới quy tắc tên cho %s: %v", purpose, err)
		}
	}
	contact.Kind = client.SubjectHousehold
	if _, err := s.Create(ctx, client.PurposeRegistryAdmin, contact); code(err) != "validation_failed" {
		t.Fatalf("họ tên chủ hộ vẫn bắt buộc: %v", err)
	}
	household := sample("0907248311")
	hh, err := s.Create(ctx, client.PurposeRegistryAdmin, household)
	if err != nil {
		t.Fatal(err)
	}
	household.FullName = ""
	if err := s.Amend(ctx, client.PurposeRegistryAdmin, hh.Ref, household); code(err) != "validation_failed" {
		t.Fatalf("không xóa được họ tên chủ hộ: %v", err)
	}
	contact.Kind, contact.Phone = client.SubjectPerson, ""
	if _, err := s.Create(ctx, client.PurposeRegistryAdmin, contact); code(err) != "validation_failed" {
		t.Fatalf("liên hệ không tên phải có SĐT: %v", err)
	}
}
