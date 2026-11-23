package domain

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/shopspring/decimal"
)

// customBase: hai giai đoạn, ba việc gốc (seq 1, 2 ở giai đoạn 1; seq 3 ở giai đoạn 2).
func customBase() SeasonSnapshot {
	return SeasonSnapshot{
		Stages: []Stage{{Seq: 1, Name: "Làm đất", FromDay: ip(-5), ToDay: ip(0)}, {Seq: 2, Name: "Chăm sóc", FromDay: ip(1), ToDay: ip(60)}},
		Steps: []Step{
			{Seq: 1, StageSeq: 1, TaskSeq: 1, Name: "Cày", ActivityCode: ActLamDat, Window: win(WindowDateRange, AnchorNXG, ip(-5), ip(-3))},
			{Seq: 2, StageSeq: 1, TaskSeq: 2, Name: "Bừa", ActivityCode: ActLamDat, Window: win(WindowDateRange, AnchorNXG, ip(-2), ip(0))},
			{Seq: 3, StageSeq: 2, TaskSeq: 1, Name: "Bón lót", ActivityCode: ActBonPhan, Window: win(WindowDateRange, AnchorNXG, ip(5), ip(10)),
				Materials: []Material{{Name: "NPK", DosePerHa: decimal.RequireFromString("100"), Unit: "kg"}}},
		},
	}
}

func applyOK(t *testing.T, s SeasonSnapshot, ch PlanChange, resolved, logged map[int]bool) (SeasonSnapshot, string) {
	t.Helper()
	next, summary, err := s.Apply(ch, resolved, logged)
	if err != nil {
		t.Fatalf("%+v: %v", ch, err)
	}
	return next, summary
}

func stageSeqs(t *testing.T, s SeasonSnapshot, stage int) []int {
	t.Helper()
	plan, err := s.Schedule(scheduleDay("2026-12-01"), scheduleDay("2026-12-01"), nil)
	if err != nil {
		t.Fatal(err)
	}
	out := []int{}
	for _, st := range plan.Stages[stage-1].Steps {
		out = append(out, st.Seq)
	}
	return out
}

func TestSeasonPlanAddMoveKeepsStableSeq(t *testing.T) {
	s := customBase()
	s, summary := applyOK(t, s, PlanChange{Kind: PlanAdd, Step: &NewStep{Name: "Phun thuốc bổ sung", StageSeq: 1, ActivityCode: ActPhunThuoc,
		Window: win(WindowDateRange, AnchorNXG, ip(-1), ip(0))}}, nil, nil)
	if summary != `Thêm việc "Phun thuốc bổ sung" vào giai đoạn "Làm đất"` {
		t.Fatalf("summary %q", summary)
	}
	// Việc thêm nhận seq 4 (sau seq lớn nhất), nằm cuối giai đoạn 1, có nhãn custom; SOP gốc không đổi.
	if got := stageSeqs(t, s, 1); !slices.Equal(got, []int{1, 2, 4}) {
		t.Fatalf("giai đoạn 1: %v", got)
	}
	if len(s.Steps) != 3 || !s.IsCustom(4) || s.IsCustom(1) {
		t.Fatalf("snapshot: %+v", s)
	}
	plan, _ := s.Schedule(scheduleDay("2026-12-01"), scheduleDay("2026-12-01"), nil)
	if st := plan.Stages[0].Steps[2]; !st.Custom || st.TaskSeq != 3 || st.Name != "Phun thuốc bổ sung" {
		t.Fatalf("việc thêm: %+v", st)
	}
	// Đổi chỗ chỉ đổi thứ tự hiển thị: seq giữ nguyên.
	s, _ = applyOK(t, s, PlanChange{Kind: PlanMove, StepSeq: 4, Direction: "up"}, nil, nil)
	s, _ = applyOK(t, s, PlanChange{Kind: PlanMove, StepSeq: 4, Direction: "up"}, nil, nil)
	if got := stageSeqs(t, s, 1); !slices.Equal(got, []int{4, 1, 2}) {
		t.Fatalf("sau khi đổi chỗ: %v", got)
	}
	if _, _, err := s.Apply(PlanChange{Kind: PlanMove, StepSeq: 4, Direction: "up"}, nil, nil); err == nil {
		t.Fatal("việc đầu giai đoạn không lên được nữa")
	}
	// Xóa việc tự thêm rồi thêm việc mới: seq không dùng lại.
	s, summary = applyOK(t, s, PlanChange{Kind: PlanRemove, StepSeq: 4}, nil, nil)
	if summary != `Xóa việc tự thêm "Phun thuốc bổ sung"` || s.IsCustom(4) {
		t.Fatalf("xóa: %q %+v", summary, s.Custom)
	}
	s, _ = applyOK(t, s, PlanChange{Kind: PlanAdd, Step: &NewStep{Name: "Kiểm tra nước", StageSeq: 2, ActivityCode: ActKiemTra, Window: win(WindowNone, "", nil, nil)}}, nil, nil)
	if !s.IsCustom(5) {
		t.Fatalf("seq mới phải là 5: %+v", s.Custom)
	}
	// Bản chụp lưu/đọc lại qua JSON giữ nguyên tùy chỉnh.
	raw, _ := json.Marshal(s)
	var back SeasonSnapshot
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if got := stageSeqs(t, back, 1); !slices.Equal(got, []int{1, 2}) {
		t.Fatalf("đọc lại giai đoạn 1: %v", got)
	}
	if got := stageSeqs(t, back, 2); !slices.Equal(got, []int{3, 5}) {
		t.Fatalf("đọc lại giai đoạn 2: %v", got)
	}
}

