package client

import (
	"context"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
	"github.com/google/uuid"
	"net/http"
)

type HouseholdCommandScope struct {
	CommandRef       uuid.UUID `json:"command_ref"`
	HouseholdRef     uuid.UUID `json:"household_ref"`
	CooperativeRef   uuid.UUID `json:"cooperative_ref"`
	Operation        string    `json:"operation"`
	ExpectedRevision int64     `json:"expected_revision"`
	CommuneCode      string    `json:"commune_code"`
	Completed        bool      `json:"completed"`
}
type HouseholdCommandVerifier interface {
	HouseholdCommandScope(context.Context, uuid.UUID) (HouseholdCommandScope, error)
}
type HouseholdIdentityWriter interface {
	WriteHouseholdIdentity(context.Context, Purpose, uuid.UUID, Identity) error
}

var ProblemHouseholdNationalIDTaken = problem.New(http.StatusConflict, "vault.household_national_id_taken", "CCCD đã tồn tại, hộ này đã có trong hệ thống")
var ProblemHouseholdPhoneTaken = problem.New(http.StatusConflict, "vault.household_phone_taken", "SĐT đã được sử dụng")

type HouseholdIdentityWriteRequest struct {
	Purpose    string `json:"purpose"`
	FullName   string `json:"full_name" pii:"true"`
	Phone      string `json:"phone" pii:"true"`
	NationalID string `json:"national_id" pii:"true"`
	Address    string `json:"address" pii:"true"`
}

func (c *httpClient) WriteHouseholdIdentity(ctx context.Context, purpose Purpose, command uuid.UUID, in Identity) error {
	if purpose == "" {
		return ProblemPurposeRequired
	}
	return c.do(ctx, call{method: "POST", path: "/v1/household-commands/" + command.String() + "/identity", purpose: purpose,
		in: HouseholdIdentityWriteRequest{Purpose: string(purpose), FullName: in.FullName, Phone: in.Phone, NationalID: in.NationalID, Address: in.Address}})
}
func (l local) WriteHouseholdIdentity(ctx context.Context, purpose Purpose, command uuid.UUID, in Identity) error {
	if purpose == "" {
		return ProblemPurposeRequired
	}
	w, ok := l.s.(HouseholdIdentityWriter)
	if !ok {
		return ProblemHouseholdScopeUnavailable
	}
	return w.WriteHouseholdIdentity(ctx, purpose, command, in)
}
