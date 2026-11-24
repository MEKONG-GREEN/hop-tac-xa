package port

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
)

// KindCooperativeSharing (AH-279, NTF-01, FN-09/BR-06): thông báo trong ứng dụng cho Đại diện HTX về yêu cầu chia sẻ
// dữ liệu — gửi tới, được duyệt, bị từ chối, bị thu hồi. Không đẩy, không email (giai đoạn sau).
const KindCooperativeSharing = "cooperative_sharing"

// SourceSharingNotice là tiền tố source_event_type: SourceSharingNotice + "." + action.
const SourceSharingNotice = "pf.notify.sharing_notice"

// ErrRecipientNotRepresentative: người nhận không phải Đại diện HTX đang hoạt động của HTX.
var ErrRecipientNotRepresentative = problem.New(http.StatusUnprocessableEntity, "notify.recipient_not_representative", "Người nhận không phải Đại diện HTX đang hoạt động của HTX này")

// SharingNotice là một thông báo về yêu cầu chia sẻ cho các Đại diện HTX của CooperativeRef. ActorCoopName là tên
// HTX đã thao tác (không CCCD, SĐT, lời nhắn). Action: request | approve | reject | revoke.
type SharingNotice struct {
	SourceID       uuid.UUID
	CooperativeRef uuid.UUID
	RequestRef     uuid.UUID
	Action         string
	ActorCoopName  string
	Recipients     []uuid.UUID
}

// RepresentativeNotifier tạo thông báo cho Đại diện HTX. Gọi NGOÀI DB transaction của bên gọi; cùng SourceID không tạo
// bản trùng; một người nhận không hợp lệ thì cả lần gọi bị từ chối.
type RepresentativeNotifier interface {
	SharingChanged(ctx context.Context, tenantID uuid.UUID, n SharingNotice) error
}

// RepresentativeDirectory kiểm một tài khoản có phải Đại diện HTX đang hoạt động của HTX (tài khoản bật, cùng tenant,
// org_ref bằng HTX, có vai coop_manager). Gốc lắp ráp hiện thực qua pf/accounts.
type RepresentativeDirectory interface {
	IsActiveRepresentative(ctx context.Context, tenantID, cooperativeRef, actorRef uuid.UUID) (bool, error)
}
