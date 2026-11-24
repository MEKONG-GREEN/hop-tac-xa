package port

import (
	"context"

	"github.com/google/uuid"
)

// AH-279 (NTF-01, FN-09/BR-06): thông báo trong ứng dụng cho Đại diện HTX về yêu cầu chia sẻ dữ liệu. Sự kiện phát
// trong giao dịch đổi trạng thái (outbox); consumer gửi pf/notify sau khi kiểm danh bạ, ngoài giao dịch.
const (
	EventNativeSharingChanged = "pf.cooperative.native_sharing.changed"
	SharingNoticeConsumer     = "pf.cooperative.sharing_notice"
)

// NativeSharingChangedV1 là một lần đổi trạng thái yêu cầu chia sẻ: request, approve, reject, revoke. ActorOrgRef là
// HTX thực hiện; ActorRef là tài khoản thực hiện (không nhận thông báo về thao tác của chính mình).
type NativeSharingChangedV1 struct {
	SourceID     uuid.UUID `json:"source_id"`
	RequestRef   uuid.UUID `json:"request_ref"`
	Revision     int64     `json:"revision"`
	Action       string    `json:"action"`
	RequesterRef uuid.UUID `json:"requester_cooperative_ref"`
	OwnerRef     uuid.UUID `json:"owner_cooperative_ref"`
	ActorOrgRef  uuid.UUID `json:"actor_cooperative_ref"`
	ActorRef     uuid.UUID `json:"actor_ref"`
}

// SharingRepresentatives liệt kê tài khoản Đại diện HTX đang hoạt động của một HTX (gọi ngoài giao dịch).
type SharingRepresentatives interface {
	Active(ctx context.Context, tenant, coop uuid.UUID) ([]uuid.UUID, error)
}

// SharingNoticeV1 là một thông báo cho Đại diện HTX của CooperativeRef. Chỉ tên HTX (ActorCoopName), không PII.
type SharingNoticeV1 struct {
	SourceID       uuid.UUID
	CooperativeRef uuid.UUID
	RequestRef     uuid.UUID
	Action         string
	ActorCoopName  string
	Recipients     []uuid.UUID
}

// SharingNotifier tạo thông báo trong ứng dụng (không đẩy, không email — AC-N6).
type SharingNotifier interface {
	SharingChanged(ctx context.Context, tenant uuid.UUID, notice SharingNoticeV1) error
}
