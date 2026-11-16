package domain

import (
	"errors"
	"strconv"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/mass"
)

// task dựng một việc trong giai đoạn 1 với cửa sổ Theo ngày so với NXG.
func task(seq int, act string, from, to int) Step {
	return Step{Seq: seq, StageSeq: 1, TaskSeq: seq, Name: "Việc " + act + strconv.Itoa(seq), ActivityCode: act, Mandatory: true,
		Window: &Window{Kind: WindowDateRange, Anchor: AnchorNXG, From: &from, To: &to}}
}

func oneStage(steps ...Step) Content {
	return Content{Stages: []Stage{{Seq: 1, Name: "Giai đoạn 1"}}, Steps: steps}
}

// HTX US-CAT-01 AC-6: thứ tự việc SOP phải liên tục 1..n.
func TestNormalizeContentContiguousSeq(t *testing.T) {
	ok := oneStage(task(1, ActLamDat, -5, -3), task(2, ActGieoSa, 0, 1), task(3, ActThuHoach, 95, 100))
	if _, err := NormalizeContent(ok); err != nil {
		t.Fatalf("việc hợp lệ bị từ chối: %v", err)
	}
	for name, steps := range map[string][]Step{
		"nhảy số":    {task(1, ActLamDat, 0, 1), task(3, ActThuHoach, 90, 93)},
		"bắt đầu 0":  {task(0, ActLamDat, 0, 1)},
		"đảo thứ tự": {task(2, ActLamDat, 0, 1), task(1, ActThuHoach, 90, 93)},
		"trùng số":   {task(1, ActLamDat, 0, 1), task(1, ActThuHoach, 90, 93)},
	} {
		_, err := NormalizeContent(oneStage(steps...))
		var se StepOrderError
		if !errors.As(err, &se) {
			t.Errorf("%s: muốn StepOrderError, có %v", name, err)
		}
	}
	bad := oneStage(task(1, "nhay_mua", 0, 1))
	if _, err := NormalizeContent(bad); err == nil {
		t.Error("hoạt động lạ phải bị từ chối")
	}
	ev := oneStage(task(1, ActBonPhan, 0, 1))
	ev.Steps[0].EvidenceKinds = []string{"anh_hien_truong", "anh_hien_truong"}
	out, err := NormalizeContent(ev)
	if err != nil || len(out.Steps[0].EvidenceKinds) != 1 {
		t.Errorf("gộp loại bằng chứng trùng: %v %v", out, err)
	}
}

// HTX US-YIELD-01 AC-1: cổng thu hoạch = mốc gốc + Từ của việc Thu hoạch cuối (Q-06).
func TestDatesForHarvestGate(t *testing.T) {
	sow := Day(2026, 9, 1)
	c, err := NormalizeContent(oneStage(task(1, ActGieoSa, 0, 1), task(2, ActThuHoach, 95, 98), task(3, ActThuHoach, 100, 105)))
	if err != nil {
		t.Fatal(err)
	}
	h, ok := HarvestStep(c.Steps)
	if !ok || h.Seq != 3 {
		t.Fatalf("việc Thu hoạch cuối: %+v", h)
	}
	d := DatesFor(sow, h)
	if FormatDay(d.ExpectedHarvest) != "2026-12-10" || FormatDay(d.HarvestOpen) != "2026-12-10" {
		t.Errorf("mốc: %s %s", FormatDay(d.ExpectedHarvest), FormatDay(d.HarvestOpen))
	}
}

// HTX US-SEASON-01 AC-2: ngày sạ trong [hôm nay − 15, hôm nay + 30].
func TestCheckSowingDate(t *testing.T) {
	today := Day(2026, 9, 29)
	for off, want := range map[int]bool{-16: false, -15: true, 0: true, 30: true, 31: false} {
		err := CheckSowingDate(AddDays(today, off), today)
		if (err == nil) != want {
			t.Errorf("lệch %d ngày: err=%v, muốn hợp lệ=%v", off, err, want)
		}
	}
}

// HTX US-GPS-01/02: nhãn vị trí và nguồn xác thực.
func TestGPSNormalizeAndStatus(t *testing.T) {
	lon, lat := decimal.RequireFromString("105.6"), decimal.RequireFromString("10.05")
	g := GPS{Lon: &lon, Lat: &lat}
	if err := g.Normalize(); err != nil || g.Source != GPSDevice {
		t.Fatalf("mặc định device: %v %s", err, g.Source)
	}
	g = GPS{}
	if err := g.Normalize(); err != nil || g.Source != GPSDenied {
		t.Fatalf("không toạ độ → denied: %v %s", err, g.Source)
	}
	g = GPS{Source: GPSPhotoEXIF}
	if err := g.Normalize(); err == nil {
		t.Error("photo_exif thiếu toạ độ phải lỗi")
	}
	g = GPS{Source: GPSManualPick, Lon: &lon, Lat: &lat}
	if err := g.Normalize(); err == nil {
		t.Error("manual_pick kèm toạ độ phải lỗi")
	}
	tol := DefaultGPSToleranceM
	d50, d51 := decimal.NewFromInt(50), decimal.NewFromInt(51)
	if LocationStatus(&d50, tol) != LocMatch || LocationStatus(&d51, tol) != LocMismatch || LocationStatus(nil, tol) != LocMissing {
		t.Error("nhãn vị trí sai")
	}
}

// HTX US-YIELD-03: 0 → cần xác nhận; vượt ngưỡng → cần xác nhận.
func TestYieldAndAnomaly(t *testing.T) {
	area := decimal.RequireFromString("1.5")
	y := YieldTPerHa(mass.MustKg("9750"), area)
	if y.String() != "6.5" {
		t.Errorf("năng suất: %s", y)
	}
	if Anomaly(mass.MustKg("0"), decimal.Zero, DefaultYieldAnomalyMax) != AnomalyZero {
		t.Error("0 phải là bất thường")
	}
	if Anomaly(mass.MustKg("30000"), YieldTPerHa(mass.MustKg("30000"), area), DefaultYieldAnomalyMax) != AnomalyAbove {
		t.Error("20 t/ha phải vượt ngưỡng 12")
	}
	if Anomaly(mass.MustKg("9750"), y, DefaultYieldAnomalyMax) != "" {
		t.Error("6.5 t/ha không bất thường")
	}
	if ExpectedKg(area, decimal.RequireFromString("6.75")).String() != "10125" {
		t.Error("sản lượng dự kiến")
	}
}

func TestNotePII(t *testing.T) {
	if _, err := NormalizeNote("note", "Gọi anh Ba 0912345678"); err == nil {
		t.Error("ghi chú có SĐT phải bị từ chối")
	}
	if _, err := NormalizeNote("note", "Bón NPK 20-20-15, 50 kg"); err != nil {
		t.Errorf("ghi chú hợp lệ: %v", err)
	}
	if _, err := NormalizeInputs([]Input{{Name: "Urê", Qty: decimal.RequireFromString("1.12345"), Unit: "kg"}}); err == nil {
		t.Error("quá 4 chữ số lẻ phải lỗi")
	}
	if NameKey("  SOP   Lúa  OM5451 ") != "sop lúa om5451" {
		t.Error("NameKey")
	}
}