func TestSeasonPlanEditRecalculatesAndValidates(t *testing.T) {
	s := customBase()
	s, summary := applyOK(t, s, PlanChange{Kind: PlanEdit, StepSeq: 3, Window: win(WindowDateRange, AnchorNXG, ip(20), ip(25)),
		Materials: []Material{{Name: "NPK", DosePerHa: decimal.RequireFromString("150"), Unit: "kg"}}}, nil, nil)
	if summary != `Sửa ngày/vật tư của việc "Bón lót"` {
		t.Fatalf("summary %q", summary)
	}
	plan, err := s.Schedule(scheduleDay("2026-12-01"), scheduleDay("2026-12-01"), nil)
	if err != nil {
		t.Fatal(err)
	}
	checkScheduleDay(t, "from", plan.Stages[1].Steps[0].FromDate, "2026-12-21")
	checkScheduleDay(t, "to", plan.Stages[1].Steps[0].ToDate, "2026-12-26")
	if st, _ := s.Step(3); !st.Materials[0].DosePerHa.Equal(decimal.RequireFromString("150")) || len(s.Steps[2].Materials) != 1 || !s.Steps[2].Materials[0].DosePerHa.Equal(decimal.RequireFromString("100")) {
		t.Fatalf("sửa vật tư chỉ đổi lớp tùy chỉnh: %+v / %+v", st, s.Steps[2])
	}
	// Cửa sổ Từ > Đến, liều ≤ 0, thiếu tên/giai đoạn: bị chặn với tên trường của việc.
	for _, tc := range []struct {
		ch    PlanChange
		field string
	}{
		{PlanChange{Kind: PlanEdit, StepSeq: 1, Window: win(WindowDateRange, AnchorNXG, ip(3), ip(1))}, "step.window.to"},
		{PlanChange{Kind: PlanEdit, StepSeq: 3, Window: win(WindowNone, "", nil, nil), Materials: []Material{{Name: "NPK", DosePerHa: decimal.Zero, Unit: "kg"}}}, "step.materials[0].dose_per_ha"},
		{PlanChange{Kind: PlanAdd, Step: &NewStep{Name: " ", StageSeq: 1, ActivityCode: ActKhac, Window: win(WindowNone, "", nil, nil)}}, "step.name"},
		{PlanChange{Kind: PlanAdd, Step: &NewStep{Name: "Cày", StageSeq: 1, ActivityCode: ActKhac, Window: win(WindowNone, "", nil, nil)}}, "step.name"},
		{PlanChange{Kind: PlanAdd, Step: &NewStep{Name: "Mới", StageSeq: 9, ActivityCode: ActKhac, Window: win(WindowNone, "", nil, nil)}}, "step.stage_seq"},
		{PlanChange{Kind: PlanAdd, Step: &NewStep{Name: "Mới", StageSeq: 1, ActivityCode: "", Window: win(WindowNone, "", nil, nil)}}, "step.activity_code"},
	} {
		_, _, err := customBase().Apply(tc.ch, nil, nil)
		var fes FieldErrors
		if !errors.As(err, &fes) || !slices.ContainsFunc(fes, func(fe FieldError) bool { return fe.Field == tc.field }) {
			t.Errorf("%+v: cần lỗi %s, có %v", tc.ch, tc.field, err)
		}
	}
}

