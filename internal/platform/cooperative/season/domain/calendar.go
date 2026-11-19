package domain

import (
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

const MaxCalendarRecipients = 500

// CalendarDraft is guidance only. It intentionally has no variety, SOP or
// harvest fields. Recipient membership/catalog activity require service ports
// outside the write transaction; this pure validator cannot authorize them.
type CalendarDraft struct {
	CropCode, SeasonCode string
	SeasonYear           int
	SowFrom, SowTo       time.Time
	Recipients           []uuid.UUID
}

func NormalizeCalendarDraft(in CalendarDraft) (CalendarDraft, error) {
	out := in
	out.CropCode, out.SeasonCode = strings.TrimSpace(in.CropCode), strings.TrimSpace(in.SeasonCode)
	errs := FieldErrors{}
	for _, field := range []struct{ name, value string }{{"crop_code", out.CropCode}, {"season_code", out.SeasonCode}} {
		if !ValidCatalogCode(field.value) {
			errs = append(errs, FieldError{field.name, "chọn mã danh mục hợp lệ"})
		}
	}
	if in.SeasonYear < 2000 || in.SeasonYear > 2100 {
		errs = append(errs, FieldError{"season_year", "năm trong khoảng 2000..2100"})
	}
	for _, field := range []struct {
		name  string
		value time.Time
	}{{"sow_from", in.SowFrom}, {"sow_to", in.SowTo}} {
		if field.value.IsZero() || !scheduleValidDay(field.value) {
			errs = append(errs, FieldError{field.name, "ngày bắt buộc trong khoảng năm 1..9999"})
		}
	}
	out.SowFrom, out.SowTo = calendarDate(in.SowFrom), calendarDate(in.SowTo)
	if out.SowFrom.After(out.SowTo) {
		errs = append(errs, FieldError{"sow_to", "ngày đến không trước ngày từ"})
	}
	if len(in.Recipients) == 0 || len(in.Recipients) > MaxCalendarRecipients {
		errs = append(errs, FieldError{"recipients", "chọn từ 1 đến 500 Thành viên HTX"})
	}
	seen := map[uuid.UUID]bool{}
	out.Recipients = []uuid.UUID{}
	for i, ref := range in.Recipients {
		if ref == uuid.Nil {
			errs = append(errs, FieldError{idx("recipients", i, ""), "tài khoản không hợp lệ"})
			continue
		}
		if !seen[ref] {
			seen[ref] = true
			out.Recipients = append(out.Recipients, ref)
		}
	}
	sort.Slice(out.Recipients, func(i, j int) bool { return out.Recipients[i].String() < out.Recipients[j].String() })
	if len(errs) > 0 {
		return CalendarDraft{}, errs
	}
	return out, nil
}
