package assembly

import (
	"context"
	"errors"
	"slices"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	accport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/port"
	coopport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/port"
	notifyport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/notify/port"
)

// sharingRepsPage, sharingRepsMax: duyệt danh bạ theo trang; một HTX không có quá vài Đại diện.
const (
	sharingRepsPage = 100
	sharingRepsMax  = 500
)

// SharingRepresentatives hiện thực cooperative/port.SharingRepresentatives (AH-279) qua danh bạ pf/accounts.
type SharingRepresentatives struct{ Dir accport.Directory }

// Active liệt kê tài khoản Đại diện HTX đang hoạt động của HTX.
func (s SharingRepresentatives) Active(ctx context.Context, tid, coop uuid.UUID) ([]uuid.UUID, error) {
	if s.Dir == nil {
		return nil, accport.ErrUnavailable
	}
	out := []uuid.UUID{}
	for offset := 0; offset < sharingRepsMax; offset += sharingRepsPage {
		users, err := s.Dir.FindUsers(ctx, accport.Query{Attributes: map[string]string{accport.AttrTenantID: tid.String(), accport.AttrOrgRef: coop.String()}, First: offset, Max: sharingRepsPage})
		if err != nil {
			return nil, err
		}
		for _, u := range users {
			if !u.Enabled || u.TenantID != tid || u.OrgRef != coop {
				continue
			}
			roles, err := s.Dir.RealmRoles(ctx, u.ID)
			if err != nil {
				return nil, err
			}
			if slices.Contains(roles, htxroles.Representative) {
				out = append(out, u.ID)
			}
		}
		if len(users) < sharingRepsPage {
			break
		}
	}
	return out, nil
}

// SharingNotifier chuyển cooperative/port.SharingNoticeV1 sang cổng thông báo Đại diện HTX của pf/notify.
type SharingNotifier struct {
	Notifier notifyport.RepresentativeNotifier
}

// SharingChanged thực hiện cooperative/port.SharingNotifier.
func (n SharingNotifier) SharingChanged(ctx context.Context, tid uuid.UUID, e coopport.SharingNoticeV1) error {
	if n.Notifier == nil {
		return errors.New("sharing notice: notifier unavailable")
	}
	return n.Notifier.SharingChanged(ctx, tid, notifyport.SharingNotice{SourceID: e.SourceID, CooperativeRef: e.CooperativeRef,
		RequestRef: e.RequestRef, Action: e.Action, ActorCoopName: e.ActorCoopName, Recipients: e.Recipients})
}
