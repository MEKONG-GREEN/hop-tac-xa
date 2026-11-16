package domain

import (
	"strconv"
	"strings"

	"github.com/shopspring/decimal"
)

// Giới hạn của nội dung SOP dạng checklist (giai đoạn → việc → vật tư).
const (
	MinOffsetDay    = -365
	MaxOffsetDay    = 1825 // cây dài ngày tới 5 năm
	MaxStages       = 30
	MaxMaterials    = 20
	MaxStageNameLen = 200
	MaxMaterialName = 200
	MaxUnitLen      = 20
)

// MaxDosePerHa là liều khuyến cáo lớn nhất trên mỗi ha.
var MaxDosePerHa = decimal.NewFromInt(1_000_000)

// Loại cửa sổ thực hiện của việc (BR-04).
const (
	WindowDateRange = "date_range" // Theo ngày: Từ–Đến so với mốc
	WindowOpen      = "open"       // Mở: chỉ có Từ
	WindowStage     = "stage"      // Theo giai đoạn: lấy khoảng ngày của giai đoạn chứa việc
	WindowNone      = "none"       // Không ngày
)

// Mốc tính ngày: NXG = mốc gốc của SOP, NTH = ngày thu hoạch dự kiến, NTT = ngày thu hoạch thực tế.
const (
	AnchorNXG = "nxg"
	AnchorNTH = "nth"
	AnchorNTT = "ntt"
)

// Stage là một giai đoạn của SOP; FromDay/ToDay tính từ mốc gốc (cả hai hoặc không có).
type Stage struct {
	Seq     int    `json:"seq"`
	Name    string `json:"name"`
	FromDay *int   `json:"from_day"`
	ToDay   *int   `json:"to_day"`
}

// Window là cửa sổ thực hiện của một việc.
type Window struct {
	Kind   string `json:"kind"`
	Anchor string `json:"anchor,omitempty"`
	From   *int   `json:"from,omitempty"`
	To     *int   `json:"to,omitempty"`
}

// Material là một dòng vật tư – liều khuyến cáo (theo ha) của một việc.
type Material struct {
	MaterialCode string          `json:"material_code,omitempty"`
	Name         string          `json:"name,omitempty"`
	DosePerHa    decimal.Decimal `json:"dose_per_ha"`
	Unit         string          `json:"unit"`
}

// Content là nội dung một phiên bản SOP: giai đoạn và danh sách việc phẳng.
type Content struct {
	Stages []Stage
	Steps  []Step
}

// FieldErrors gom mọi lỗi theo trường của một lần kiểm.
type FieldErrors []FieldError

func (e FieldErrors) Error() string {
	parts := make([]string, len(e))
	for i, f := range e {
		parts[i] = f.Error()
	}
	return strings.Join(parts, "; ")
}

func idx(prefix string, i int, rest string) string {
	return prefix + "[" + strconv.Itoa(i) + "]" + rest
}

type contentCheck struct {
	errs   FieldErrors
	orders []StepOrderError
}

func (k *contentCheck) add(field, reason string) { k.errs = append(k.errs, FieldError{field, reason}) }

func (k *contentCheck) orderAt(i int, field string) {
	k.orders = append(k.orders, StepOrderError{Index: i, Field: field})
}

// Lý do lỗi dùng chung: nơi khác (nhập Excel) đổi sang cách gọi của người dùng dựa vào các hằng này.
const (
	MsgActivityChoice   = "phải là lam_dat, gieo_sa, tuoi_nuoc, bon_phan, phun_thuoc, kiem_tra, thu_hoach, cat_tia hoặc khac"
	MsgWindowKindNeeded = "bắt buộc: date_range, open, stage hoặc none"
	MsgWindowKindChoice = "phải là date_range, open, stage hoặc none"
	MsgAnchorChoice     = "bắt buộc: nxg, nth hoặc ntt"
)

func inRange(d int) bool { return d >= MinOffsetDay && d <= MaxOffsetDay }

const msgRange = "trong khoảng -365..1825 ngày so với mốc"

// NormalizeContent chuẩn hóa và kiểm nội dung SOP. Gom mọi lỗi theo trường (FieldErrors)
// thay vì dừng ở lỗi đầu; thứ tự giai đoạn/việc không liên tục trả StepOrderError.
// day_offset, window_days, undated do server tính, giá trị client gửi bị ghi đè.
func NormalizeContent(c Content) (Content, error) {
	k := &contentCheck{}
	out := Content{Stages: k.normalizeStages(c.Stages)}
	out.Steps = k.normalizeSteps(c.Steps, out.Stages)
	if len(k.orders) > 0 {
		return Content{}, k.orders[0]
	}
	if len(k.errs) > 0 {
		return Content{}, k.errs
	}
	computeLegacy(out)
	return out, nil
}

