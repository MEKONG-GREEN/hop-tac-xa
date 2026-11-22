package app

import (
	"context"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/plot/store"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// SharedCooperativeReads reports whether the signed Representative currently
// holds an approved FN-09 grant to read another cooperative (FN-06/BR-04). It
// is a live check, never a login snapshot, and never widens write scope.
type SharedCooperativeReads interface {
	NativeCanReadSharedCooperative(ctx context.Context, ref uuid.UUID) (bool, error)
}

// sharedPlotsOfCooperative is the read-only FN-06 path for a cooperative that
// approved sharing with the caller's cooperative. The physical READ ONLY tx and
// the SQL grant check in RLS are both required; the grant is checked again
// after the read so a concurrent revocation never releases the page.
func (s *Service) sharedPlotsOfCooperative(ctx context.Context, a access, coop, after uuid.UUID, limit int) (CoopPage, error) {
	if s.SharedReads == nil || coop == uuid.Nil {
		return CoopPage{}, ErrOutOfScope
	}
	check := func() error {
		ok, err := s.SharedReads.NativeCanReadSharedCooperative(ctx, coop)
		if err != nil {
			return err
		}
		if !ok {
			return ErrOutOfScope
		}
		return nil
	}
	if err := check(); err != nil {
		return CoopPage{}, err
	}
	var items []store.Plot
	err := tenant.WithTenantTxOptions(ctx, s.DB, a.p.TenantID, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pf_coop.native_shared_read_begin()`); err != nil {
			return err
		}
		var e error
		items, e = store.ListByCooperative(ctx, tx, store.Filter{All: true}, coop, after, limit+1)
		return e
	})
	if err != nil {
		return CoopPage{}, err
	}
	if err := check(); err != nil {
		return CoopPage{}, err
	}
	pg := CoopPage{Items: items}
	if len(items) > limit {
		pg.Items = items[:limit]
		last := pg.Items[limit-1].Ref
		pg.Next = &last
	}
	if pg.Items == nil {
		pg.Items = []store.Plot{}
	}
	return pg, nil
}
