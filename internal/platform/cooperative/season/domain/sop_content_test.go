package domain

import (
	"errors"
	"slices"
	"testing"

	"github.com/shopspring/decimal"
)

func ip(n int) *int { return &n }

func win(kind, anchor string, from, to *int) *Window {
	return &Window{Kind: kind, Anchor: anchor, From: from, To: to}
}

func fieldsOf(t *testing.T, err error) []string {
	t.Helper()
	var fe FieldErrors
	if !errors.As(err, &fe) {
		t.Fatalf("muốn FieldErrors, có %T %v", err, err)
	}
	out := make([]string, len(fe))
	for i, f := range fe {
		out[i] = f.Field
	}
	return out
}

func mat(code string, dose string, unit string) Material {
	return Material{MaterialCode: code, DosePerHa: decimal.RequireFromString(dose), Unit: unit}
}

// AC2: gom mọi lỗi theo trường một lần, không dừng ở lỗi đầu.
func TestNormalizeContent_CollectsAllFieldErrors(t *testing.T) {
	c := Content{
		Stages: []Stage{
			{Seq: 1, Name: "Làm đất", FromDay: ip(10), ToDay: ip(2)},
			{Seq: 2, Name: " làm  ĐẤT "},
			{Seq: 3, Name: "Chăm sóc", FromDay: ip(5)},
		},
		Steps: []Step{
			{Seq: 1, StageSeq: 1, TaskSeq: 1, Name: "Cày", ActivityCode: ActLamDat, Window: win(WindowDateRange, AnchorNXG, ip(5), ip(1)),
				Materials: []Material{mat("URE", "0", "kg"), mat("URE", "2", "")}},
			{Seq: 2, StageSeq: 1, TaskSeq: 2, Name: "cày", ActivityCode: ActLamDat, Window: win(WindowNone, "", nil, nil)},
		},
	}
	_, err := NormalizeContent(c)
	got := fieldsOf(t, err)
	for _, want := range []string{
		"stages[0].to_day", "stages[1].name", "stages[2].to_day", "steps[0].window.to",
		"steps[0].materials[0].dose_per_ha", "steps[0].materials[1].material_code", "steps[0].materials[1].unit", "steps[1].name",
	} {
		if !slices.Contains(got, want) {
			t.Errorf("thiếu lỗi trường %s trong %v", want, got)
		}
	}
}

func TestNormalizeContent_OrderErrors(t *testing.T) {
	stages := []Stage{{Seq: 1, Name: "A"}, {Seq: 3, Name: "B"}}
	_, err := NormalizeContent(Content{Stages: stages})
	var so StepOrderError
	if !errors.As(err, &so) || so.Field != "stages[1].seq" {
		t.Fatalf("giai đoạn seq 1,3: %v", err)
	}
	c := oneStage(task(1, ActLamDat, 0, 1), task(2, ActGieoSa, 0, 1))
	c.Steps[1].TaskSeq = 3
	_, err = NormalizeContent(c)
	if !errors.As(err, &so) || so.Field != "steps[1].task_seq" {
		t.Fatalf("task_seq nhảy số: %v", err)
	}
	c = Content{Stages: []Stage{{Seq: 1, Name: "A"}, {Seq: 2, Name: "B"}}, Steps: []Step{
		{Seq: 1, StageSeq: 2, TaskSeq: 1, Name: "x", ActivityCode: ActKhac, Window: win(WindowNone, "", nil, nil)},
		{Seq: 2, StageSeq: 1, TaskSeq: 1, Name: "y", ActivityCode: ActKhac, Window: win(WindowNone, "", nil, nil)},
	}}
	if _, err = NormalizeContent(c); !errors.As(err, &so) || so.Field != "steps[1].stage_seq" {
		t.Fatalf("việc không xếp theo giai đoạn: %v", err)
	}
}

func TestNormalizeContent_SingleSeasonSummary(t *testing.T) {
	c := oneStage(task(1, ActKiemTra, 0, 1), task(2, ActKiemTra, 2, 3))
	c.Steps[0].SeasonSummary, c.Steps[1].SeasonSummary = true, true
	_, err := NormalizeContent(c)
	if got := fieldsOf(t, err); !slices.Equal(got, []string{"steps[1].season_summary"}) {
		t.Fatalf("tối đa một việc tổng kết vụ: %v", got)
	}
}

