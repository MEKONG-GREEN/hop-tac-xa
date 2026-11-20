package client

import (
	"context"

	"github.com/google/uuid"
)

// HouseholdIdentityReader is the native FN-04 contact capability, verified by
// the owning platform on every read. It never supplies generic Vault roles.
type HouseholdIdentityReader interface {
	ReadHouseholdIdentity(context.Context, Purpose, uuid.UUID) (Identity, error)
}

func AsHouseholdIdentityReader(c Client) (HouseholdIdentityReader, bool) {
	r, ok := c.(HouseholdIdentityReader)
	return r, ok
}

func (c *httpClient) ReadHouseholdIdentity(ctx context.Context, purpose Purpose, ref uuid.UUID) (Identity, error) {
	if purpose == "" {
		return Identity{}, ProblemPurposeRequired
	}
	var out IdentityResponse
	err := c.do(ctx, call{method: "POST", path: "/v1/household-identities/" + ref.String() + "/read", purpose: purpose,
		in: PurposeRequest{Purpose: string(purpose)}, out: &out})
	if err != nil {
		return Identity{}, err
	}
	return Identity{Kind: SubjectKind(out.Kind), FullName: out.FullName, Phone: out.Phone, NationalID: out.NationalID, Address: out.Address, AreaCode: out.AreaCode}, nil
}

func (l local) ReadHouseholdIdentity(ctx context.Context, purpose Purpose, ref uuid.UUID) (Identity, error) {
	if purpose == "" {
		return Identity{}, ProblemPurposeRequired
	}
	r, ok := l.s.(HouseholdIdentityReader)
	if !ok {
		return Identity{}, ProblemHouseholdScopeUnavailable
	}
	return r.ReadHouseholdIdentity(ctx, purpose, ref)
}
