package app

import (
	"context"
	"net/http"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/store"
	mdport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
)

const fnCalendar = "KHMV-FN-03-LICH"

var (
	ErrCalendarNotFound  = problem.New(http.StatusNotFound, "cropcycle.calendar_not_found", "Không tìm thấy lịch thời vụ")
	ErrCalendarChanged   = problem.New(http.StatusConflict, "cropcycle.calendar_changed", "Lịch đã thay đổi; tải lại trước khi lưu")
	ErrCalendarCancelled = problem.New(http.StatusConflict, "cropcycle.calendar_cancelled", "Lịch đã hủy, không sửa hoặc phát lại")
)

type CalendarView struct {
	Event     store.CalendarEvent
	CanManage bool
}
type CalendarWriteInput struct {
	CooperativeRef, CalendarRef, ExpectedEventID uuid.UUID
	Draft                                        domain.CalendarDraft
	Action, IdemKey                              string
}
type CalendarWriteResult struct {
	CalendarView
	Replayed bool
}

func (s *Service) calendarAccess(ctx context.Context, coop uuid.UUID, action htxroles.Action) (access, error) {
	a, err := s.matrixAccess(ctx, fnCalendar, action)
	if err != nil {
		return a, err
	}
	if coop == uuid.Nil || (a.kind != accAll && (a.p.OrgRef == uuid.Nil || a.p.OrgRef != coop)) {
		return a, ErrCalendarNotFound
	}
	return a, nil
}
func calendarManager(a access) bool {
	return htxroles.Can(htxroles.Effective(a.p.Has), fnCalendar, htxroles.Create)
}

func (s *Service) Calendar(ctx context.Context, ref uuid.UUID) (CalendarView, error) {
	a, err := s.matrixAccess(ctx, fnCalendar, htxroles.Read)
	if err != nil {
		return CalendarView{}, err
	}
	if ref == uuid.Nil || (a.kind != accAll && a.p.OrgRef == uuid.Nil) {
		return CalendarView{}, ErrCalendarNotFound
	}
	var out CalendarView
	err = s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		e, err := store.CalendarCurrent(ctx, tx, ref)
		if err != nil {
			return err
		}
		if e == nil || e.TenantID != a.p.TenantID || (a.kind != accAll && e.CooperativeRef != a.p.OrgRef) {
			return ErrCalendarNotFound
		}
		manage := calendarManager(a)
		if !manage && (e.Action == "cancelled" || !slices.Contains(e.Draft.Recipients, a.p.ActorRef)) {
			return ErrCalendarNotFound
		}
		out = CalendarView{*e, manage}
		return nil
	})
	return out, err
}

func (s *Service) Calendars(ctx context.Context, coop, after uuid.UUID, limit int) ([]CalendarView, *uuid.UUID, error) {
	a, err := s.calendarAccess(ctx, coop, htxroles.Read)
	if err != nil {
		return nil, nil, err
	}
	if limit < 1 || limit > 100 {
		return nil, nil, problem.Validation.WithField("limit", "từ 1 đến 100")
	}
	var events []store.CalendarEvent
	err = s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		var err error
		events, err = store.ListCalendars(ctx, tx, coop, a.p.ActorRef, after, limit+1, calendarManager(a))
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	var next *uuid.UUID
	if len(events) > limit {
		ref := events[limit-1].CalendarRef
		next = &ref
		events = events[:limit]
	}
	out := make([]CalendarView, 0, len(events))
	for _, e := range events {
		out = append(out, CalendarView{e, calendarManager(a)})
	}
	return out, next, nil
}

type CalendarRecipient struct {
	ActorRef uuid.UUID `json:"actor_ref"`
	Label    string    `json:"label" pii:"true"`
}
type CalendarRecipients struct {
	Items      []CalendarRecipient `json:"items"`
	NextOffset int                 `json:"next_offset"`
}

