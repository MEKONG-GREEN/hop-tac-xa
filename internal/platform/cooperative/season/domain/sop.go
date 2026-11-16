package domain

import (
	"strconv"
	"time"
)

// Phạm vi và trạng thái SOP.
const (
	ScopeSystem = "system"
	ScopeHTX    = "htx"

	TemplateDraft     = "draft"
	TemplatePublished = "published"
	TemplateRetired   = "retired"

	VersionDraft     = "draft"
	VersionPublished = "published"
)

// Trạng thái hiển thị của phiên bản (suy từ dữ liệu, không lưu cột): Nháp / Đang áp dụng / Ngừng áp dụng.
const (
	VersionStateDraft   = "draft"
	VersionStateActive  = "active"
	VersionStateRetired = "retired"
)

// VersionState là chỗ duy nhất suy trạng thái hiển thị của phiên bản (AH-21 AC2): bản nháp là draft; bản đã phát hành
// mới nhất của mẫu chưa ngừng là active; mọi bản đã phát hành còn lại là retired.
func VersionState(versionStatus string, versionNo int, templateStatus string, publishedVersion *int) string {
	switch {
	case versionStatus != VersionPublished:
		return VersionStateDraft
	case templateStatus != TemplateRetired && publishedVersion != nil && versionNo == *publishedVersion:
		return VersionStateActive
	}
	return VersionStateRetired
}

// Giới hạn của bước SOP.
const (
	MaxSteps       = 100
	MaxPHIDays     = 120
	DefaultWindow  = 3
	MaxStepNameLen = 200
)

// Step là một việc của quy trình SOP; Seq đánh số toàn cục 1..n, StageSeq/TaskSeq xác định
// giai đoạn và thứ tự trong giai đoạn. DayOffset và WindowDays là hai trường cũ do server
// tính theo quy ước tạm (xem computeLegacy); Undated = việc chưa có ngày trước khi vụ chạy.
type Step struct {
	Seq           int        `json:"seq"`
	Name          string     `json:"name"`
	StageCode     string     `json:"stage_code"`
	ActivityCode  string     `json:"activity_code"`
	DayOffset     int        `json:"day_offset"`
	WindowDays    int        `json:"window_days"`
	Mandatory     bool       `json:"mandatory"`
	EvidenceKinds []string   `json:"evidence_kinds"`
	PHIDays       *int       `json:"phi_days,omitempty"`
	Instruction   string     `json:"instruction,omitempty"`
	StageSeq      int        `json:"stage_seq"`
	TaskSeq       int        `json:"task_seq"`
	Window        *Window    `json:"window,omitempty"`
	Materials     []Material `json:"materials,omitempty"`
	SeasonSummary bool       `json:"season_summary,omitempty"`
	Undated       bool       `json:"undated,omitempty"`
}

// MsgStepOrder là thông báo của HTX US-CAT-01 AC-6 khi thứ tự giai đoạn/việc không liên tục.
const MsgStepOrder = "Thứ tự bước không hợp lệ, vui lòng kiểm tra lại."

// StepOrderError: thứ tự giai đoạn hoặc việc không liên tục; Field là trường sai.
type StepOrderError struct {
	Index int
	Field string
}

func (e StepOrderError) Error() string {
	return "cropcycle: thứ tự không liên tục tại " + e.Field + " (vị trí " + strconv.Itoa(e.Index+1) + ")"
}

// HarvestStep trả việc Thu hoạch cuối: việc thu_hoach có seq lớn nhất và đã có ngày
// (Q-05, Q-06); ok = false nếu không có.
func HarvestStep(steps []Step) (Step, bool) {
	var h Step
	ok := false
	for _, s := range steps {
		if s.ActivityCode == ActThuHoach && !s.Undated && (!ok || s.Seq > h.Seq) {
			h, ok = s, true
		}
	}
	return h, ok
}

// StepBySeq trả bước theo seq.
func StepBySeq(steps []Step, seq int) (Step, bool) {
	if seq >= 1 && seq <= len(steps) && steps[seq-1].Seq == seq {
		return steps[seq-1], true
	}
	for _, s := range steps {
		if s.Seq == seq {
			return s, true
		}
	}
	return Step{}, false
}

// SeasonDates là các mốc của vụ suy từ SOP đã ghim.
type SeasonDates struct {
	ExpectedHarvest time.Time // mốc gốc + Từ của việc Thu hoạch cuối
	HarvestOpen     time.Time // sớm nhất được khai sản lượng (= ExpectedHarvest khi cửa sổ thu hoạch = 0)
}

// DatesFor tính mốc vụ từ ngày mốc gốc và việc Thu hoạch cuối (g2-conventions: cổng thu
// hoạch theo SOP đã ghim, không cứng 60 ngày). HarvestOpen không sớm hơn ngày mốc gốc.
func DatesFor(sowing time.Time, harvest Step) SeasonDates {
	d := SeasonDates{
		ExpectedHarvest: AddDays(sowing, harvest.DayOffset),
		HarvestOpen:     AddDays(sowing, harvest.DayOffset-harvest.WindowDays),
	}
	if d.ExpectedHarvest.Before(sowing) {
		d.ExpectedHarvest = sowing
	}
	if d.HarvestOpen.Before(sowing) {
		d.HarvestOpen = sowing
	}
	return d
}
