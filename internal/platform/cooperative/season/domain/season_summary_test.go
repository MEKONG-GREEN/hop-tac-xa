package domain

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestAH152_AggregateMaterials(t *testing.T) {
	got := AggregateMaterials([]Input{
		{Name: "URE", Qty: decimal.RequireFromString("25.5"), Unit: "kg"},
		{Name: "DAP", Qty: decimal.NewFromInt(10), Unit: "kg"},
		{Name: "URE", Qty: decimal.RequireFromString("4.5"), Unit: "kg"},
		{Name: "URE", Qty: decimal.NewFromInt(2), Unit: "bao"},
		{Name: "Thuốc", Qty: decimal.Zero, Unit: "lít"},
	})
	if len(got) != 3 || got[0].Name != "DAP" || got[1].Name != "URE" || got[1].Unit != "bao" || got[2].Unit != "kg" || got[2].Qty.String() != "30" {
		t.Fatalf("%+v", got)
	}
}

func TestAH152_SummarizeTasks(t *testing.T) {
	d := "2026-09-20"
	c := Checklist{Stages: []StageProgress{
		{Seq: 1, Tasks: []TaskProgress{
			{ScheduleStep: ScheduleStep{Seq: 1}, CompletionStatus: "done_early", DoneOn: &d},
			{ScheduleStep: ScheduleStep{Seq: 2}, CompletionStatus: "done_on_time", DoneOn: &d},
			{ScheduleStep: ScheduleStep{Seq: 3}, CompletionStatus: "done_late", DoneOn: &d},
			{ScheduleStep: ScheduleStep{Seq: 4}, CompletionStatus: "overdue"},
		}},
		{Seq: 2, Tasks: []TaskProgress{
			{ScheduleStep: ScheduleStep{Seq: 5}, CompletionStatus: "skipped"},
			{ScheduleStep: ScheduleStep{Seq: 6}, CompletionStatus: "upcoming"}, // việc tổng kết
		}},
	}}
	tasks, unresolved, last := SummarizeTasks(c, 6)
	if tasks != (SummaryTasks{Early: 1, OnTime: 1, Late: 1, Skipped: 1, NotDone: 1, Total: 5}) || len(unresolved) != 0 || !last {
		t.Fatalf("%+v %v %v", tasks, unresolved, last)
	}
	// Không có việc tổng kết: việc 6 chưa làm chặn hoàn thành.
	_, unresolved, last = SummarizeTasks(c, 0)
	if len(unresolved) != 1 || unresolved[0] != 6 || last {
		t.Fatalf("%v %v", unresolved, last)
	}
}