// AC3: bốn loại cửa sổ và ràng buộc trường theo từng loại.
func TestWindowKinds_AH8_AC3(t *testing.T) {
	staged := func(w *Window, withRange bool) Content {
		c := oneStage(Step{Seq: 1, StageSeq: 1, TaskSeq: 1, Name: "x", ActivityCode: ActKhac, Window: w})
		if withRange {
			c.Stages[0].FromDay, c.Stages[0].ToDay = ip(3), ip(9)
		}
		return c
	}
	cases := []struct {
		name  string
		w     *Window
		stage bool
		field string // rỗng = hợp lệ
	}{
		{"theo ngày NXG âm", win(WindowDateRange, AnchorNXG, ip(-5), ip(-1)), false, ""},
		{"theo ngày NTH", win(WindowDateRange, AnchorNTH, ip(-10), ip(-7)), false, ""},
		{"theo ngày NTT", win(WindowDateRange, AnchorNTT, ip(0), ip(3)), false, ""},
		{"theo ngày thiếu mốc", win(WindowDateRange, "", ip(0), ip(3)), false, "steps[0].window.anchor"},
		{"theo ngày thiếu Đến", win(WindowDateRange, AnchorNXG, ip(0), nil), false, "steps[0].window.to"},
		{"theo ngày Từ > Đến", win(WindowDateRange, AnchorNXG, ip(4), ip(3)), false, "steps[0].window.to"},
		{"mở", win(WindowOpen, AnchorNXG, ip(7), nil), false, ""},
		{"mở có Đến", win(WindowOpen, AnchorNXG, ip(7), ip(9)), false, "steps[0].window.to"},
		{"mở thiếu Từ", win(WindowOpen, AnchorNXG, nil, nil), false, "steps[0].window.from"},
		{"theo giai đoạn", win(WindowStage, "", nil, nil), true, ""},
		{"theo giai đoạn chưa có khoảng ngày", win(WindowStage, "", nil, nil), false, "steps[0].window.kind"},
		{"theo giai đoạn kèm Từ", win(WindowStage, "", ip(1), nil), true, "steps[0].window.from"},
		{"không ngày", win(WindowNone, "", nil, nil), false, ""},
		{"không ngày kèm mốc", win(WindowNone, AnchorNXG, nil, nil), false, "steps[0].window.anchor"},
		{"thiếu cửa sổ", nil, false, "steps[0].window.kind"},
		{"loại lạ", win("lung_tung", "", nil, nil), false, "steps[0].window.kind"},
	}
	for _, tc := range cases {
		_, err := NormalizeContent(staged(tc.w, tc.stage))
		if tc.field == "" {
			if err != nil {
				t.Errorf("%s: bị từ chối: %v", tc.name, err)
			}
			continue
		}
		if got := fieldsOf(t, err); !slices.Contains(got, tc.field) {
			t.Errorf("%s: muốn lỗi %s, có %v", tc.name, tc.field, got)
		}
	}
}

// Quy ước tạm của day_offset/window_days/undated (tới AH-71).
func TestNormalizeContent_LegacyFields(t *testing.T) {
	c := Content{Stages: []Stage{{Seq: 1, Name: "A", FromDay: ip(20), ToDay: ip(40)}}, Steps: []Step{
		{Seq: 1, StageSeq: 1, TaskSeq: 1, Name: "nxg", ActivityCode: ActBonPhan, DayOffset: 99, WindowDays: 9, Window: win(WindowDateRange, AnchorNXG, ip(7), ip(12))},
		{Seq: 2, StageSeq: 1, TaskSeq: 2, Name: "nth", ActivityCode: ActKiemTra, Window: win(WindowDateRange, AnchorNTH, ip(-10), ip(-5))},
		{Seq: 3, StageSeq: 1, TaskSeq: 3, Name: "giai đoạn", ActivityCode: ActKiemTra, Window: win(WindowStage, "", nil, nil)},
		{Seq: 4, StageSeq: 1, TaskSeq: 4, Name: "ntt", ActivityCode: ActKiemTra, Window: win(WindowOpen, AnchorNTT, ip(3), nil)},
		{Seq: 5, StageSeq: 1, TaskSeq: 5, Name: "không ngày", ActivityCode: ActKhac, Window: win(WindowNone, "", nil, nil)},
		{Seq: 6, StageSeq: 1, TaskSeq: 6, Name: "thu hoạch", ActivityCode: ActThuHoach, Window: win(WindowDateRange, AnchorNXG, ip(100), ip(105))},
	}}
	out, err := NormalizeContent(c)
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		off     int
		undated bool
	}{{7, false}, {90, false}, {20, false}, {0, true}, {0, true}, {100, false}}
	for i, w := range want {
		s := out.Steps[i]
		if s.DayOffset != w.off || s.WindowDays != 0 || s.Undated != w.undated {
			t.Errorf("việc %d: day_offset=%d window_days=%d undated=%v, muốn %d/0/%v", i+1, s.DayOffset, s.WindowDays, s.Undated, w.off, w.undated)
		}
	}
}

