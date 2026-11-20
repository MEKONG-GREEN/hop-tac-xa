package clienttest

import (
	"bytes"
	"github.com/gin-gonic/gin"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/clock"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/httpx"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/vault/identity"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/vault/identity/client"
)

// NewPersistent dựng client kiểm thử dùng Vault thật với DB do fixture cấp.
// Khóa cố định CHỈ cho test. Caller vẫn chỉ import client/, không vượt R-04 để
// dựng trực tiếp nội bộ kho. Quyền, mã hóa, nhật ký và receipt đều là mã thật.
func NewPersistent(t *testing.T, database tenant.Beginner) client.Client {
	return persistentModule(t, database, false, nil).Client()
}

// NewPersistentHTTP kiểm cả chuyển tiếp token, mục đích, receipt và MFA của
// Vault thật qua HTTP. Không tắt kiểm quyền để chiều theo fixture của caller.
func NewPersistentHTTP(t *testing.T, database tenant.Beginner, authenticate gin.HandlerFunc) client.Client {
	return NewPersistentHTTPWithHouseholdScopes(t, database, authenticate, nil)
}

// NewPersistentHTTPWithHouseholdScopes uses a real owner verifier, so tests can
// exercise both HTTP hops without replacing authorization or storing PII in PF.
func NewPersistentHTTPWithHouseholdScopes(t *testing.T, database tenant.Beginner, authenticate gin.HandlerFunc, scopes client.HouseholdScopeVerifier) client.Client {
	t.Helper()
	module := persistentModule(t, database, true, scopes)
	engine := httpx.NewEngine(telemetry.NewLogger("test", io.Discard), 1<<20)
	module.RegisterApp(engine.Group("/v1", authenticate))
	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)
	return client.NewHTTP(server.URL, server.Client())
}

func persistentModule(t *testing.T, database tenant.Beginner, requireMFA bool, scopes client.HouseholdScopeVerifier) *identity.Module {
	t.Helper()
	kms, err := identity.NewLocalKMS("clienttest-only", bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatal(err)
	}
	module, err := identity.New(identity.Config{DB: database, Clock: clock.System{}, KMS: kms, BlindKey: bytes.Repeat([]byte{0x17}, 32), RequireMFASensitive: requireMFA, HouseholdScopes: scopes})
	if err != nil {
		t.Fatal(err)
	}
	return module
}
