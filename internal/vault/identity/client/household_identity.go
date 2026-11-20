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

type HouseholdPhoneLookup interface {
	LookupHouseholdPhone(context.Context, Purpose, string) (uuid.UUID, error)
}

func (c *httpClient) LookupHouseholdPhone(ctx context.Context, purpose Purpose, value string) (uuid.UUID, error) {
	if purpose == "" {
		return uuid.Nil, ProblemPurposeRequired
	}
	var out LookupResponse
	err := c.do(ctx, call{method: "POST", path: "/v1/household-identities/phone-lookup", purpose: purpose, in: LookupRequest{Purpose: string(purpose), Value: value}, out: &out})
	if err != nil {
		return uuid.Nil, err
	}
	ref, err := uuid.Parse(out.Ref)
	if err != nil || ref == uuid.Nil {
		return uuid.Nil, ProblemHouseholdScopeUnavailable
	}
	return ref, nil
}
func (l local) LookupHouseholdPhone(ctx context.Context, purpose Purpose, value string) (uuid.UUID, error) {
	if purpose == "" {
		return uuid.Nil, ProblemPurposeRequired
	}
	q, ok := l.s.(HouseholdPhoneLookup)
	if !ok {
		return uuid.Nil, ProblemHouseholdScopeUnavailable
	}
	return q.LookupHouseholdPhone(ctx, purpose, value)
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
