package domain

import (
	"github.com/google/uuid"
	"reflect"
	"testing"
)

func calendarDraft() CalendarDraft {
	return CalendarDraft{CropCode: "LUA", SeasonCode: "DX", SeasonYear: 2026, SowFrom: scheduleDay("2026-12-25"), SowTo: scheduleDay("2027-01-05"), Recipients: []uuid.UUID{uuid.MustParse("00000000-0000-4000-8000-000000000002"), uuid.MustParse("00000000-0000-4000-8000-000000000001")}}
}

func TestCalendarGuidanceCrossYearAndNoMutation(t *testing.T) {
	in := calendarDraft()
	in.CropCode = " LUA "
	in.Recipients = append(in.Recipients, in.Recipients[0])
	original := append([]uuid.UUID(nil), in.Recipients...)
	out, err := NormalizeCalendarDraft(in)
	if err != nil {
		t.Fatal(err)
	}
	if out.CropCode != "LUA" || len(out.Recipients) != 2 || !reflect.DeepEqual(in.Recipients, original) || FormatDay(out.SowTo) != "2027-01-05" {
		t.Fatalf("out=%+v input=%+v", out, in)
	}
	in.SowTo = in.SowFrom
	if _, err = NormalizeCalendarDraft(in); err != nil {
		t.Fatalf("inclusive one-day frame: %v", err)
	}
}

func TestCalendarRejectsInvalidFields(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*CalendarDraft)
	}{
		{"reversed", func(d *CalendarDraft) { d.SowTo = d.SowFrom.AddDate(0, 0, -1) }},
		{"missing date", func(d *CalendarDraft) { d.SowFrom = d.SowFrom.AddDate(-2026, 0, 0) }},
		{"invalid crop", func(d *CalendarDraft) { d.CropCode = "" }},
		{"invalid season", func(d *CalendarDraft) { d.SeasonCode = "bad/code" }},
		{"year", func(d *CalendarDraft) { d.SeasonYear = 2101 }},
		{"no recipients", func(d *CalendarDraft) { d.Recipients = nil }},
		{"nil recipient", func(d *CalendarDraft) { d.Recipients = []uuid.UUID{uuid.Nil} }},
		{"too many", func(d *CalendarDraft) { d.Recipients = make([]uuid.UUID, 501) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := calendarDraft()
			tc.mutate(&d)
			if _, err := NormalizeCalendarDraft(d); err == nil {
				t.Fatal("invalid guidance accepted")
			}
		})
	}
}
