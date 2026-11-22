package app

import (
	"context"
	"strings"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/vault/identity/client"
	"github.com/google/uuid"
)

func householdIdentityCaller(ctx context.Context, purpose client.Purpose) (authz.Principal, error) {
	p, err := authz.Require(ctx, htxroles.With("FN-04", htxroles.Read)...)
	if err != nil {
		return authz.Principal{}, err
	}
	if purpose == "" {
		return authz.Principal{}, client.ProblemPurposeRequired
	}
	if purpose != client.PurposeContact {
		return authz.Principal{}, problem.Validation.WithField("purpose", "Định danh hộ HTX chỉ dùng mục đích liên hệ hộ")
	}
	if p.Has(htxroles.Admin) && !p.MFA {
		return authz.Principal{}, problem.MFARequired
	}
	if !p.Has(htxroles.Admin) && p.OrgRef == uuid.Nil {
		return authz.Principal{}, problem.NodeScopeRequired
	}
	return p, nil
}

func (s *Service) householdReadScope(ctx context.Context, p authz.Principal, ref uuid.UUID) (client.HouseholdIdentityScope, error) {
	if s.HouseholdScopes == nil {
		return client.HouseholdIdentityScope{}, client.ProblemHouseholdScopeUnavailable
	}
	scope, err := s.HouseholdScopes.HouseholdIdentityScope(ctx, ref)
	if err != nil {
		return client.HouseholdIdentityScope{}, err
	}
	shared := !p.Has(htxroles.Admin) && scope.CooperativeRef != p.OrgRef
	if ref == uuid.Nil || scope.Ref != ref || (shared &&
		(!p.Has(htxroles.Representative) || !scope.Shared || scope.ContactAccess != "masked" || scope.CanWrite)) {
		return client.HouseholdIdentityScope{}, client.ProblemNotFound
	}
	if scope.ContactAccess != "full" && scope.ContactAccess != "masked" {
		return client.HouseholdIdentityScope{}, client.ProblemHouseholdScopeUnavailable
	}
	// The owner may further restrict access. It can never expand a Member's
	// token into a full-identity capability.
	if htxroles.IsMemberOnly(p.Has) {
		scope.ContactAccess = "masked"
	}
	return scope, nil
}

// ReadHouseholdIdentity exposes FN-04 contact fields after fresh owner proof.
// It neither grants a Vault role nor bypasses generic Resolve authorization.
// Network proof, KMS and audit each run outside one another's DB transactions.
func (s *Service) ReadHouseholdIdentity(ctx context.Context, purpose client.Purpose, ref uuid.UUID) (client.Identity, error) {
	p, err := householdIdentityCaller(ctx, purpose)
	if err != nil {
		return client.Identity{}, err
	}
	if err := s.sensitive(ctx, p, "household.read", purpose, false, true); err != nil {
		return client.Identity{}, err
	}
	scope, err := s.householdReadScope(ctx, p, ref)
	if err != nil {
		return client.Identity{}, err
	}
	out, err := s.resolveAuthorized(ctx, p, purpose, ref, client.SubjectHousehold)
	if err != nil {
		return client.Identity{}, err
	}
	// Recheck before releasing plaintext if membership changed during KMS I/O.
	latest, err := s.householdReadScope(ctx, p, ref)
	if err != nil {
		return client.Identity{}, err
	}
	if latest.CooperativeRef != scope.CooperativeRef {
		return client.Identity{}, client.ProblemNotFound
	}
	if len(out.Phone) == 11 && strings.HasPrefix(out.Phone, "84") {
		out.Phone = "0" + strings.TrimPrefix(out.Phone, "84")
	}
	if scope.ContactAccess == "masked" || latest.ContactAccess == "masked" {
		out.Phone = householdMask(out.Phone, 4, 3)
		out.NationalID = householdMask(out.NationalID, 3, 3)
	}
	return out, nil
}

func householdMask(value string, prefix, suffix int) string {
	runes := []rune(value)
	if len(runes) <= prefix+suffix {
		return strings.Repeat("*", len(runes))
	}
	return string(runes[:prefix]) + strings.Repeat("*", len(runes)-prefix-suffix) + string(runes[len(runes)-suffix:])
}
