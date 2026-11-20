package app

import (
	"bytes"
	"context"
	"testing"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/evidence/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/vault/identity/client"
	"github.com/google/uuid"
)

func TestAH99OwnedImageReceiptDoesNotGrantViewing(t *testing.T) {
	f := newFx(t)
	up := f.as(f.tid, f.orgA, "htx_member")
	file, err := f.attach(up, nil)
	if err != nil {
		t.Fatal(err)
	}
	const limit = 1 << 20
	if err := f.svc.VerifyOwnedImages(up, file.SubjectRef, []uuid.UUID{file.Ref}, limit); err != nil {
		t.Fatalf("own receipt: %v", err)
	}
	if _, err := f.svc.Open(up, file.Ref, string(client.PurposeMRVEvidence)); err == nil {
		t.Fatal("receipt verification granted viewer permissions")
	}
	for _, tc := range []struct {
		ctx     context.Context
		subject uuid.UUID
	}{{f.as(f.tid, f.orgA, "htx_member"), file.SubjectRef}, {up, ids.New()}} {
		if err := f.svc.VerifyOwnedImages(tc.ctx, tc.subject, []uuid.UUID{file.Ref}, limit); err == nil {
			t.Fatal("foreign actor/household receipt accepted")
		}
	}
	principal, _ := authz.FromContext(up)
	principal.TenantID = ids.New()
	other := tenant.WithID(authz.WithPrincipal(context.Background(), principal), principal.TenantID)
	if err := f.svc.VerifyOwnedImages(other, file.SubjectRef, []uuid.UUID{file.Ref}, limit); err == nil {
		t.Fatal("receipt leaked across tenants")
	}
	meta, blob := f.seal(up, bytes.Repeat([]byte{1}, limit+1))
	large, _, err := f.svc.Attach(up, port.AttachInput{Meta: meta, Blob: blob})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.VerifyOwnedImages(up, large.SubjectRef, []uuid.UUID{large.Ref}, limit); err == nil {
		t.Fatal("image above plaintext limit accepted")
	}
	meta, blob = f.seal(up, []byte("document"))
	meta.ContentType = "application/pdf"
	pdf, _, err := f.svc.Attach(up, port.AttachInput{Meta: meta, Blob: blob})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.VerifyOwnedImages(up, pdf.SubjectRef, []uuid.UUID{pdf.Ref}, limit); err == nil {
		t.Fatal("non-image attachment accepted")
	}
}
