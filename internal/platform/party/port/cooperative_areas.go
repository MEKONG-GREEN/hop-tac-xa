package port

import (
	"context"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// CooperativeAreas là diện tích các hộ đang thuộc HTX (kể cả hộ ngừng tham gia
// chưa đóng membership). Không cộng diện tích vụ hay diện tích đăng ký KN.
// Tổng nil khi có hộ chưa khai báo; HTX chưa có hộ trả số 0 và Missing=0.
type CooperativeAreas struct {
	CultivatedHa      *decimal.Decimal
	LivestockHa       *decimal.Decimal
	Households        int64
	MissingCultivated int64
	MissingLivestock  int64
}

// CooperativeAreaSource đọc nguồn pf/cooperative. Caller chỉ truyền các HTX đã
// kiểm quyền; callee vẫn áp RLS tenant/phạm vi của request. Gọi ngoài giao dịch.
type CooperativeAreaSource interface {
	CooperativeAreas(context.Context, []uuid.UUID) (map[uuid.UUID]CooperativeAreas, error)
}
