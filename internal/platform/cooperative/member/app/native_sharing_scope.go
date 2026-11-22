package app

import (
	"context"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/member/store"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func nativeSharedReader(p authz.Principal) bool {
	return p.Has(htxroles.Representative) && !p.Has(htxroles.Admin) && p.KN == nil && p.SelectedSession == nil && p.OrgRef != uuid.Nil
}

// nativeReadTx never changes identity, node, tenant or commune projection. Its
// opt-in is local to a physically read-only tx and recomputes live grants in SQL.
func (s *Service) nativeReadTx(ctx context.Context, tid uuid.UUID, fn func(pgx.Tx) error) error {
	return tenant.WithTenantTxOptions(ctx, s.DB, tid, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		p, ok := authz.FromContext(ctx)
		if ok && nativeSharedReader(p) {
			if _, err := tx.Exec(ctx, `SELECT pf_coop.native_shared_read_begin()`); err != nil {
				return err
			}
		}
		return fn(tx)
	})
}

// NativeSharedCooperatives is the narrow port for Party's list/detail guard.
// The result is a live set, never cached on the login session or token.
func (s *Service) NativeSharedCooperatives(ctx context.Context) ([]uuid.UUID, error) {
	p, ok := authz.FromContext(ctx)
	if !ok || !nativeSharedReader(p) {
		return []uuid.UUID{}, nil
	}
	var out []uuid.UUID
	err := s.nativeReadTx(ctx, p.TenantID, func(tx pgx.Tx) error {
		requests, err := store.SharingList(ctx, tx, p.OrgRef)
		if err != nil {
			return err
		}
		for _, g := range requests {
			if g.Requester != p.OrgRef || g.State != "approved" {
				continue
			}
			var allowed bool
			if err := tx.QueryRow(ctx, `SELECT pf_coop.native_shared_read_allowed($1)`, g.Owner).Scan(&allowed); err != nil {
				return err
			}
			if allowed {
				out = append(out, g.Owner)
			}
		}
		return nil
	})
	return out, err
}

// NativeCanReadSharedCooperative is true only for another cooperative whose
// approved grant to the caller's cooperative is in force now (never own).
func (s *Service) NativeCanReadSharedCooperative(ctx context.Context, ref uuid.UUID) (bool, error) {
	p, ok := authz.FromContext(ctx)
	if !ok || ref == uuid.Nil || ref == p.OrgRef || !nativeSharedReader(p) {
		return false, nil
	}
	return s.nativeCanReadCooperative(ctx, p, ref)
}

func (s *Service) nativeCanReadCooperative(ctx context.Context, p authz.Principal, ref uuid.UUID) (bool, error) {
	if ref == p.OrgRef {
		return true, nil
	}
	if !nativeSharedReader(p) {
		return false, nil
	}
	var allowed bool
	err := s.nativeReadTx(ctx, p.TenantID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT pf_coop.native_shared_read_allowed($1)`, ref).Scan(&allowed)
	})
	return allowed, err
}
