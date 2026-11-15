package app

import (
	"context"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/notify/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/notify/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/outbox"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
)

// MemberNotifier hiện thực port.MemberNotifier (AH-22): kiểm nội dung, kiểm người nhận qua Members ngoài
// transaction, rồi ghi thông báo và xếp lượt đẩy bằng đường deliver đang có.
type MemberNotifier struct{ Svc *Service }

// CalendarPublished tạo NTF.1.
func (m MemberNotifier) CalendarPublished(ctx context.Context, tid uuid.UUID, n port.CalendarPublishedNotice) error {
	tpl, err := domain.CalendarPublishedTemplate(n, m.Svc.now())
	if err != nil {
		return err
	}
	return m.Svc.notifyMembers(ctx, tid, n.MemberNoticeBase, tpl)
}

// TaskDue tạo NTF.2 (đến hạn hoặc trễ hạn).
func (m MemberNotifier) TaskDue(ctx context.Context, tid uuid.UUID, n port.TaskDueNotice) error {
	tpl, err := domain.TaskDueTemplate(n, m.Svc.now())
	if err != nil {
		return err
	}
	return m.Svc.notifyMembers(ctx, tid, n.MemberNoticeBase, tpl)
}

// LogMissing tạo NTF.3.
func (m MemberNotifier) LogMissing(ctx context.Context, tid uuid.UUID, n port.LogMissingNotice) error {
	tpl, err := domain.LogMissingTemplate(n, m.Svc.now())
	if err != nil {
		return err
	}
	return m.Svc.notifyMembers(ctx, tid, n.MemberNoticeBase, tpl)
}

// ProcessChanged tạo NTF.4.
func (m MemberNotifier) ProcessChanged(ctx context.Context, tid uuid.UUID, n port.ProcessChangedNotice) error {
	tpl, err := domain.ProcessChangedTemplate(n, m.Svc.now())
	if err != nil {
		return err
	}
	return m.Svc.notifyMembers(ctx, tid, n.MemberNoticeBase, tpl)
}

// notifyMembers kiểm từng người nhận NGOÀI transaction (gọi Keycloak); một người không hợp lệ thì từ chối cả
// lần gọi và không tạo gì. Gọi lại cùng SourceID không tạo bản trùng (unique source_event_id + người nhận).
func (s *Service) notifyMembers(ctx context.Context, tid uuid.UUID, base port.MemberNoticeBase, tpl domain.Template) error {
	if tid == uuid.Nil {
		return port.ErrMemberNoticeInvalid.WithField("tenant_id", "bắt buộc")
	}
	if s.Members == nil {
		return ErrDependency
	}
	recipients := domain.DedupRefs(base.Recipients)
	for i, r := range recipients {
		ok, err := s.Members.IsActiveMember(ctx, tid, base.CooperativeRef, r)
		if err != nil {
			return err
		}
		if !ok {
			return port.ErrRecipientNotMember.WithField("recipients", "người nhận thứ "+strconv.Itoa(i+1))
		}
	}
	ns := make([]domain.Notification, 0, len(recipients))
	for _, r := range recipients {
		ns = append(ns, tpl.For(r))
	}
	env := outbox.Envelope{EventID: base.SourceID, EventType: port.SourceMemberNotice + "." + tpl.Kind, TenantID: tid, OccurredAt: s.now()}
	return tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error { return s.deliver(ctx, tx, env, ns) })
}
