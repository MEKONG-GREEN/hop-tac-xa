package domain

import (
	"encoding/json"
	"testing"
	"time"
)

func scheduleDay(s string) time.Time {
	d, err := ParseDay(s)
	if err != nil {
		panic(err)
	}
	return d
}

func scheduleContent(w *Window) Content {
	return Content{
		Stages: []Stage{{Seq: 1, Name: "Canh tác", FromDay: ip(-5), ToDay: ip(7)}},
		Steps: []Step{
			{Seq: 1, StageSeq: 1, TaskSeq: 1, Name: "Thu hoạch", ActivityCode: ActThuHoach, Window: win(WindowDateRange, AnchorNXG, ip(90), ip(100))},
			{Seq: 2, StageSeq: 1, TaskSeq: 2, Name: "Theo dõi", ActivityCode: ActKiemTra, Window: w},
		},
	}
}

func TestScheduleFourWindowsAndInclusiveDates(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		window                  *Window
		today, from, to, status string
		undated                 bool
	}{
		{"range before", win(WindowDateRange, AnchorNXG, ip(-1), ip(2)), "2024-02-28", "2024-02-29", "2024-03-03", ScheduleUpcoming, false},
		{"range first", win(WindowDateRange, AnchorNXG, ip(-1), ip(2)), "2024-02-29", "2024-02-29", "2024-03-03", ScheduleDue, false},
		{"range last", win(WindowDateRange, AnchorNXG, ip(-1), ip(2)), "2024-03-03", "2024-02-29", "2024-03-03", ScheduleDue, false},
		{"range overdue", win(WindowDateRange, AnchorNXG, ip(-1), ip(2)), "2024-03-04", "2024-02-29", "2024-03-03", ScheduleOverdue, false},
		{"open before", win(WindowOpen, AnchorNXG, ip(1), nil), "2024-03-01", "2024-03-02", "", ScheduleUpcoming, false},
		{"open first", win(WindowOpen, AnchorNXG, ip(1), nil), "2024-03-02", "2024-03-02", "", ScheduleDue, false},
		{"open never late", win(WindowOpen, AnchorNXG, ip(1), nil), "2030-01-01", "2024-03-02", "", ScheduleDue, false},
		{"stage", win(WindowStage, "", nil, nil), "2024-03-01", "2024-02-25", "2024-03-08", ScheduleDue, false},
		{"none before anchor", win(WindowNone, "", nil, nil), "2023-01-01", "", "", ScheduleDue, true},
		{"none never late", win(WindowNone, "", nil, nil), "2030-01-01", "", "", ScheduleDue, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := CalculateSchedule(scheduleContent(tc.window), scheduleDay("2024-03-01"), scheduleDay(tc.today), nil)
			if err != nil {
				t.Fatal(err)
			}
			s := plan.Stages[0].Steps[1]
			checkScheduleDay(t, "from", s.FromDate, tc.from)
			checkScheduleDay(t, "to", s.ToDate, tc.to)
			checkScheduleDay(t, "expected", s.ExpectedDate, tc.from)
			if s.Status != tc.status || s.Undated != tc.undated {
				t.Fatalf("step: %+v", s)
			}
		})
	}
}

func checkScheduleDay(t *testing.T, field string, got *string, want string) {
	t.Helper()
	if want == "" {
		if got != nil {
			t.Fatalf("%s=%s, want nil", field, *got)
		}
		return
	}
	if got == nil || *got != want {
		t.Fatalf("%s=%v, want %s", field, got, want)
	}
}

func TestScheduleHarvestAnchorsAndMissingAnchor(t *testing.T) {
	c := scheduleContent(win(WindowDateRange, AnchorNTH, ip(2), ip(4)))
	p, err := CalculateSchedule(c, scheduleDay("2024-03-01"), scheduleDay("2024-06-01"), nil)
	if err != nil {
		t.Fatal(err)
	}
	checkScheduleDay(t, "harvest uses From", p.ExpectedHarvestDate, "2024-05-30")
	checkScheduleDay(t, "nth", p.Stages[0].Steps[1].FromDate, "2024-06-01")
	c.Steps[1].Window.Anchor = AnchorNTT
	p, err = CalculateSchedule(c, scheduleDay("2024-03-01"), scheduleDay("2024-06-01"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if s := p.Stages[0].Steps[1]; s.Status != ScheduleWaitingAnchor || s.Undated || s.FromDate != nil {
		t.Fatalf("missing NTT must not become none or NTH: %+v", s)
	}
	actual := scheduleDay("2024-04-10")
	p, err = CalculateSchedule(c, scheduleDay("2024-03-01"), scheduleDay("2024-04-12"), &actual)
	if err != nil {
		t.Fatal(err)
	}
	checkScheduleDay(t, "ntt", p.Stages[0].Steps[1].FromDate, "2024-04-12")
	c.Steps[0].ActivityCode = ActKiemTra
	c.Steps[1].Window.Anchor = AnchorNTH
	p, err = CalculateSchedule(c, scheduleDay("2024-03-01"), scheduleDay("2024-06-01"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.ExpectedHarvestDate != nil || p.Stages[0].Steps[1].Status != ScheduleWaitingAnchor {
		t.Fatalf("missing harvest: %+v", p)
	}
}

func TestScheduleLastHarvestStageAndCalendarRecalculation(t *testing.T) {
	c := scheduleContent(win(WindowDateRange, AnchorNTH, ip(-1), ip(0)))
	c.Stages[0].FromDay, c.Stages[0].ToDay = ip(30), ip(40)
	c.Steps = append(c.Steps, Step{Seq: 3, StageSeq: 1, TaskSeq: 3, Name: "Thu hoạch cuối", ActivityCode: ActThuHoach, Window: win(WindowStage, "", nil, nil)})
	before, _ := json.Marshal(c)
	p, err := CalculateSchedule(c, scheduleDay("2024-12-15"), scheduleDay("2025-01-01"), nil)
	if err != nil {
		t.Fatal(err)
	}
	checkScheduleDay(t, "last harvest From", p.ExpectedHarvestDate, "2025-01-14")
	checkScheduleDay(t, "nth negative", p.Stages[0].Steps[1].FromDate, "2025-01-13")
	q, err := CalculateSchedule(c, scheduleDay("2024-12-16"), scheduleDay("2025-01-01"), nil)
	if err != nil {
		t.Fatal(err)
	}
	checkScheduleDay(t, "recalculated", q.Stages[0].Steps[1].FromDate, "2025-01-14")
	checkScheduleDay(t, "old result stable", p.Stages[0].Steps[1].FromDate, "2025-01-13")
	after, _ := json.Marshal(c)
	if string(before) != string(after) {
		t.Fatal("calculator mutated SOP snapshot")
	}
}

func TestScheduleRejectsInvalidWindowsAndCalendarOverflow(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content Content
		anchor  time.Time
	}{
		{"missing window", scheduleContent(nil), scheduleDay("2024-01-01")},
		{"invalid range", scheduleContent(win(WindowDateRange, AnchorNXG, ip(3), ip(1))), scheduleDay("2024-01-01")},
		{"overflow", scheduleContent(win(WindowNone, "", nil, nil)), scheduleDay("9999-12-31")},
		{"underflow", scheduleContent(win(WindowNone, "", nil, nil)), scheduleDay("0001-01-02")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := CalculateSchedule(tc.content, tc.anchor, scheduleDay("2024-01-01"), nil); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}