// AC5: chặn phát hành (EX-05, Q-06).
func TestCheckPublishable(t *testing.T) {
	harvest := func(w *Window) Content {
		c := oneStage(task(1, ActGieoSa, 0, 1), Step{Seq: 2, StageSeq: 1, TaskSeq: 2, Name: "Thu", ActivityCode: ActThuHoach, Window: w})
		c.Stages[0].FromDay, c.Stages[0].ToDay = ip(90), ip(110)
		return c
	}
	cases := []struct {
		name  string
		c     Content
		kind  string
		field string
	}{
		{"rỗng", Content{}, BlockEmpty, "stages"},
		{"giai đoạn chưa có việc", Content{Stages: []Stage{{Seq: 1, Name: "A"}, {Seq: 2, Name: "B"}}, Steps: oneStage(task(1, ActThuHoach, 90, 95)).Steps}, BlockEmpty, "stages[1]"},
		{"không có việc thu hoạch", oneStage(task(1, ActGieoSa, 0, 1)), BlockNoHarvest, "steps"},
		{"thu hoạch cuối mở", harvest(win(WindowOpen, AnchorNXG, ip(95), nil)), BlockHarvestWindow, "steps[1].window"},
		{"thu hoạch cuối không ngày", harvest(win(WindowNone, "", nil, nil)), BlockHarvestWindow, "steps[1].window"},
		{"thu hoạch cuối mốc NTH", harvest(win(WindowDateRange, AnchorNTH, ip(0), ip(1))), BlockHarvestWindow, "steps[1].window"},
		{"thu hoạch cuối mốc NTT", harvest(win(WindowDateRange, AnchorNTT, ip(0), ip(1))), BlockHarvestWindow, "steps[1].window"},
		{"việc dùng NTH nhưng thu hoạch không ngày", func() Content {
			c := harvest(win(WindowNone, "", nil, nil))
			c.Steps[0].Window = win(WindowDateRange, AnchorNTH, ip(-5), ip(-1))
			return c
		}(), BlockHarvestWindow, "steps[1].window"},
		{"thu hoạch theo ngày NXG", harvest(win(WindowDateRange, AnchorNXG, ip(95), ip(100))), "", ""},
		{"thu hoạch theo giai đoạn", harvest(win(WindowStage, "", nil, nil)), "", ""},
	}
	for _, tc := range cases {
		err := CheckPublishable(tc.c)
		if tc.kind == "" {
			if err != nil {
				t.Errorf("%s: bị chặn: %v", tc.name, err)
			}
			continue
		}
		var pb PublishBlock
		if !errors.As(err, &pb) || pb.Kind != tc.kind || pb.Field != tc.field {
			t.Errorf("%s: muốn %s/%s, có %v", tc.name, tc.kind, tc.field, err)
		}
	}
}

func TestNormalizeContent_MaterialRules(t *testing.T) {
	c := oneStage(task(1, ActBonPhan, 0, 1))
	c.Steps[0].Materials = []Material{
		mat("URE", "50.5", "kg"),
		{Name: "Phân tự pha", DosePerHa: decimal.RequireFromString("1.25"), Unit: "lít"},
		{Name: " phân  TỰ pha", DosePerHa: decimal.RequireFromString("1"), Unit: "lít"},
		mat("DAP", "1.12345", "kg"),
		{DosePerHa: decimal.NewFromInt(1), Unit: "kg"},
	}
	_, err := NormalizeContent(c)
	got := fieldsOf(t, err)
	for _, want := range []string{"steps[0].materials[2].material_code", "steps[0].materials[3].dose_per_ha", "steps[0].materials[4].name"} {
		if !slices.Contains(got, want) {
			t.Errorf("thiếu lỗi %s trong %v", want, got)
		}
	}
	if slices.Contains(got, "steps[0].materials[0].dose_per_ha") || slices.Contains(got, "steps[0].materials[1].material_code") {
		t.Errorf("dòng hợp lệ bị báo lỗi: %v", got)
	}
}
