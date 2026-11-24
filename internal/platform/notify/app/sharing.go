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

// RepresentativeNotifier hiện thực port.RepresentativeNotifier (AH-279): kiểm người nhận qua Representatives ngoài
// transaction rồi ghi thông báo trong ứng dụng (SuppressPush — không đẩy, không email).
type RepresentativeNotifier struct{ Svc *Service }

// SharingChanged tạo NTF-01 cho các Đại diện HTX của n.CooperativeRef.
func (m RepresentativeNotifier) SharingChanged(ctx context.Context, tid uuid.UUID, n port.SharingNotice) error {
	tpl, err := domain.SharingTemplate(n)
	if err != nil {
		return err
	}
	tpl.CreatedAt = m.Svc.now()
	if tid == uuid.Nil {
		return port.ErrMemberNoticeInvalid.WithField("tenant_id", "bắt buộc")
	}
	if m.Svc.Representatives == nil {
		return ErrDependency
	}
	recipients := domain.DedupRefs(n.Recipients)
	if len(recipients) == 0 || len(recipients) > port.MaxMemberRecipients {
		return port.ErrMemberNoticeInvalid.WithField("recipients", "từ 1 đến 500 người nhận")
	}
	for i, r := range recipients {
		ok, err := m.Svc.Representatives.IsActiveRepresentative(ctx, tid, n.CooperativeRef, r)
		if err != nil {
			return err
		}
		if !ok {
			return port.ErrRecipientNotRepresentative.WithField("recipients", "người nhận thứ "+strconv.Itoa(i+1))
		}
	}
	ns := make([]domain.Notification, 0, len(recipients))
	for _, r := range recipients {
		x := tpl.For(r)
		x.SuppressPush = true
		ns = append(ns, x)
	}
	env := outbox.Envelope{EventID: n.SourceID, EventType: port.SourceSharingNotice + "." + n.Action, TenantID: tid, OccurredAt: m.Svc.now()}
	return tenant.WithTenantTx(ctx, m.Svc.DB, tid, func(tx pgx.Tx) error { return m.Svc.deliver(ctx, tx, env, ns) })
}
