package domain

import (
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/notify/port"
)

// SharingDeepLinkPrefix: đường dẫn trong app của yêu cầu chia sẻ; web/app mở màn Liên kết & chia sẻ dữ liệu (FN-09).
const SharingDeepLinkPrefix = "/cooperatives/sharing/"

// SharingTemplate dựng thông báo NTF-01 (AH-279). Nội dung chỉ có tên HTX.
func SharingTemplate(n port.SharingNotice) (Template, error) {
	if n.SourceID == uuid.Nil || n.SourceID.Version() != 7 {
		return Template{}, invalid("source_id", "phải là UUIDv7")
	}
	if err := requireRef("cooperative_ref", n.CooperativeRef); err != nil {
		return Template{}, err
	}
	if err := requireRef("request_ref", n.RequestRef); err != nil {
		return Template{}, err
	}
	name, err := requireText("actor_coop_name", n.ActorCoopName)
	if err != nil {
		return Template{}, err
	}
	var title, body string
	switch n.Action {
	case "request":
		title, body = "Yêu cầu chia sẻ dữ liệu mới", name+" gửi yêu cầu chia sẻ dữ liệu."
	case "approve":
		title, body = "Yêu cầu chia sẻ dữ liệu được duyệt", name+" đã duyệt yêu cầu chia sẻ dữ liệu. Bạn đã xem được dữ liệu của "+name+"."
	case "reject":
		title, body = "Yêu cầu chia sẻ dữ liệu bị từ chối", name+" đã từ chối yêu cầu chia sẻ dữ liệu."
	case "revoke":
		title, body = "Yêu cầu chia sẻ dữ liệu bị thu hồi", name+" đã thu hồi yêu cầu chia sẻ dữ liệu."
	default:
		return Template{}, invalid("action", "request, approve, reject hoặc revoke")
	}
	subject := n.RequestRef
	return Template{Kind: port.KindCooperativeSharing, Severity: port.SeverityNormal, Title: title, Body: body,
		DeepLink: SharingDeepLinkPrefix + n.RequestRef.String(), SubjectRef: &subject}, nil
}
