package app

import (
	"context"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/store"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// All historical seasons count, including canceled/closed ones. Collection
// tickets per household do not exist yet (AH-232..234); their owner must join
// this protocol when implemented, as required by AH-285's 15/11 decision.
type HouseholdHistory struct{ DB tenant.Beginner }

func (h HouseholdHistory) HasHouseholdProduction(ctx context.Context, ref uuid.UUID) (bool, error) {
	var used bool
	err := tenant.FromCtxTx(ctx, h.DB, func(tx pgx.Tx) error { var e error; used, e = store.HasHouseholdProduction(ctx, tx, ref); return e })
	return used, err
}

var _ port.HouseholdProductionHistory = HouseholdHistory{}
