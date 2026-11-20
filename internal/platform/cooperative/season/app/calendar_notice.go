package app

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/store"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/outbox"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/txdeps"
)

// External membership lookups and notify's own transaction run only after inbox
// rolled back, including when both modules share a pool with MaxConns=1.
func (s *Service) HandleCalendarNotice(ctx context.Context, _ pgx.Tx, env outbox.Envelope) error {
	if env.EventType != port.EventSeasonCalendarPublished || env.Producer != "pf/cooperative" || env.SchemaVersion != 1 {
		return errors.New("calendar: unexpected notification event")
	}
	var notice port.SeasonCalendarPublishedV1
	if err := json.Unmarshal(env.Payload, &notice); err != nil {
		return err
	}
	if notice.SourceID.Version() != 7 || notice.CalendarRef.Version() != 7 || notice.CropName == "" || notice.SeasonName == "" {
		return errors.New("calendar: invalid notice payload")
	}
	_, err := txdeps.Get(ctx, "calendar.notice:"+notice.SourceID.String(), func(ctx context.Context) (bool, error) {
		if s.CalendarMembers == nil || s.CalendarNotifier == nil {
			return false, ErrDependency
		}
		var e *store.CalendarEvent
		err := s.inTx(ctx, env.TenantID, func(tx pgx.Tx) error {
			var err error
			e, err = store.CalendarCurrent(ctx, tx, notice.CalendarRef)
			return err
		})
		if err != nil {
			return false, err
		}
		if e == nil || e.Action == "cancelled" || e.ID != notice.SourceID {
			return false, nil
		}
		if e.TenantID != env.TenantID || e.CooperativeRef != notice.CooperativeRef || e.Draft.SeasonYear != notice.Year || domain.FormatDay(e.Draft.SowFrom) != notice.SowFrom || domain.FormatDay(e.Draft.SowTo) != notice.SowTo || !slices.Equal(e.Draft.Recipients, notice.Recipients) {
			return false, errors.New("calendar: notice does not match immutable revision")
		}
		recipients, err := s.CalendarMembers.Active(ctx, env.TenantID, e.CooperativeRef, e.Draft.Recipients)
		if err != nil {
			return false, ErrDependency.WithCause(err)
		}
		if len(recipients) == 0 {
			return false, nil
		}
		// A directory adapter must never broaden the selected recipient set.
		for _, ref := range recipients {
			if !slices.Contains(e.Draft.Recipients, ref) {
				return false, errors.New("calendar: directory returned an unselected recipient")
			}
		}
		notice.Recipients = recipients
		if err = s.CalendarNotifier.CalendarPublished(ctx, env.TenantID, notice); err != nil {
			return false, err
		}
		return true, nil
	})
	return err
}