// CheckContent chạy đúng bộ luật của NormalizeContent nhưng trả mọi lỗi một lần: lỗi thứ tự
// (MsgStepOrder) đứng cùng lỗi theo trường. Không có lỗi thì trả nội dung đã chuẩn hóa.
func CheckContent(c Content) (Content, FieldErrors) {
	k := &contentCheck{}
	out := Content{Stages: k.normalizeStages(c.Stages)}
	out.Steps = k.normalizeSteps(c.Steps, out.Stages)
	errs := make(FieldErrors, 0, len(k.orders)+len(k.errs))
	for _, o := range k.orders {
		errs = append(errs, FieldError{o.Field, MsgStepOrder})
	}
	errs = append(errs, k.errs...)
	if len(errs) > 0 {
		return Content{}, errs
	}
	computeLegacy(out)
	return out, nil
}

func (k *contentCheck) normalizeStages(in []Stage) []Stage {
	if len(in) > MaxStages {
		k.add("stages", "tối đa "+strconv.Itoa(MaxStages)+" giai đoạn")
		return nil
	}
	out := make([]Stage, 0, len(in))
	seen := map[string]bool{}
	for i, s := range in {
		if s.Seq != i+1 {
			k.orderAt(i, idx("stages", i, ".seq"))
		}
		s.Seq = i + 1
		s.Name = strings.TrimSpace(s.Name)
		switch key := NameKey(s.Name); {
		case s.Name == "" || len([]rune(s.Name)) > MaxStageNameLen:
			k.add(idx("stages", i, ".name"), "bắt buộc, tối đa 200 ký tự")
		case seen[key]:
			k.add(idx("stages", i, ".name"), "tên giai đoạn trùng trong quy trình")
		default:
			seen[key] = true
		}
		switch {
		case (s.FromDay == nil) != (s.ToDay == nil):
			f := ".to_day"
			if s.FromDay == nil {
				f = ".from_day"
			}
			k.add(idx("stages", i, f), "nhập cả Từ và Đến hoặc để trống cả hai")
		case s.FromDay != nil:
			if !inRange(*s.FromDay) {
				k.add(idx("stages", i, ".from_day"), msgRange)
			}
			if !inRange(*s.ToDay) {
				k.add(idx("stages", i, ".to_day"), msgRange)
			}
			if *s.FromDay > *s.ToDay {
				k.add(idx("stages", i, ".to_day"), "Đến phải lớn hơn hoặc bằng Từ")
			}
		}
		out = append(out, s)
	}
	return out
}

func (k *contentCheck) normalizeSteps(in []Step, stages []Stage) []Step {
	if len(in) > MaxSteps {
		k.add("steps", "tối đa "+strconv.Itoa(MaxSteps)+" việc")
		return nil
	}
	out := make([]Step, 0, len(in))
	nameSeen := map[[2]string]bool{}
	taskNext := map[int]int{}
	prevStage, summary := 0, false
	for j, s := range in {
		f := idx("steps", j, ".")
		if s.Seq != j+1 {
			k.orderAt(j, f+"seq")
		}
		s.Seq = j + 1
		validStage := s.StageSeq >= 1 && s.StageSeq <= len(stages)
		switch {
		case !validStage:
			k.add(f+"stage_seq", "phải là một giai đoạn có trong quy trình")
		case s.StageSeq < prevStage:
			k.orderAt(j, f+"stage_seq")
		default:
			prevStage = s.StageSeq
			taskNext[s.StageSeq]++
			if s.TaskSeq != taskNext[s.StageSeq] {
				k.orderAt(j, f+"task_seq")
			}
		}
		s.Name = strings.TrimSpace(s.Name)
		if s.Name == "" || len([]rune(s.Name)) > MaxStepNameLen {
			k.add(f+"name", "bắt buộc, tối đa 200 ký tự")
		} else if key := [2]string{strconv.Itoa(s.StageSeq), NameKey(s.Name)}; nameSeen[key] {
			k.add(f+"name", "nội dung việc trùng trong cùng giai đoạn")
		} else {
			nameSeen[key] = true
		}
		s.ActivityCode = strings.TrimSpace(s.ActivityCode)
		if !KnownActivity(s.ActivityCode) {
			k.add(f+"activity_code", MsgActivityChoice)
		}
		s.StageCode = strings.TrimSpace(s.StageCode)
		if s.StageCode != "" && !ValidCode(s.StageCode) {
			k.add(f+"stage_code", "dạng [a-z][a-z0-9_]{1,39}")
		}
		if s.PHIDays != nil && (*s.PHIDays < 0 || *s.PHIDays > MaxPHIDays) {
			k.add(f+"phi_days", "trong khoảng 0..120 ngày")
		}
		s.EvidenceKinds = k.evidence(f, s.EvidenceKinds)
		if instr, err := CleanText(f+"instruction", s.Instruction, 2000); err != nil {
			if fe, ok := AsFieldError(err); ok {
				k.errs = append(k.errs, fe)
			}
		} else {
			s.Instruction = instr
		}
		if s.SeasonSummary {
			if summary {
				k.add(f+"season_summary", "chỉ một việc được đánh dấu tổng kết vụ")
			}
			summary = true
		}
		s.Window = k.window(f, s.Window, stages, s.StageSeq, validStage)
		s.Materials = k.materials(f, s.Materials)
		out = append(out, s)
	}
	return out
}

