package app

import (
	"context"
	"slices"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/party/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/google/uuid"
)

// nativeCooperativeRefs trả HTX của người gọi cùng các HTX đã duyệt chia sẻ đang hiệu lực (FN-06/BR-04). Chỉ
// Đại diện HTX được hưởng chia sẻ; Thành viên HTX, Admin và ngữ cảnh KN/phiên chọn giữ HTX của mình.
func (r *Registry) nativeCooperativeRefs(ctx context.Context, p authz.Principal) ([]uuid.UUID, error) {
	refs := []uuid.UUID{p.OrgRef}
	if r.NativeSharing == nil || p.KN != nil || p.SelectedSession != nil ||
		!p.Has(port.RoleCoopManager) || p.Has(authz.RolePlatformAdmin) {
		return refs, nil
	}
	shared, err := r.NativeSharing.NativeSharedCooperatives(ctx)
	if err != nil {
		return nil, err
	}
	for _, ref := range shared {
		if ref != uuid.Nil && !slices.Contains(refs, ref) {
			refs = append(refs, ref)
		}
	}
	return refs, nil
}

func (r *Registry) nativeCooperativeVisible(ctx context.Context, p authz.Principal, ref uuid.UUID) (bool, error) {
	refs, err := r.nativeCooperativeRefs(ctx, p)
	return slices.Contains(refs, ref), err
}