func TestSeasonPlanResolvedStepsLockedAndSkip(t *testing.T) {
	s := customBase()
	done := map[int]bool{1: true}
	for _, ch := range []PlanChange{
		{Kind: PlanEdit, StepSeq: 1, Window: win(WindowNone, "", nil, nil)},
		{Kind: PlanMove, StepSeq: 1, Direction: "down"},
		{Kind: PlanRemove, StepSeq: 1},
	} {
		if _, _, err := s.Apply(ch, done, done); !errors.Is(err, ErrPlanStepResolved) {
			t.Errorf("%+v: việc đã xong phải bị từ chối, có %v", ch, err)
		}
	}
	if _, _, err := s.Apply(PlanChange{Kind: PlanRemove, StepSeq: 99}, nil, nil); !errors.Is(err, ErrPlanStepUnknown) {
		t.Fatalf("seq lạ: %v", err)
	}
	// Bỏ việc SOP → Bỏ qua (không xóa), không tích được, không tính vào mẫu số.
	s, summary := applyOK(t, s, PlanChange{Kind: PlanRemove, StepSeq: 2}, nil, nil)
	if summary != `Bỏ việc "Bừa" (Bỏ qua)` || len(s.Steps) != 3 || !s.SkippedSet()[2] {
		t.Fatalf("bỏ việc SOP: %q %+v", summary, s.Custom)
	}
	if _, _, err := s.Apply(PlanChange{Kind: PlanEdit, StepSeq: 2, Window: win(WindowNone, "", nil, nil)}, nil, nil); !errors.Is(err, ErrPlanStepSkipped) {
		t.Fatalf("việc đã bỏ qua: %v", err)
	}
	plan, err := s.Schedule(scheduleDay("2026-12-01"), scheduleDay("2026-12-01"), nil)
	if err != nil {
		t.Fatal(err)
	}
	records := []TaskRecord{{StepSeq: 1, StageSeq: 1, State: "done", DoneOn: "2026-11-27"}}
	check, err := BuildSeasonChecklist(plan, records, "open", s.SkippedSet())
	if err != nil {
		t.Fatal(err)
	}
	bua := check.Stages[0].Tasks[1]
	if bua.Seq != 2 || bua.CompletionStatus != "skipped" || bua.CanComplete || check.Skipped != 1 || check.Done != 1 || check.Total != 3 {
		t.Fatalf("checklist: %+v", check)
	}
	if check.Stages[0].Status != "done" || check.Percent == nil || *check.Percent != "50.00" {
		t.Fatalf("giai đoạn 1 xong (1 xong + 1 bỏ qua), tiến độ 1/2: %+v", check)
	}
	// AC5: thêm việc chưa xong vào giai đoạn đã xong → giai đoạn về Đang thực hiện.
	s, _ = applyOK(t, s, PlanChange{Kind: PlanAdd, Step: &NewStep{Name: "Cày bổ sung", StageSeq: 1, ActivityCode: ActLamDat, Window: win(WindowNone, "", nil, nil)}}, done, done)
	plan, _ = s.Schedule(scheduleDay("2026-12-01"), scheduleDay("2026-12-01"), nil)
	check, _ = BuildSeasonChecklist(plan, records, "open", s.SkippedSet())
	if check.Stages[0].Status != "in_progress" || check.Total != 4 {
		t.Fatalf("AC5: %+v", check.Stages[0])
	}
	// Việc tự thêm đã có nhật ký (kể cả chưa xong) không xóa được.
	if _, _, err := s.Apply(PlanChange{Kind: PlanRemove, StepSeq: 4}, nil, map[int]bool{4: true}); !errors.Is(err, ErrPlanStepResolved) {
		t.Fatalf("việc tự thêm có nhật ký: %v", err)
	}
}