func (k *contentCheck) evidence(f string, in []string) []string {
	kinds := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, e := range in {
		e = strings.TrimSpace(e)
		if !KnownEvidenceKind(e) {
			k.add(f+"evidence_kinds", "loại bằng chứng không hợp lệ: "+e)
			continue
		}
		if !seen[e] {
			seen[e] = true
			kinds = append(kinds, e)
		}
	}
	return kinds
}

func (k *contentCheck) window(f string, in *Window, stages []Stage, stageSeq int, validStage bool) *Window {
	if in == nil {
		k.add(f+"window.kind", MsgWindowKindNeeded)
		return nil
	}
	w := *in
	w.Kind, w.Anchor = strings.TrimSpace(w.Kind), strings.TrimSpace(w.Anchor)
	f += "window."
	anchorOK := func() {
		if w.Anchor != AnchorNXG && w.Anchor != AnchorNTH && w.Anchor != AnchorNTT {
			k.add(f+"anchor", MsgAnchorChoice)
		}
	}
	fromOK := func() {
		if w.From == nil {
			k.add(f+"from", "bắt buộc")
		} else if !inRange(*w.From) {
			k.add(f+"from", msgRange)
		}
	}
	absent := func(fields ...string) {
		for _, name := range fields {
			switch {
			case name == "anchor" && w.Anchor != "":
				k.add(f+name, "không nhập cho loại cửa sổ này")
			case name == "from" && w.From != nil:
				k.add(f+name, "không nhập cho loại cửa sổ này")
			case name == "to" && w.To != nil:
				k.add(f+name, "không nhập cho loại cửa sổ này")
			}
		}
	}
	switch w.Kind {
	case WindowDateRange:
		anchorOK()
		fromOK()
		switch {
		case w.To == nil:
			k.add(f+"to", "bắt buộc")
		case !inRange(*w.To):
			k.add(f+"to", msgRange)
		case w.From != nil && *w.From > *w.To:
			k.add(f+"to", "Đến phải lớn hơn hoặc bằng Từ")
		}
	case WindowOpen:
		anchorOK()
		fromOK()
		absent("to")
	case WindowStage:
		absent("anchor", "from", "to")
		if validStage && stages[stageSeq-1].FromDay == nil {
			k.add(f+"kind", "giai đoạn chứa việc chưa có khoảng ngày Từ–Đến")
		}
	case WindowNone:
		absent("anchor", "from", "to")
	default:
		k.add(f+"kind", MsgWindowKindChoice)
	}
	return &w
}

