package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/notify/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/clock"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
)

type fakeMembers struct {
	members map[uuid.UUID]bool
	err     error
	calls   int
}

func (f *fakeMembers) IsActiveMember(_ context.Context, _, _, actor uuid.UUID) (bool, error) {
	f.calls++
	return f.members[actor], f.err
}

func memberNotice(recipients ...uuid.UUID) port.LogMissingNotice {
	return port.LogMissingNotice{
		MemberNoticeBase: port.MemberNoticeBase{SourceID: uuid.Must(uuid.NewV7()), CooperativeRef: uuid.Must(uuid.NewV7()), Recipients: recipients},
		SeasonRef:        uuid.Must(uuid.NewV7()), HouseholdCode: "NH001", Days: 3,
	}
}

// DB: nil — nếu kiểm người nhận không chạy trước transaction thì chạm DB sẽ panic.
func newMemberSvc(m port.MemberDirectory) MemberNotifier {
	s := NewService(nil, clock.NewFixed(time.Date(2026, 10, 1, 3, 0, 0, 0, time.UTC)), Deps{Members: m})
	return MemberNotifier{Svc: s}
}

func TestAH22_AC4_RejectNonMember(t *testing.T) {
	member, admin := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	tid := uuid.Must(uuid.NewV7())

	t.Run("một người không phải Thành viên → từ chối cả lần gọi", func(t *testing.T) {
		dir := &fakeMembers{members: map[uuid.UUID]bool{member: true}}
		err := newMemberSvc(dir).LogMissing(context.Background(), tid, memberNotice(member, admin))
		if p := problem.From(err); p.Code != "notify.recipient_not_member" {
			t.Fatalf("lỗi = %v", err)
		}
	})
	t.Run("lỗi của directory được trả nguyên", func(t *testing.T) {
		boom := errors.New("keycloak down")
		dir := &fakeMembers{err: boom}
		err := newMemberSvc(dir).LogMissing(context.Background(), tid, memberNotice(member))
		if !errors.Is(err, boom) {
			t.Fatalf("lỗi = %v", err)
		}
	})
	t.Run("chưa nối directory → fail-closed", func(t *testing.T) {
		err := newMemberSvc(nil).LogMissing(context.Background(), tid, memberNotice(member))
		if !errors.Is(err, ErrDependency) {
			t.Fatalf("lỗi = %v", err)
		}
	})
	t.Run("tenant Nil", func(t *testing.T) {
		dir := &fakeMembers{members: map[uuid.UUID]bool{member: true}}
		err := newMemberSvc(dir).LogMissing(context.Background(), uuid.Nil, memberNotice(member))
		if p := problem.From(err); p.Code != "notify.member_notice_invalid" || dir.calls != 0 {
			t.Fatalf("lỗi = %v, calls = %d", err, dir.calls)
		}
	})
	t.Run("nội dung sai bị chặn trước khi gọi directory", func(t *testing.T) {
		dir := &fakeMembers{members: map[uuid.UUID]bool{member: true}}
		n := memberNotice(member)
		n.Days = 0
		err := newMemberSvc(dir).LogMissing(context.Background(), tid, n)
		if p := problem.From(err); p.Code != "notify.member_notice_invalid" || dir.calls != 0 {
			t.Fatalf("lỗi = %v, calls = %d", err, dir.calls)
		}
	})
}
