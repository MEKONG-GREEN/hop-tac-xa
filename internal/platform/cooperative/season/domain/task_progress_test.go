package domain

import "testing"

func TestTaskProgressDatesAndStates(t *testing.T) {
	for _, tc := range []struct{ day, want string }{{"2024-02-28", "done_early"}, {"2024-02-29", "done_on_time"}, {"2024-03-03", "done_on_time"}, {"2024-03-04", "done_late"}} {
		p, err := CalculateSchedule(scheduleContent(win(WindowDateRange, AnchorNXG, ip(-1), ip(2))), scheduleDay("2024-03-01"), scheduleDay("2024-03-10"), nil)
		if err != nil {
			t.Fatal(err)
		}
		c, err := BuildChecklist(p, []TaskRecord{{StepSeq: 2, State: "done", DoneOn: tc.day}}, "open")
		if err != nil {
			t.Fatal(err)
		}
		if c.Stages[0].Tasks[1].CompletionStatus != tc.want || c.Stages[0].Status != "in_progress" || *c.Percent != "50.00" || c.Stages[0].Tasks[1].CanComplete {
			t.Fatalf("%s: %+v", tc.day, c)
		}
	}
}

func TestTaskProgressNoneOpenSkippedAndClosed(t *testing.T) {
	for _, w := range []*Window{win(WindowNone, "", nil, nil), win(WindowOpen, AnchorNXG, ip(1), nil)} {
		p, err := CalculateSchedule(scheduleContent(w), scheduleDay("2024-03-01"), scheduleDay("2030-01-01"), nil)
		if err != nil {
			t.Fatal(err)
		}
		c, err := BuildChecklist(p, []TaskRecord{{StepSeq: 1, State: "skipped", DoneOn: "2024-03-01"}, {StepSeq: 2, State: "done", DoneOn: "2029-01-01"}}, "open")
		if err != nil {
			t.Fatal(err)
		}
		if c.Stages[0].Status != "done" || c.Stages[0].Tasks[1].CompletionStatus != "done_on_time" || *c.Percent != "100.00" {
			t.Fatalf("%+v", c)
		}
		c, err = BuildChecklist(p, []TaskRecord{{StepSeq: 1, State: "skipped", DoneOn: "2024-03-01"}, {StepSeq: 2, State: "skipped", DoneOn: "2024-03-01"}}, "open")
		if err != nil {
			t.Fatal(err)
		}
		if c.Percent != nil || c.Stages[0].Status != "done" {
			t.Fatal(c)
		}
		for _, status := range []string{"cancelled", "closed", "unknown"} {
			c, err = BuildChecklist(p, nil, status)
			if err != nil {
				t.Fatal(err)
			}
			for _, s := range c.Stages {
				for _, task := range s.Tasks {
					if task.CanComplete {
						t.Fatal(status)
					}
				}
			}
		}
	}
}

func TestTaskProgressHarvestedOnlyLastStageAndPartial(t *testing.T) {
	c := scheduleContent(win(WindowNone, "", nil, nil))
	c.Stages = append(c.Stages, Stage{Seq: 2, Name: "Cuối vụ"})
	c.Steps[1].StageSeq = 2
	c.Steps[1].TaskSeq = 1
	p, err := CalculateSchedule(c, scheduleDay("2024-03-01"), scheduleDay("2024-03-10"), nil)
	if err != nil {
		t.Fatal(err)
	}
	r, err := BuildChecklist(p, []TaskRecord{{StepSeq: 2, State: "partial", DoneOn: "2024-03-01"}}, "completed")
	if err != nil {
		t.Fatal(err)
	}
	if r.Stages[0].Tasks[0].CanComplete || !r.Stages[1].Tasks[0].CanComplete || r.Stages[1].Status != "in_progress" {
		t.Fatal(r)
	}
	if _, err = BuildChecklist(p, []TaskRecord{{StepSeq: 2, State: "done", DoneOn: "2030-01-01"}}, "open"); err == nil {
		t.Fatal("future completion accepted")
	}
}

// AH-124: đếm việc Đến hạn / Trễ chưa xong; việc Không ngày, đã xong, bỏ qua không tính.
func TestTaskProgressDueOverdueCounts(t *testing.T) {
	for _, tc := range []struct {
		name         string
		w            *Window
		asOf         string
		records      []TaskRecord
		due, overdue int
	}{
		{"đến hạn", win(WindowDateRange, AnchorNXG, ip(-1), ip(2)), "2024-03-02", nil, 1, 0},
		{"trễ", win(WindowDateRange, AnchorNXG, ip(-1), ip(2)), "2024-03-10", nil, 0, 1},
		{"làm dở vẫn trễ", win(WindowDateRange, AnchorNXG, ip(-1), ip(2)), "2024-03-10", []TaskRecord{{StepSeq: 2, State: "partial", DoneOn: "2024-03-05"}}, 0, 1},
		{"đã xong", win(WindowDateRange, AnchorNXG, ip(-1), ip(2)), "2024-03-10", []TaskRecord{{StepSeq: 2, State: "done", DoneOn: "2024-03-05"}}, 0, 0},
		{"bỏ qua", win(WindowDateRange, AnchorNXG, ip(-1), ip(2)), "2024-03-10", []TaskRecord{{StepSeq: 2, State: "skipped", DoneOn: "2024-03-05"}}, 0, 0},
		{"không ngày", win(WindowNone, "", nil, nil), "2024-03-10", nil, 0, 0},
		{"cửa sổ mở", win(WindowOpen, AnchorNXG, ip(1), nil), "2024-03-10", nil, 1, 0},
	} {
		p, err := CalculateSchedule(scheduleContent(tc.w), scheduleDay("2024-03-01"), scheduleDay(tc.asOf), nil)
		if err != nil {
			t.Fatal(err)
		}
		c, err := BuildChecklist(p, tc.records, "open")
		if err != nil {
			t.Fatal(err)
		}
		if c.Due != tc.due || c.Overdue != tc.overdue {
			t.Fatalf("%s: due=%d overdue=%d", tc.name, c.Due, c.Overdue)
		}
	}
}
