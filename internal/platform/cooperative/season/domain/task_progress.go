package domain

import (
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// TaskRecord is copied into FarmLog, including the recommendation at recording.
// State values partial/skipped support subsequent journal workflows; quick tick
// only writes done. None of these records imply a journal review.
type TaskRecord struct {
	StepSeq       int          `json:"step_seq"`
	StageSeq      int          `json:"stage_seq"`
	State         string       `json:"state"`
	DoneOn        string       `json:"done_on"`
	SOPVersionID  uuid.UUID    `json:"sop_version_id"`
	SeasonEventID uuid.UUID    `json:"season_event_id"`
	Step          Step         `json:"step"`
	Details       *TaskDetails `json:"details,omitempty"`
	// Adhoc: việc phát sinh ngoài quy trình (AH-163) — nhãn "Ngoài SOP", số việc riêng > AdhocStepSeqBase, không tính
	// vào tiến độ.
	Adhoc bool `json:"adhoc,omitempty"`
}

// AdhocStepSeqBase: số việc của việc phát sinh bắt đầu sau mọi số việc của quy trình (MaxPlanStepSeq).
const AdhocStepSeqBase = MaxPlanStepSeq

type TaskProgress struct {
	ScheduleStep
	CompletionStatus string  `json:"completion_status"`
	DoneOn           *string `json:"done_on"`
	CanComplete      bool    `json:"can_complete"`
}
type StageProgress struct {
	Seq    int            `json:"seq"`
	Name   string         `json:"name"`
	Status string         `json:"status"`
	Tasks  []TaskProgress `json:"tasks"`
}
type Checklist struct {
	AsOfDate string          `json:"as_of_date"`
	Stages   []StageProgress `json:"stages"`
	Done     int             `json:"done"`
	Skipped  int             `json:"skipped"`
	Total    int             `json:"total"`
	Percent  *string         `json:"percent"`
}

// BuildChecklist is a pure derivation from the pinned schedule and current logs.
// completed is the existing harvested state; unknown/closed/cancelled fail closed.
func BuildChecklist(plan SchedulePlan, records []TaskRecord, seasonStatus string) (Checklist, error) {
	return BuildSeasonChecklist(plan, records, seasonStatus, nil)
}

// BuildSeasonChecklist như BuildChecklist; skipped là việc gốc đã bỏ khỏi quy trình của vụ (AH-113) — hiện "Bỏ qua",
// không tích được, không tính vào mẫu số tiến độ. Việc đã xong trước khi bỏ vẫn là đã xong.
func BuildSeasonChecklist(plan SchedulePlan, records []TaskRecord, seasonStatus string, skipped map[int]bool) (Checklist, error) {
	byStep := map[int]TaskRecord{}
	for _, r := range records {
		day, err := ParseDay(r.DoneOn)
		if err != nil || FormatDay(day) > plan.AsOfDate {
			return Checklist{}, FieldErrors{{"done_on", "ngày làm xong không được sau hôm nay"}}
		}
		if r.State != "done" && r.State != "partial" && r.State != "skipped" {
			return Checklist{}, FieldErrors{{"state", "trạng thái nhật ký không hợp lệ"}}
		}
		old, ok := byStep[r.StepSeq]
		if !ok || old.State == "partial" || r.State == "done" {
			byStep[r.StepSeq] = r
		}
	}
	out := Checklist{AsOfDate: plan.AsOfDate, Stages: []StageProgress{}}
	lastStage := 0
	for _, s := range plan.Stages {
		if s.Seq > lastStage {
			lastStage = s.Seq
		}
	}
	for _, s := range plan.Stages {
		stage := StageProgress{Seq: s.Seq, Name: s.Name, Status: "not_started", Tasks: []TaskProgress{}}
		resolved, started := 0, false
		for _, w := range s.Steps {
			task := TaskProgress{ScheduleStep: w, CompletionStatus: w.Status}
			r, ok := byStep[w.Seq]
			if ok {
				started = true
			}
			if ok && r.State == "done" {
				out.Done++
				resolved++
				d := r.DoneOn
				task.DoneOn = &d
				task.CompletionStatus = "done"
				if w.Undated {
					task.CompletionStatus = "done_on_time"
				} else if w.FromDate != nil {
					task.CompletionStatus = "done_on_time"
					if d < *w.FromDate {
						task.CompletionStatus = "done_early"
					} else if w.ToDate != nil && d > *w.ToDate {
						task.CompletionStatus = "done_late"
					}
				}
			} else if (ok && r.State == "skipped") || skipped[w.Seq] {
				out.Skipped++
				resolved++
				task.CompletionStatus = "skipped"
			}
			task.CanComplete = !skipped[w.Seq] && (!ok || r.State == "partial") && (seasonStatus == "open" || (seasonStatus == "completed" && s.Seq == lastStage))
			out.Total++
			stage.Tasks = append(stage.Tasks, task)
		}
		if started {
			stage.Status = "in_progress"
		}
		if len(s.Steps) > 0 && resolved == len(s.Steps) {
			stage.Status = "done"
		}
		out.Stages = append(out.Stages, stage)
	}
	if denominator := out.Total - out.Skipped; denominator > 0 {
		p := decimal.NewFromInt(int64(out.Done)).Mul(decimal.NewFromInt(100)).Div(decimal.NewFromInt(int64(denominator))).StringFixed(2)
		out.Percent = &p
	}
	return out, nil
}
