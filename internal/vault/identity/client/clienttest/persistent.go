package clienttest

import (
	"bytes"
	"testing"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/clock"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/vault/identity"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/vault/identity/client"
)

// NewPersistent dựng client kiểm thử dùng Vault thật với DB do fixture cấp.
// Khóa cố định CHỈ cho test. Caller vẫn chỉ import client/, không vượt R-04 để
// dựng trực tiếp nội bộ kho. Quyền, mã hóa, nhật ký và receipt đều là mã thật.
func NewPersistent(t *testing.T, database tenant.Beginner) client.Client {
	t.Helper()
	kms, err := identity.NewLocalKMS("clienttest-only", bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatal(err)
	}
	module, err := identity.New(identity.Config{DB: database, Clock: clock.System{}, KMS: kms, BlindKey: bytes.Repeat([]byte{0x17}, 32)})
	if err != nil {
		t.Fatal(err)
	}
	return module.Client()
}
