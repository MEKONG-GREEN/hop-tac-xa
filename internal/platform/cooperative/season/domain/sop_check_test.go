package domain

import (
	"errors"
	"slices"
	"testing"
)

// [AH-38 FN-01.4/AC-04] CheckContent trả mọi lỗi (cả thứ tự lẫn theo trường); NormalizeContent giữ hành vi cũ.
func TestCheckContent_CollectsOrderAndFieldErrors(t *testing.T) {
	c := Content{
		Stages: []Stage{
			{Seq: 1, Name: "Làm đất"},
			{Seq: 3, Name: "Chăm sóc", FromDay: ip(9), ToDay: ip(2)},
		},
		Steps: []Step{
			{Seq: 1, StageSeq: 1, TaskSeq: 1, Name: "", ActivityCode: ActLamDat, Window: win(WindowNone, "", nil, nil)},
			{Seq: 2, StageSeq: 2, TaskSeq: 4, Name: "Bón", ActivityCode: ActBonPhan, Window: win(WindowDateRange, AnchorNXG, ip(5), ip(1))},
		},
	}
	_, errs := CheckContent(c)
	got := make([]string, len(errs))
	for i, e := range errs {
		got[i] = e.Field
	}
	for _, want := range []string{"stages[1].seq", "steps[1].task_seq", "stages[1].to_day", "steps[0].name", "steps[1].window.to"} {
		if !slices.Contains(got, want) {
			t.Errorf("thiếu lỗi %s trong %v", want, got)
		}
	}
	for _, e := range errs {
		if e.Field == "stages[1].seq" && e.Reason != MsgStepOrder {
			t.Errorf("lỗi thứ tự phải mang MsgStepOrder, có %q", e.Reason)
		}
	}

	_, err := NormalizeContent(c)
	var so StepOrderError
	if !errors.As(err, &so) || so.Field != "stages[1].seq" {
		t.Fatalf("NormalizeContent phải trả StepOrderError của lỗi đầu tiên, có %T %v", err, err)
	}
}

func TestCheckContent_ValidMatchesNormalize(t *testing.T) {
	c := Content{
		Stages: []Stage{{Seq: 1, Name: "Làm đất", FromDay: ip(0), ToDay: ip(10)}},
		Steps:  []Step{{Seq: 1, StageSeq: 1, TaskSeq: 1, Name: "Cày", ActivityCode: ActLamDat, Window: win(WindowStage, "", nil, nil)}},
	}
	got, errs := CheckContent(c)
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	want, err := NormalizeContent(c)
	if err != nil || len(got.Steps) != 1 || got.Steps[0].DayOffset != want.Steps[0].DayOffset || got.Steps[0].Undated != want.Steps[0].Undated {
		t.Fatalf("khác nhau: %+v vs %+v (%v)", got, want, err)
	}
}

func TestActivityByLabel(t *testing.T) {
	for in, want := range map[string]string{"bón phân": ActBonPhan, " Thu  hoạch ": ActThuHoach, "Phun thuốc - dưỡng": ActPhunThuoc} {
		if got, ok := ActivityByLabel(in); !ok || got != want {
			t.Errorf("ActivityByLabel(%q) = %q,%v; muốn %q", in, got, ok, want)
		}
	}
	if _, ok := ActivityByLabel("Tưới tắm"); ok {
		t.Error("nhãn lạ phải trả false")
	}
	labels := ActivityLabels()
	if len(labels) != len(activities) || labels[0] != "Làm đất" {
		t.Errorf("ActivityLabels: %v", labels)
	}
}
