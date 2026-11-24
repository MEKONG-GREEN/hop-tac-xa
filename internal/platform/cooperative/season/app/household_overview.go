package app

// AH-140 (FN-03.12, FN-03-BR-08, DATA-03): Đại diện HTX (và Thành viên, Admin) xem chỉ đọc danh sách vụ nông hộ của HTX
// kèm trạng thái, % tiến độ, số việc đến hạn, trễ; lọc theo người mở vụ (Thành viên HTX) và trạng thái. Phạm vi theo HTX
// (KH-02); tên người mở vụ đọc qua cổng ActorNames NGOÀI giao dịch.

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/store"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
)

// SeasonOverview là một vụ trong danh sách tiến độ của HTX.
type SeasonOverview struct {
	Season        store.Season
	Progress      *domain.Checklist
	OpenedByLabel string
}

// SeasonOverviewPage là một trang danh sách tiến độ.
type SeasonOverviewPage struct {
	Items []SeasonOverview
	Next  *uuid.UUID
}

// HouseholdSeasonOverview đọc danh sách vụ nông hộ của HTX kèm tiến độ (KHMV-FN-03-XEM R).
func (s *Service) HouseholdSeasonOverview(ctx context.Context, coop uuid.UUID, status string, openedBy, after uuid.UUID, limit int) (SeasonOverviewPage, error) {
	if _, err := s.matrixAccess(ctx, fnSeasonView, htxroles.Read); err != nil {
		return SeasonOverviewPage{}, err
	}
	a, err := s.householdAccess(ctx, coop, false)
	if err != nil {
		return SeasonOverviewPage{}, err
	}
	if status != "" && status != port.SeasonOpen && status != "completed" && status != port.SeasonCancelled {
		return SeasonOverviewPage{}, problem.Validation.WithField("status", "trạng thái không hợp lệ")
	}
	limit = clampLimit(limit)
	page := SeasonOverviewPage{Items: []SeasonOverview{}}
	err = s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		rows, err := store.CoopHouseholdSeasons(ctx, tx, coop, status, openedBy, after, limit+1)
		if err != nil {
			return err
		}
		if len(rows) > limit {
			rows = rows[:limit]
			ref := rows[len(rows)-1].SeasonRef
			page.Next = &ref
		}
		for _, cur := range rows {
			item := SeasonOverview{Season: cur}
			if list, err := s.householdChecklistTx(ctx, tx, cur, false); err == nil {
				item.Progress = &list.Progress
			} else {
				s.logWarn("không tính được tiến độ vụ", "season_ref", cur.SeasonRef, "error", err)
			}
			page.Items = append(page.Items, item)
		}
		return nil
	})
	if err != nil || s.Names == nil || len(page.Items) == 0 {
		return page, err
	}
	refs := make([]uuid.UUID, 0, len(page.Items))
	seen := map[uuid.UUID]bool{}
	for _, it := range page.Items {
		if r := it.Season.OpenedBy; r != uuid.Nil && !seen[r] {
			seen[r] = true
			refs = append(refs, r)
		}
	}
	labels, err := s.Names.DisplayNames(ctx, refs)
	if err != nil {
		return page, nil // tên chỉ để hiển thị; thiếu danh bạ vẫn trả danh sách
	}
	for i := range page.Items {
		page.Items[i].OpenedByLabel = labels[page.Items[i].Season.OpenedBy]
	}
	return page, nil
}
