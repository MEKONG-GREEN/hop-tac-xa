package app

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/vault/identity/client"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/vault/identity/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/vault/identity/store"
)

// Exact lookup returns a reference only after current owner authorization.
// Absence and a phone belonging to another cooperative are indistinguishable.
func (s *Service) LookupHouseholdPhone(ctx context.Context, purpose client.Purpose, value string) (uuid.UUID, error) {
	phone, err := domain.NormalizePhone(value)
	if local := strings.TrimSpace(value); nativePhone.MatchString(local) {
		phone = "84" + local[1:]
		err = nil
	}
	if err != nil {
		if _, perr := householdIdentityCaller(ctx, purpose); perr != nil {
			return uuid.Nil, perr
		}
		return uuid.Nil, problem.Validation.WithField("phone", "SĐT không hợp lệ")
	}
	return s.lookupHousehold(ctx, purpose, domain.BlindPhone, phone)
}

// LookupHouseholdNationalID (AH-293, AC-N2) tìm hộ theo ĐỦ số CCCD (12 số) hoặc CMND (9 số) qua chỉ mục mù; một phần
// số không khớp (không có tìm gần đúng trên dữ liệu mã hóa). Cùng kiểm phạm vi chủ sở hữu như tìm theo SĐT.
func (s *Service) LookupHouseholdNationalID(ctx context.Context, purpose client.Purpose, value string) (uuid.UUID, error) {
	id, err := domain.NormalizeNationalID(value)
	if err != nil {
		if _, perr := householdIdentityCaller(ctx, purpose); perr != nil {
			return uuid.Nil, perr
		}
		return uuid.Nil, problem.Validation.WithField("national_id", "Nhập đủ 12 số CCCD hoặc 9 số CMND")
	}
	return s.lookupHousehold(ctx, purpose, domain.BlindNationalID, id)
}

func (s *Service) lookupHousehold(ctx context.Context, purpose client.Purpose, kind, normalized string) (uuid.UUID, error) {
	p, err := householdIdentityCaller(ctx, purpose)
	if err != nil {
		return uuid.Nil, err
	}
	if err = s.sensitive(ctx, p, "household.lookup", purpose, false, true); err != nil {
		return uuid.Nil, err
	}
	var ref uuid.UUID
	var found bool
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		ref, found, e = store.FindBlind(ctx, tx, []store.Blind{{Kind: kind, Value: domain.BlindIndex(s.BlindKey, p.TenantID, kind, normalized)}})
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
	scope, err := s.householdReadScope(ctx, p, ref)
	if err != nil {
		return uuid.Nil, err
	}
	// A shared (masked) reader must not confirm a full phone or ID number by probing.
	if scope.Shared {
		return uuid.Nil, client.ProblemNotFound
	}
	if err = s.logAccessTx(ctx, p, ref, domain.OpLookup, purpose, domain.OutcomeOK); err != nil {
		return uuid.Nil, err
	}
	return ref, nil
}
