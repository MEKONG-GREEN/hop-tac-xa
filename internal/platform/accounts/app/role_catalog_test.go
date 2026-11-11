package app

import (
	"context"
	"errors"
	"testing"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/outbox"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
)

func TestRoleCatalogInvalidConfigurationFailsBeforeDependencies(t *testing.T) {
	s := &Service{Roles: &port.RoleCatalog{}}
	ctx := authz.WithPrincipal(context.Background(), authz.Principal{
		ActorRef: ids.New(), TenantID: ids.New(), Roles: []string{authz.RolePlatformAdmin}, MFA: true,
	})
	var p *problem.Problem
	if _, err := s.caller(ctx); !errors.As(err, &p) || p.Code != ErrDependency.Code || p.Status != ErrDependency.Status {
		t.Fatalf("invalid configured catalog did not fail closed: %v", err)
	}
	if _, err := s.HandleOrgStatus(context.Background(), nil, outbox.Envelope{}); !errors.Is(err, ErrDependency) {
		t.Fatalf("worker invalid catalog reached dependencies or fell back: %v", err)
	}
	if s.RoleCatalog().Valid() || len(s.RoleCatalog().Grantable([]string{authz.RolePlatformAdmin})) != 0 {
		t.Fatal("configured zero catalog acquired legacy grants")
	}
}
