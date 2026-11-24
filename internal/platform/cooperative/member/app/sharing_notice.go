package app

// AH-279 (NTF-01, FN-09/BR-06): consumer pf.cooperative.sharing_notice gửi thông báo trong ứng dụng cho mọi Đại diện
// HTX đang hoạt động của bên nhận: yêu cầu gửi tới → HTX chủ dữ liệu; duyệt/từ chối → HTX yêu cầu; thu hồi → bên còn
// lại. Người vừa thao tác không nhận thông báo về thao tác của mình. Danh bạ và pf/notify chỉ gọi ngoài giao dịch inbox
// (txdeps); SourceID cố định nên giao lại không tạo bản trùng.

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/outbox"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/txdeps"
)

// SharingNoticeReady kiểm phụ thuộc của consumer khi khởi động (fail-closed).
func (s *Service) SharingNoticeReady() error {
	if s.SharingReps == nil || s.SharingNotifier == nil || s.Orgs == nil {
		return errors.New("sharing notice: configure accounts directory, notify and party before starting the consumer")
	}
	return nil
}

// sharingNoticeTarget trả HTX nhận thông báo của một thao tác.
func sharingNoticeTarget(e port.NativeSharingChangedV1) (uuid.UUID, bool) {
	switch e.Action {
	case "request":
		return e.OwnerRef, true
	case "approve", "reject":
		return e.RequesterRef, true
	case "revoke":
		if e.ActorOrgRef == e.RequesterRef {
			return e.OwnerRef, true
		}
		if e.ActorOrgRef == e.OwnerRef {
			return e.RequesterRef, true
		}
	}
	return uuid.Nil, false
}

// HandleSharingNotice là inbox.Handler của consumer port.SharingNoticeConsumer.
func (s *Service) HandleSharingNotice(ctx context.Context, _ pgx.Tx, env outbox.Envelope) error {
	if env.EventType != port.EventNativeSharingChanged || env.Producer != Producer || env.SchemaVersion != 1 {
		return errors.New("sharing notice: unexpected event")
	}
	var e port.NativeSharingChangedV1
	if err := json.Unmarshal(env.Payload, &e); err != nil {
		return err
	}
	target, ok := sharingNoticeTarget(e)
	if e.SourceID.Version() != 7 || e.RequestRef == uuid.Nil || e.ActorOrgRef == uuid.Nil || !ok {
		return errors.New("sharing notice: invalid payload")
	}
	_, err := txdeps.Get(ctx, "sharing.notice:"+e.SourceID.String(), func(ctx context.Context) (bool, error) {
		if err := s.SharingNoticeReady(); err != nil {
			return false, ErrDependency.WithCause(err)
		}
		actor, err := s.coopOrg(ctx, env.TenantID, e.ActorOrgRef)
		if err != nil {
			return false, err
		}
		if actor == nil {
			return false, nil // HTX thực hiện không còn: bỏ qua
		}
		reps, err := s.SharingReps.Active(ctx, env.TenantID, target)
		if err != nil {
			return false, ErrDependency.WithCause(err)
		}
		recipients := make([]uuid.UUID, 0, len(reps))
		for _, r := range reps {
			if r != e.ActorRef {
				recipients = append(recipients, r)
			}
		}
		if len(recipients) == 0 {
			return false, nil
		}
		return true, s.SharingNotifier.SharingChanged(ctx, env.TenantID, port.SharingNoticeV1{SourceID: e.SourceID, CooperativeRef: target,
			RequestRef: e.RequestRef, Action: e.Action, ActorCoopName: actor.Name, Recipients: recipients})
	})
	return err
}