func (s *Service) CalendarRecipients(ctx context.Context, coop uuid.UUID, offset, limit int) (CalendarRecipients, error) {
	a, err := s.calendarAccess(ctx, coop, htxroles.Create)
	if err != nil {
		return CalendarRecipients{}, err
	}
	if offset < 0 || offset > 1000000 || limit < 1 || limit > 100 {
		return CalendarRecipients{}, problem.Validation.WithField("pagination", "phân trang không hợp lệ")
	}
	if s.dependencyTxOpen(ctx) || s.CalendarMembers == nil || s.Orgs == nil {
		return CalendarRecipients{}, ErrDependency
	}
	if err = s.checkCoop(ctx, coop); err != nil {
		return CalendarRecipients{}, err
	}
	page, err := s.CalendarMembers.Page(ctx, a.p.TenantID, coop, offset, limit)
	if err != nil {
		return CalendarRecipients{}, ErrDependency.WithCause(err)
	}
	labels := map[uuid.UUID]string{}
	if s.Names != nil && len(page.Refs) > 0 {
		labels, err = s.Names.DisplayNames(ctx, page.Refs)
		if err != nil {
			return CalendarRecipients{}, ErrDependency.WithCause(err)
		}
	}
	out := CalendarRecipients{Items: []CalendarRecipient{}, NextOffset: page.NextOffset}
	for _, ref := range page.Refs {
		out.Items = append(out.Items, CalendarRecipient{ActorRef: ref, Label: labels[ref]})
	}
	return out, nil
}

