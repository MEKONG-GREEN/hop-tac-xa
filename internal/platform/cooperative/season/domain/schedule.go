package domain

import "time"

const (
	ScheduleUpcoming      = "upcoming"
	ScheduleDue           = "due"
	ScheduleOverdue       = "overdue"
	ScheduleWaitingAnchor = "waiting_anchor"
)

// SchedulePlan is derived from the season's immutable SOP snapshot. Dates are
// calendar days, not instants. No yield, ledger or snapshot data is changed.
type SchedulePlan struct {
	AnchorDate          string          `json:"anchor_date"`
	AsOfDate            string          `json:"as_of_date"`
	ExpectedHarvestDate *string         `json:"expected_harvest_date"`
	Stages              []ScheduleStage `json:"stages"`
}

type ScheduleStage struct {
	Seq      int            `json:"seq"`
	Name     string         `json:"name"`
	FromDate *string        `json:"from_date"`
	ToDate   *string        `json:"to_date"`
	Steps    []ScheduleStep `json:"steps"`
}

type ScheduleStep struct {
	Seq          int     `json:"seq"`
	StageSeq     int     `json:"stage_seq"`
	TaskSeq      int     `json:"task_seq"`
	Name         string  `json:"name"`
	ActivityCode string  `json:"activity_code"`
	WindowKind   string  `json:"window_kind"`
	FromDate     *string `json:"from_date"`
	ToDate       *string `json:"to_date"`
	ExpectedDate *string `json:"expected_date"`
	Status       string  `json:"status"`
	Undated      bool    `json:"undated"`
}

// CalculateSchedule is pure. The caller supplies the business day in GMT+7 as
// asOf; all inputs are civil dates (their clock components are discarded). NTH
// uses From of the last harvest by sequence. Unknown NTH/NTT stays unresolved,
// whereas an explicit "none" window is always due, even before the anchor.
func CalculateSchedule(content Content, anchor, asOf time.Time, actualHarvest *time.Time) (SchedulePlan, error) {
	anchor, asOf = calendarDate(anchor), calendarDate(asOf)
	for _, d := range []struct {
		field string
		value time.Time
	}{{"anchor_date", anchor}, {"as_of_date", asOf}} {
		if !scheduleValidDay(d.value) {
			return SchedulePlan{}, FieldErrors{{d.field, "ngày trong khoảng năm 1..9999"}}
		}
	}
	var actual *time.Time
	if actualHarvest != nil {
		d := calendarDate(*actualHarvest)
		if !scheduleValidDay(d) {
			return SchedulePlan{}, FieldErrors{{"actual_harvest_date", "ngày trong khoảng năm 1..9999"}}
		}
		actual = &d
	}
	c, err := NormalizeContent(content)
	if err != nil {
		return SchedulePlan{}, err
	}
	var harvest *time.Time
	if i := lastHarvestIndex(c.Steps); i >= 0 {
		from, _, err := scheduleWindow(c.Steps[i], c.Stages, anchor, nil, actual)
		if err != nil {
			return SchedulePlan{}, err
		}
		harvest = from
	}
	plan := SchedulePlan{AnchorDate: FormatDay(anchor), AsOfDate: FormatDay(asOf),
		ExpectedHarvestDate: scheduleDateText(harvest), Stages: make([]ScheduleStage, len(c.Stages))}
	for i, stage := range c.Stages {
		from, to, err := scheduleOffsets(anchor, stage.FromDay, stage.ToDay)
		if err != nil {
			return SchedulePlan{}, err
		}
		plan.Stages[i] = ScheduleStage{Seq: stage.Seq, Name: stage.Name,
			FromDate: scheduleDateText(from), ToDate: scheduleDateText(to), Steps: []ScheduleStep{}}
	}
	for _, step := range c.Steps {
		from, to, err := scheduleWindow(step, c.Stages, anchor, harvest, actual)
		if err != nil {
			return SchedulePlan{}, err
		}
		undated := step.Window.Kind == WindowNone
		status := ScheduleWaitingAnchor
		switch {
		case undated:
			status = ScheduleDue
		case from == nil:
		case asOf.Before(*from):
			status = ScheduleUpcoming
		case to != nil && asOf.After(*to):
			status = ScheduleOverdue
		default:
			status = ScheduleDue
		}
		s := ScheduleStep{Seq: step.Seq, StageSeq: step.StageSeq, TaskSeq: step.TaskSeq, Name: step.Name,
			ActivityCode: step.ActivityCode, WindowKind: step.Window.Kind, FromDate: scheduleDateText(from),
			ToDate: scheduleDateText(to), ExpectedDate: scheduleDateText(from), Status: status, Undated: undated}
		plan.Stages[step.StageSeq-1].Steps = append(plan.Stages[step.StageSeq-1].Steps, s)
	}
	return plan, nil
}

func calendarDate(d time.Time) time.Time { return Day(d.Year(), d.Month(), d.Day()) }
func scheduleValidDay(d time.Time) bool  { return d.Year() >= 1 && d.Year() <= 9999 }

func scheduleDateText(d *time.Time) *string {
	if d == nil {
		return nil
	}
	s := FormatDay(*d)
	return &s
}

func scheduleOffsets(base time.Time, fromOffset, toOffset *int) (*time.Time, *time.Time, error) {
	var dates [2]*time.Time
	for i, offset := range []*int{fromOffset, toOffset} {
		if offset == nil {
			continue
		}
		d := AddDays(base, *offset)
		if !scheduleValidDay(d) {
			return nil, nil, FieldErrors{{"anchor_date", "mốc và cửa sổ làm ngày kế hoạch ngoài khoảng năm 1..9999"}}
		}
		dates[i] = &d
	}
	return dates[0], dates[1], nil
}

// Inputs have already passed NormalizeContent, including stage indices and
// required ranges. Do not use the temporary legacy day_offset/window_days.
func scheduleWindow(s Step, stages []Stage, anchor time.Time, harvest, actual *time.Time) (*time.Time, *time.Time, error) {
	w := s.Window
	switch w.Kind {
	case WindowNone:
		return nil, nil, nil
	case WindowStage:
		stage := stages[s.StageSeq-1]
		return scheduleOffsets(anchor, stage.FromDay, stage.ToDay)
	}
	base := &anchor
	switch w.Anchor {
	case AnchorNTH:
		base = harvest
	case AnchorNTT:
		base = actual
	}
	if base == nil {
		return nil, nil, nil
	}
	return scheduleOffsets(*base, w.From, w.To)
}
