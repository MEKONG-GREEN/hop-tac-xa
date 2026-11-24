package domain

import (
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/notify/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
)

// AH-279: nội dung bốn sự kiện chỉ có tên HTX; hành động lạ hoặc thiếu dữ liệu bị từ chối.
func TestSharingTemplate(t *testing.T) {
	base := port.SharingNotice{SourceID: ids.New(), CooperativeRef: ids.New(), RequestRef: ids.New(), ActorCoopName: "HTX B"}
	for action, want := range map[string]string{
		"request": "HTX B gửi yêu cầu chia sẻ dữ liệu.",
		"approve": "HTX B đã duyệt yêu cầu chia sẻ dữ liệu. Bạn đã xem được dữ liệu của HTX B.",
		"reject":  "HTX B đã từ chối yêu cầu chia sẻ dữ liệu.",
		"revoke":  "HTX B đã thu hồi yêu cầu chia sẻ dữ liệu.",
	} {
		n := base
		n.Action = action
		tpl, err := SharingTemplate(n)
		if err != nil || tpl.Body != want || tpl.Kind != port.KindCooperativeSharing || !strings.HasPrefix(tpl.DeepLink, SharingDeepLinkPrefix) {
			t.Fatalf("%s: %+v %v", action, tpl, err)
		}
	}
	for name, n := range map[string]port.SharingNotice{
		"hành động lạ":  {SourceID: base.SourceID, CooperativeRef: base.CooperativeRef, RequestRef: base.RequestRef, ActorCoopName: "HTX B", Action: "delete"},
		"thiếu tên":     {SourceID: base.SourceID, CooperativeRef: base.CooperativeRef, RequestRef: base.RequestRef, Action: "request"},
		"thiếu yêu cầu": {SourceID: base.SourceID, CooperativeRef: base.CooperativeRef, ActorCoopName: "HTX B", Action: "request"},
	} {
		if _, err := SharingTemplate(n); err == nil {
			t.Fatalf("%s: phải lỗi", name)
		}
	}
}
