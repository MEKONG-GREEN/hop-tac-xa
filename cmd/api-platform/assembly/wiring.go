package assembly

import (
	"context"

	"github.com/google/uuid"

	accport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/port"
	coopport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
)

// Bộ chuyển giữa port của các module (R-02: module chỉ thấy port của nhau; gốc lắp ráp
// ghép). Không có logic nghiệp vụ ngoài việc đổi dạng dữ liệu.

// ---------------------------------------------------------------------------
// Cảnh báo khẩn tới nông dân theo địa bàn (HTX US-NEWS-03): pf/cooperative + pf/accounts → pf/notify
// ---------------------------------------------------------------------------

// AreaFarmers ghép nông hộ có thửa trong xã (pf/cooperative) với tài khoản nông dân liên kết
// (pf/accounts) thành notify/port.AreaFarmers.
type AreaFarmers struct {
	Households coopport.Households
	Accounts   accport.FarmerAccounts
}

// FarmersInCommunes thực hiện notify/port.AreaFarmers.
func (a AreaFarmers) FarmersInCommunes(ctx context.Context, tid uuid.UUID, communes []string) ([]uuid.UUID, error) {
	ctx = tenant.WithID(ctx, tid)
	hh, err := a.Households.InCommunes(ctx, communes)
	if err != nil || len(hh) == 0 {
		return nil, err
	}
	return a.Accounts.SubjectsForHouseholds(ctx, hh)
}

// ---------------------------------------------------------------------------
// Cảnh báo trước khi vô hiệu hóa HTX (HTX US-HTXSTATUS-01 AC-1): pf/cooperative → pf/party
// ---------------------------------------------------------------------------

// SeasonActivity chuyển cooperative/port.Seasons sang party/port.SeasonActivity (party là kernel,
// không import cooperativeport; W3.3 thay bằng OrgImpactSource do pf/cooperative hiện thực).
type SeasonActivity struct{ Seasons coopport.Seasons }

// CoopActivity thực hiện party/port.SeasonActivity.
func (s SeasonActivity) CoopActivity(ctx context.Context, coop uuid.UUID) (int64, int64, error) {
	m, err := s.Seasons.ActivityByCooperative(ctx, []uuid.UUID{coop})
	if err != nil {
		return 0, 0, err
	}
	a := m[coop]
	return a.OpenSeasons, a.PendingLogs, nil
}
