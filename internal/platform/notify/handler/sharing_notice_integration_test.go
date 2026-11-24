package handler_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	notifyport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/notify/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
)

// AH-279 (NTF-01): thông báo trong ứng dụng cho Đại diện HTX về yêu cầu chia sẻ dữ liệu — có trong Trung tâm thông báo,
// không đẩy dù có thiết bị (AC-N6), chỉ Đại diện HTX đúng HTX nhận (AC-N5), gọi lại không trùng.
func TestAH279_SharingNoticeInAppOnly(t *testing.T) {
	p := &fakePusher{}
	f := newFixture(t, p)
	ctx := context.Background()
	coop := ids.New()
	rep := f.newMember(coop) // có thiết bị đã đăng ký
	f.members.mu.Lock()
	delete(f.members.members, rep.actor)
	f.members.reps[rep.actor] = coop
	f.members.mu.Unlock()
	member := f.newMember(coop)
	n := f.mod.RepresentativeNotifier()
	notice := notifyport.SharingNotice{SourceID: ids.New(), CooperativeRef: coop, RequestRef: ids.New(), Action: "request",
		ActorCoopName: "HTX Tân Phú", Recipients: []uuid.UUID{rep.actor}}

	// AC-N5: Thành viên HTX không nhận loại này — người nhận sai thì từ chối cả lần gọi.
	bad := notice
	bad.SourceID, bad.Recipients = ids.New(), []uuid.UUID{rep.actor, member.actor}
	if err := n.SharingChanged(ctx, f.tenant, bad); !errors.Is(err, notifyport.ErrRecipientNotRepresentative) {
		t.Fatalf("Thành viên nhận thông báo chia sẻ: %v", err)
	}
	if len(rep.list("")) != 0 {
		t.Fatal("lần gọi bị từ chối vẫn tạo thông báo")
	}
	for range 2 {
		if err := n.SharingChanged(ctx, f.tenant, notice); err != nil {
			t.Fatal(err)
		}
	}
	items := rep.list("")
	if len(items) != 1 {
		t.Fatalf("muốn 1 thông báo (không trùng), có %d", len(items))
	}
	it := items[0]
	if str(it, "kind") != notifyport.KindCooperativeSharing || !strings.Contains(str(it, "body"), "HTX Tân Phú gửi yêu cầu chia sẻ dữ liệu") ||
		str(it, "deep_link") != "/cooperatives/sharing/"+notice.RequestRef.String() {
		t.Fatalf("nội dung: %v", it)
	}
	if rep.unread() != 1 {
		t.Fatalf("chưa đọc: %d", rep.unread())
	}
	// AC-N6: không đẩy, không xếp lượt gửi.
	if ds := f.deliveries(str(it, "id")); len(ds) != 0 {
		t.Fatalf("thông báo chia sẻ không được đẩy: %+v", ds)
	}
	if n := f.count(`SELECT count(*) FROM shared.outbox WHERE tenant_id = $1 AND event_type = $2`, f.tenant, notifyport.EventPushRequested); n != 0 {
		t.Fatalf("phát pf.notify.push.requested %d lần", n)
	}
	if len(member.list("")) != 0 {
		t.Fatal("Thành viên HTX không có thông báo chia sẻ")
	}
}