func (k *contentCheck) materials(f string, in []Material) []Material {
	if len(in) > MaxMaterials {
		k.add(f+"materials", "tối đa "+strconv.Itoa(MaxMaterials)+" dòng vật tư")
		return nil
	}
	out := make([]Material, 0, len(in))
	seen := map[string]bool{}
	for i, m := range in {
		mf := idx(f+"materials", i, ".")
		m.MaterialCode, m.Name, m.Unit = strings.TrimSpace(m.MaterialCode), strings.TrimSpace(m.Name), strings.TrimSpace(m.Unit)
		key := ""
		switch {
		case m.MaterialCode != "":
			key = "c:" + m.MaterialCode
			if !ValidCatalogCode(m.MaterialCode) {
				k.add(mf+"material_code", "mã không hợp lệ")
			}
		case m.Name != "":
			key = "n:" + NameKey(m.Name)
		default:
			k.add(mf+"name", "chọn vật tư trong danh mục hoặc nhập tên")
		}
		if len([]rune(m.Name)) > MaxMaterialName {
			k.add(mf+"name", "tối đa 200 ký tự")
		}
		if key != "" {
			if seen[key] {
				k.add(mf+"material_code", "vật tư lặp lại trong cùng một việc")
			}
			seen[key] = true
		}
		switch {
		case !m.DosePerHa.IsPositive():
			k.add(mf+"dose_per_ha", "liều phải lớn hơn 0")
		case !m.DosePerHa.Round(4).Equal(m.DosePerHa):
			k.add(mf+"dose_per_ha", "tối đa 4 chữ số lẻ")
		case m.DosePerHa.GreaterThan(MaxDosePerHa):
			k.add(mf+"dose_per_ha", "tối đa 1.000.000")
		}
		if m.Unit == "" || len([]rune(m.Unit)) > MaxUnitLen {
			k.add(mf+"unit", "đơn vị bắt buộc, tối đa 20 ký tự")
		}
		out = append(out, m)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// stageDay trả khoảng Từ của giai đoạn chứa việc.
func stageFrom(stages []Stage, seq int) (int, bool) {
	if seq < 1 || seq > len(stages) || stages[seq-1].FromDay == nil {
		return 0, false
	}
	return *stages[seq-1].FromDay, true
}

// startDay trả ngày bắt đầu của việc so với mốc gốc; harvest là ngày bắt đầu của việc
// Thu hoạch cuối nếu đã biết (dùng cho mốc NTH).
func startDay(s Step, stages []Stage, harvest *int) (int, bool) {
	w := s.Window
	if w == nil {
		return 0, false
	}
	switch w.Kind {
	case WindowStage:
		return stageFrom(stages, s.StageSeq)
	case WindowDateRange, WindowOpen:
		if w.From == nil {
			return 0, false
		}
		switch w.Anchor {
		case AnchorNXG:
			return *w.From, true
		case AnchorNTH:
			if harvest != nil {
				return *harvest + *w.From, true
			}
		}
	}
	return 0, false
}

// lastHarvestIndex trả vị trí việc Thu hoạch cuối (thu_hoach có seq lớn nhất); -1 nếu không có.
func lastHarvestIndex(steps []Step) int {
	last := -1
	for i, s := range steps {
		if s.ActivityCode == ActThuHoach && (last < 0 || s.Seq > steps[last].Seq) {
			last = i
		}
	}
	return last
}

// computeLegacy ghi hai trường cũ day_offset/window_days và cờ undated theo quy ước tạm
// (tới AH-71): ngày bắt đầu so với mốc gốc, cửa sổ 0; không tính được ngày → 0/0/undated.
func computeLegacy(c Content) {
	var harvest *int
	if i := lastHarvestIndex(c.Steps); i >= 0 {
		if d, ok := startDay(c.Steps[i], c.Stages, nil); ok {
			harvest = &d
		}
	}
	for i := range c.Steps {
		d, ok := startDay(c.Steps[i], c.Stages, harvest)
		c.Steps[i].DayOffset, c.Steps[i].WindowDays, c.Steps[i].Undated = d, 0, !ok
	}
}

// Loại việc chặn phát hành (EX-05, Q-06).
const (
	BlockEmpty         = "empty"
	BlockNoHarvest     = "no_harvest"
	BlockHarvestWindow = "harvest_window"
)

// PublishBlock là lý do SOP chưa phát hành được; Field trỏ tới trường cần sửa.
type PublishBlock struct {
	Kind      string
	Field     string
	StepIndex int // vị trí việc (0-based), -1 nếu không gắn với việc
	StageSeq  int
	TaskSeq   int
	StepName  string
}

func (b PublishBlock) Error() string { return "cropcycle: chưa phát hành được (" + b.Kind + ")" }

// CheckPublishable kiểm nội dung đã chuẩn hóa có phát hành được không: có giai đoạn và việc,
// mỗi giai đoạn có việc, có việc Thu hoạch, và việc Thu hoạch cuối có cửa sổ ngày so với
// mốc gốc hoặc theo giai đoạn.
func CheckPublishable(c Content) error {
	if len(c.Stages) == 0 || len(c.Steps) == 0 {
		return PublishBlock{Kind: BlockEmpty, Field: "stages", StepIndex: -1}
	}
	has := make([]bool, len(c.Stages)+1)
	for _, s := range c.Steps {
		if s.StageSeq >= 1 && s.StageSeq < len(has) {
			has[s.StageSeq] = true
		}
	}
	for i := range c.Stages {
		if !has[i+1] {
			return PublishBlock{Kind: BlockEmpty, Field: idx("stages", i, ""), StepIndex: -1, StageSeq: i + 1, StepName: c.Stages[i].Name}
		}
	}
	j := lastHarvestIndex(c.Steps)
	if j < 0 {
		return PublishBlock{Kind: BlockNoHarvest, Field: "steps", StepIndex: -1}
	}
	s := c.Steps[j]
	w := s.Window
	ok := w != nil && (w.Kind == WindowStage || (w.Kind == WindowDateRange && w.Anchor == AnchorNXG))
	if !ok {
		return PublishBlock{Kind: BlockHarvestWindow, Field: idx("steps", j, ".window"), StepIndex: j,
			StageSeq: s.StageSeq, TaskSeq: s.TaskSeq, StepName: s.Name}
	}
	return nil
}
