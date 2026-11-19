package port

import (
	"context"

	"github.com/google/uuid"
)

const EventSeasonCalendarPublished = "pf.cooperative.season_calendar.published"
const CalendarNoticeConsumer = "pf.cooperative.calendar_notice"

// CalendarMembers resolves account membership outside cooperative transactions.
// A dual representative/member account follows its effective representative role.
type CalendarMembers interface {
	Active(ctx context.Context, tenant, coop uuid.UUID, refs []uuid.UUID) ([]uuid.UUID, error)
	Page(ctx context.Context, tenant, coop uuid.UUID, offset, limit int) (CalendarMemberPage, error)
}

type CalendarMemberPage struct {
	Refs       []uuid.UUID
	NextOffset int // zero when the source page was the last page
}

// SeasonCalendarPublishedV1 contains business labels only, never account PII.
// SourceID identifies the immutable revision, so notify deduplicates retries.
type SeasonCalendarPublishedV1 struct {
	SourceID       uuid.UUID   `json:"source_id"`
	CalendarRef    uuid.UUID   `json:"calendar_ref"`
	CooperativeRef uuid.UUID   `json:"cooperative_ref"`
	CropName       string      `json:"crop_name"`
	SeasonName     string      `json:"season_name"`
	Year           int         `json:"season_year"`
	SowFrom        string      `json:"sow_from"`
	SowTo          string      `json:"sow_to"`
	Recipients     []uuid.UUID `json:"recipients"`
}

type CalendarNotifier interface {
	CalendarPublished(ctx context.Context, tenant uuid.UUID, notice SeasonCalendarPublishedV1) error
}