// Port reads happen before the transaction. Membership is checked again by the
// notification consumer; a missing directory never accepts unchecked UUIDs.
func (s *Service) WriteCalendar(ctx context.Context, in CalendarWriteInput) (CalendarWriteResult, error) {
	action := htxroles.Create
	if in.Action == "edited" {
		action = htxroles.Update
	}
	if in.Action == "cancelled" {
		action = htxroles.Delete
	}
	a, err := s.calendarAccess(ctx, in.CooperativeRef, action)
	if err != nil {
		return CalendarWriteResult{}, err
	}
	if in.IdemKey == "" {
		return CalendarWriteResult{}, problem.IdempotencyRequired
	}
	if in.Action != "published" && in.Action != "edited" && in.Action != "cancelled" {
		return CalendarWriteResult{}, problem.Validation.WithField("action", "thao tác không hợp lệ")
	}
	if (in.Action == "published" && (in.CalendarRef != uuid.Nil || in.ExpectedEventID != uuid.Nil)) || (in.Action != "published" && (in.CalendarRef == uuid.Nil || in.ExpectedEventID == uuid.Nil)) {
		return CalendarWriteResult{}, problem.Validation.WithField("expected_event_id", "cần phiên bản lịch hiện hành")
	}
	if in.Action != "cancelled" {
		in.Draft, err = domain.NormalizeCalendarDraft(in.Draft)
		if err != nil {
			return CalendarWriteResult{}, fieldProblem(err)
		}
	}
	key := "calendar:" + a.p.ActorRef.String() + ":" + in.IdemKey
	// Durable replay precedes dependency/state validation, so a successful retry
	// remains readable after the catalog/account changes or the calendar is cancelled.
	var old *store.CalendarEvent
	err = s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error { var err error; old, err = store.CalendarByIdem(ctx, tx, key); return err })
	if err != nil {
		return CalendarWriteResult{}, err
	}
	if old != nil {
		return replayCalendar(*old, in)
	}
	var cropName, seasonName string
	if in.Action != "cancelled" {
		if s.dependencyTxOpen(ctx) || s.CalendarMembers == nil || s.Catalogs == nil || s.Orgs == nil {
			return CalendarWriteResult{}, ErrDependency
		}
		if err = s.checkCoop(ctx, in.CooperativeRef); err != nil {
			return CalendarWriteResult{}, err
		}
		if err = s.checkCatalogScope(ctx, in.Draft.CropCode, ""); err != nil {
			return CalendarWriteResult{}, err
		}
		crop, err := s.catalogItem(ctx, mdport.CatalogCrop, "crop_code", in.Draft.CropCode)
		if err != nil {
			return CalendarWriteResult{}, err
		}
		cropName = crop.Name
		season, err := s.catalogItem(ctx, mdport.CatalogSeason, "season_code", in.Draft.SeasonCode)
		if err != nil {
			return CalendarWriteResult{}, err
		}
		seasonName = season.Name
		valid, err := s.CalendarMembers.Active(ctx, a.p.TenantID, in.CooperativeRef, in.Draft.Recipients)
		if err != nil {
			return CalendarWriteResult{}, ErrDependency.WithCause(err)
		}
		if len(valid) != len(in.Draft.Recipients) {
			return CalendarWriteResult{}, problem.Validation.WithField("recipients", "chọn Thành viên đang hoạt động của HTX này")
		}
		for _, r := range in.Draft.Recipients {
			if !slices.Contains(valid, r) {
				return CalendarWriteResult{}, problem.Validation.WithField("recipients", "chọn Thành viên đang hoạt động của HTX này")
			}
		}
	}
	var out CalendarWriteResult
	err = s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		out = CalendarWriteResult{}
		if err := store.LockCalendarOperation(ctx, tx, key); err != nil {
			return err
		}
		old, err := store.CalendarByIdem(ctx, tx, key)
		if err != nil {
			return err
		}
		if old != nil {
			out, err = replayCalendar(*old, in)
			return err
		}
		ref := in.CalendarRef
		var cur *store.CalendarEvent
		if in.Action == "published" {
			ref = ids.New()
		} else {
			if err := store.LockCalendar(ctx, tx, ref); err != nil {
				return err
			}
			cur, err = store.CalendarCurrent(ctx, tx, ref)
			if err != nil {
				return err
			}
			if cur == nil || cur.TenantID != a.p.TenantID || cur.CooperativeRef != in.CooperativeRef {
				return ErrCalendarNotFound
			}
			if cur.ID != in.ExpectedEventID {
				return ErrCalendarChanged
			}
			if cur.Action == "cancelled" {
				return ErrCalendarCancelled
			}
		}
		e := store.CalendarEvent{ID: ids.New(), TenantID: a.p.TenantID, CalendarRef: ref, CooperativeRef: in.CooperativeRef, Action: in.Action, Draft: in.Draft, ActorRef: a.p.ActorRef, EventTime: s.now(), IdemKey: key}
		if cur != nil {
			e.SupersedesID = &cur.ID
			reason := "Cập nhật lịch thời vụ"
			if in.Action == "cancelled" {
				reason = "Hủy lịch thời vụ"
				e.Draft = cur.Draft
			}
			e.CorrectionReason = &reason
		}
		if err = store.InsertCalendarEvent(ctx, tx, &e); err != nil {
			return err
		}
		if err = store.SaveCalendar(ctx, tx, e); err != nil {
			return err
		}
		if in.Action != "cancelled" {
			err = enqueue(ctx, tx, a.p.TenantID, port.EventSeasonCalendarPublished, e.EventTime, port.SeasonCalendarPublishedV1{SourceID: e.ID, CalendarRef: e.CalendarRef, CooperativeRef: e.CooperativeRef, CropName: cropName, SeasonName: seasonName, Year: e.Draft.SeasonYear, SowFrom: domain.FormatDay(e.Draft.SowFrom), SowTo: domain.FormatDay(e.Draft.SowTo), Recipients: e.Draft.Recipients})
			if err != nil {
				return err
			}
		}
		out = CalendarWriteResult{CalendarView: CalendarView{e, true}}
		return nil
	})
	return out, err
}
func replayCalendar(e store.CalendarEvent, in CalendarWriteInput) (CalendarWriteResult, error) {
	match := e.CooperativeRef == in.CooperativeRef && e.Action == in.Action
	if in.Action != "published" {
		match = match && e.CalendarRef == in.CalendarRef && e.SupersedesID != nil && *e.SupersedesID == in.ExpectedEventID
	}
	if in.Action != "cancelled" {
		match = match && e.Draft.CropCode == in.Draft.CropCode && e.Draft.SeasonCode == in.Draft.SeasonCode && e.Draft.SeasonYear == in.Draft.SeasonYear && domain.FormatDay(e.Draft.SowFrom) == domain.FormatDay(in.Draft.SowFrom) && domain.FormatDay(e.Draft.SowTo) == domain.FormatDay(in.Draft.SowTo) && slices.Equal(e.Draft.Recipients, in.Draft.Recipients)
	}
	if !match {
		return CalendarWriteResult{}, problem.IdempotencyConflict
	}
	return CalendarWriteResult{CalendarView: CalendarView{e, true}, Replayed: true}, nil
}
