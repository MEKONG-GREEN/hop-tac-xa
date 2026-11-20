package app

import (
	"context"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/vault/identity/client"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/vault/identity/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/vault/identity/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"strings"
)

// Exact lookup returns a reference only after current owner authorization.
// Absence and a phone belonging to another cooperative are indistinguishable.
func (s *Service) LookupHouseholdPhone(ctx context.Context, purpose client.Purpose, value string) (uuid.UUID, error) {
	p, err := householdIdentityCaller(ctx, purpose)
	if err != nil {
		return uuid.Nil, err
	}
	phone, err := domain.NormalizePhone(value)
	if local := strings.TrimSpace(value); nativePhone.MatchString(local) {
		phone = "84" + local[1:]
		err = nil
	}
	if err != nil {
		return uuid.Nil, problem.Validation.WithField("phone", "SĐT không hợp lệ")
	}
	if err = s.sensitive(ctx, p, "household.lookup", purpose, false, true); err != nil {
		return uuid.Nil, err
	}
	var ref uuid.UUID
	var found bool
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		ref, found, e = store.FindBlind(ctx, tx, []store.Blind{{Kind: domain.BlindPhone, Value: domain.BlindIndex(s.BlindKey, p.TenantID, domain.BlindPhone, phone)}})
		if e != nil {
			return e
		}
		if !found {
			return s.logAccess(ctx, tx, p, nil, false, domain.OpLookup, purpose, domain.OutcomeNotFound)
		}
		return nil
	})
	if err != nil {
		return uuid.Nil, err
	}
	if !found {
		return uuid.Nil, client.ProblemNotFound
	}
	if _, err = s.householdReadScope(ctx, p, ref); err != nil {
		return uuid.Nil, err
	}
	if err = s.logAccessTx(ctx, p, ref, domain.OpLookup, purpose, domain.OutcomeOK); err != nil {
		return uuid.Nil, err
	}
	return ref, nil
}
