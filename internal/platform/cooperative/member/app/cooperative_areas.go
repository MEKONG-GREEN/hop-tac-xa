package app

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/member/store"
	partyport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/party/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
)

var _ partyport.CooperativeAreaSource = (*Service)(nil)

// CooperativeAreas thực hiện port nội bộ của pf/party; quyền đọc HTX được caller
// kiểm trước. Không gọi port khác bên trong giao dịch đọc nguồn.
func (s *Service) CooperativeAreas(ctx context.Context, refs []uuid.UUID) (map[uuid.UUID]partyport.CooperativeAreas, error) {
	out := make(map[uuid.UUID]partyport.CooperativeAreas, len(refs))
	if len(refs) == 0 {
		return out, nil
	}
	var areas map[uuid.UUID]store.CooperativeAreas
	if err := tenant.FromCtxTx(ctx, s.DB, func(tx pgx.Tx) error {
		var err error
		areas, err = store.AreasByCooperatives(ctx, tx, refs)
		return err
	}); err != nil {
		return nil, err
	}
	for _, ref := range refs {
		a := areas[ref]
		v := partyport.CooperativeAreas{Households: a.Households, MissingCultivated: a.MissingCultivated, MissingLivestock: a.MissingLivestock}
		if a.MissingCultivated == 0 {
			v.CultivatedHa = &a.Cultivated
		}
		if a.MissingLivestock == 0 {
			v.LivestockHa = &a.Livestock
		}
		out[ref] = v
	}
	return out, nil
}
