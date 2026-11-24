package assembly

import (
	"context"
	"errors"
	"slices"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	accport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/port"
	coopport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/port"
	notifyport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/notify/port"
)

type CalendarMembers struct{ Dir accport.Directory }

func (m CalendarMembers) active(ctx context.Context, tid, coop uuid.UUID, u accport.User) (bool, error) {
	if !u.Enabled || u.TenantID != tid || u.OrgRef != coop {
		return false, nil
	}
	roles, err := m.Dir.RealmRoles(ctx, u.ID)
	if err != nil {
		return false, err
	}
	return htxroles.Effective(func(role string) bool { return slices.Contains(roles, role) }) == htxroles.Member, nil
}
func (m CalendarMembers) Active(ctx context.Context, tid, coop uuid.UUID, refs []uuid.UUID) ([]uuid.UUID, error) {
	if m.Dir == nil {
		return nil, accport.ErrUnavailable
	}
	valid := make([]bool, len(refs))
	group, gctx := errgroup.WithContext(ctx)
	group.SetLimit(8)
	for i, ref := range refs {
		group.Go(func() error {
			u, err := m.Dir.GetUser(gctx, ref)
			if errors.Is(err, accport.ErrUserNotFound) {
				return nil
			}
			if err != nil {
				return err
			}
			valid[i], err = m.active(gctx, tid, coop, u)
			return err
		})
	}
	if err := group.Wait(); err != nil {
		return nil, err
	}
	out := []uuid.UUID{}
	for i, ref := range refs {
		if valid[i] {
			out = append(out, ref)
		}
	}
	return out, nil
}
func (m CalendarMembers) Page(ctx context.Context, tid, coop uuid.UUID, offset, limit int) (coopport.CalendarMemberPage, error) {
	if m.Dir == nil {
		return coopport.CalendarMemberPage{}, accport.ErrUnavailable
	}
	users, err := m.Dir.FindUsers(ctx, accport.Query{Attributes: map[string]string{accport.AttrTenantID: tid.String(), accport.AttrOrgRef: coop.String()}, First: offset, Max: limit})
	if err != nil {
		return coopport.CalendarMemberPage{}, err
	}
	out := coopport.CalendarMemberPage{Refs: []uuid.UUID{}}
	if len(users) == limit {
		out.NextOffset = offset + len(users)
	}
	valid := make([]bool, len(users))
	group, gctx := errgroup.WithContext(ctx)
	group.SetLimit(8)
	for i, u := range users {
		group.Go(func() error { var err error; valid[i], err = m.active(gctx, tid, coop, u); return err })
	}
	if err := group.Wait(); err != nil {
		return out, err
	}
	for i, u := range users {
		if valid[i] {
			out.Refs = append(out.Refs, u.ID)
		}
	}
	return out, nil
}

type CalendarNotifier struct{ Notifier notifyport.MemberNotifier }

func (n CalendarNotifier) CalendarPublished(ctx context.Context, tid uuid.UUID, e coopport.SeasonCalendarPublishedV1) error {
	if n.Notifier == nil {
		return errors.New("calendar: notifier unavailable")
	}
	return n.Notifier.CalendarPublished(ctx, tid, notifyport.CalendarPublishedNotice{MemberNoticeBase: notifyport.MemberNoticeBase{SourceID: e.SourceID, CooperativeRef: e.CooperativeRef, Recipients: e.Recipients}, CalendarRef: e.CalendarRef, CropName: e.CropName, SeasonName: e.SeasonName, Year: e.Year, SowFrom: e.SowFrom, SowTo: e.SowTo})
}

// TaskDue gửi NTF.2 của job nhắc việc (AH-191). TaskRef là mã lượt nhắc của vụ: một thông báo gom các việc cùng vụ
// cùng loại nhắc (KH-21), nên không trỏ tới một việc riêng.
func (n CalendarNotifier) TaskDue(ctx context.Context, tid uuid.UUID, e coopport.TaskDueV1) error {
	if n.Notifier == nil {
		return errors.New("task reminder: notifier unavailable")
	}
	return n.Notifier.TaskDue(ctx, tid, notifyport.TaskDueNotice{MemberNoticeBase: notifyport.MemberNoticeBase{SourceID: e.SourceID, CooperativeRef: e.CooperativeRef, Recipients: e.Recipients},
		SeasonRef: e.SeasonRef, TaskRef: e.SourceID, HouseholdCode: e.HouseholdCode, TaskName: e.TaskName, DueDate: e.DueDate, Overdue: e.Overdue})
}
