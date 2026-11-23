package domain

// AH-113 (FN-03.3, FN-03.6, FN-03-BR-04, FN-03-BR-06, UX-07): tùy chỉnh quy trình của riêng một vụ theo hộ.
//
// Bản chụp SOP của vụ giữ nguyên các việc gốc với seq cố định — nhật ký đã ghi tham chiếu step_seq nên seq là định
// danh, không đánh số lại. Tùy chỉnh là một lớp phủ nằm trong cùng bản chụp (khóa "custom"), SOP chuẩn không đổi:
//   - added: việc tự thêm, seq cấp tiếp theo seq lớn nhất đã cấp (không dùng lại sau khi xóa);
//   - edits: cửa sổ ngày và vật tư đã sửa của việc gốc (việc tự thêm thì sửa thẳng trong added);
//   - skipped: việc gốc đã bỏ → "Bỏ qua" (việc tự thêm bị bỏ thì xóa khỏi added);
//   - order: thứ tự hiển thị trong từng giai đoạn — đổi chỗ chỉ đổi thứ tự hiển thị.
// Lịch việc, checklist và tiến độ đều tính từ quy trình hiệu lực (Effective); việc đã xong không sửa, đổi chỗ, bỏ được.

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Các loại thay đổi quy trình của vụ.
const (
	PlanAdd    = "add"
	PlanEdit   = "edit"
	PlanMove   = "move"
	PlanRemove = "remove"
)

// MaxPlanStepSeq là seq lớn nhất của một việc trong quy trình của vụ: việc gốc ≤ MaxSteps, việc tự thêm cấp tiếp theo
// và không dùng lại seq đã xóa.
const MaxPlanStepSeq = 1000

// Customization là lớp tùy chỉnh của bản chụp SOP của vụ.
type Customization struct {
	Added   []Step       `json:"added,omitempty"`
	Edits   []StepEdit   `json:"edits,omitempty"`
	Skipped []int        `json:"skipped,omitempty"`
	Order   []StageOrder `json:"order,omitempty"`
	// LastSeq là seq lớn nhất đã cấp cho việc tự thêm (kể cả việc đã xóa) để seq không bị dùng lại.
	LastSeq int `json:"last_seq,omitempty"`
}

// StepEdit là cửa sổ ngày và vật tư đã sửa của một việc gốc.
type StepEdit struct {
	Seq       int        `json:"seq"`
	Window    *Window    `json:"window"`
	Materials []Material `json:"materials"`
}

// StageOrder là thứ tự hiển thị (theo seq) của các việc trong một giai đoạn.
type StageOrder struct {
	StageSeq int   `json:"stage_seq"`
	Steps    []int `json:"steps"`
}

// SeasonSnapshot là phần quy trình của bản chụp SOP của vụ theo hộ.
type SeasonSnapshot struct {
	Stages []Stage        `json:"stages"`
	Steps  []Step         `json:"steps"`
	Custom *Customization `json:"custom,omitempty"`
}

// PlanChange là một thao tác tùy chỉnh: thêm (Step), sửa cửa sổ/vật tư (Window, Materials), đổi chỗ (Direction
// "up"/"down") hoặc bỏ việc StepSeq.
type PlanChange struct {
	Kind      string     `json:"kind"`
	StepSeq   int        `json:"step_seq,omitempty"`
	Direction string     `json:"direction,omitempty"`
	Step      *NewStep   `json:"step,omitempty"`
	Window    *Window    `json:"window,omitempty"`
	Materials []Material `json:"materials,omitempty"`
}

// NewStep là việc tự thêm: nội dung, giai đoạn, loại việc, cửa sổ bắt buộc; vật tư, hướng dẫn tùy chọn.
type NewStep struct {
	Name         string     `json:"name"`
	StageSeq     int        `json:"stage_seq"`
	ActivityCode string     `json:"activity_code"`
	Window       *Window    `json:"window"`
	Materials    []Material `json:"materials,omitempty"`
	Instruction  string     `json:"instruction,omitempty"`
}

// Lỗi nghiệp vụ của tùy chỉnh (tầng app ánh xạ sang problem).
var (
	// ErrPlanStepResolved: việc đã xong (hoặc đã có nhật ký khi xóa việc tự thêm) — không sửa, đổi chỗ, bỏ.
	ErrPlanStepResolved = errors.New("cropcycle: việc đã xong hoặc đã có nhật ký")
	// ErrPlanStepSkipped: việc gốc đã bỏ qua.
	ErrPlanStepSkipped = errors.New("cropcycle: việc đã bỏ qua")
	// ErrPlanStepUnknown: seq không thuộc quy trình của vụ.
	ErrPlanStepUnknown = errors.New("cropcycle: việc không thuộc quy trình của vụ")
)

func (s SeasonSnapshot) custom() Customization {
	if s.Custom == nil {
		return Customization{}
	}
	return *s.Custom
}

func (s SeasonSnapshot) maxSeq() int {
	n := len(s.Steps)
	for _, st := range s.Steps {
		n = max(n, st.Seq)
	}
	c := s.custom()
	n = max(n, c.LastSeq)
	for _, a := range c.Added {
		n = max(n, a.Seq)
	}
	return n
}

// IsCustom cho biết seq là việc tự thêm.
func (s SeasonSnapshot) IsCustom(seq int) bool {
	return slices.ContainsFunc(s.custom().Added, func(a Step) bool { return a.Seq == seq })
}

// SkippedSet là các việc gốc đã bỏ.
func (s SeasonSnapshot) SkippedSet() map[int]bool {
	out := map[int]bool{}
	for _, seq := range s.custom().Skipped {
		out[seq] = true
	}
	return out
}

// Step trả việc hiệu lực theo seq (đã áp sửa đổi).
func (s SeasonSnapshot) Step(seq int) (Step, bool) {
	for _, st := range s.effectiveSteps() {
		if st.Seq == seq {
			return st, true
		}
	}
	return Step{}, false
}

// effectiveSteps: việc gốc (đã áp sửa) và việc tự thêm, xếp theo giai đoạn rồi thứ tự hiển thị; giữ seq ổn định.
func (s SeasonSnapshot) effectiveSteps() []Step {
	c := s.custom()
	edits := map[int]StepEdit{}
	for _, e := range c.Edits {
		edits[e.Seq] = e
	}
	all := make([]Step, 0, len(s.Steps)+len(c.Added))
	for _, st := range s.Steps {
		if e, ok := edits[st.Seq]; ok {
			st.Window, st.Materials = e.Window, e.Materials
		}
		all = append(all, st)
	}
	all = append(all, c.Added...)
	order := map[int]map[int]int{}
	for _, o := range c.Order {
		pos := map[int]int{}
		for i, seq := range o.Steps {
			pos[seq] = i
		}
		order[o.StageSeq] = pos
	}
	defaultPos := map[int]int{}
	for i, st := range all {
		defaultPos[st.Seq] = i
	}
	rank := func(st Step) (int, int) {
		if pos, ok := order[st.StageSeq][st.Seq]; ok {
			return 0, pos
		}
		return 1, defaultPos[st.Seq] // việc chưa có trong thứ tự đã lưu: sau cùng, theo thứ tự mặc định
	}
	slices.SortStableFunc(all, func(a, b Step) int {
		if a.StageSeq != b.StageSeq {
			return a.StageSeq - b.StageSeq
		}
		ka, pa := rank(a)
		kb, pb := rank(b)
		if ka != kb {
			return ka - kb
		}
		return pa - pb
	})
	return all
}

// Effective trả nội dung hiệu lực đã chuẩn hóa (seq theo vị trí, như NormalizeContent đòi hỏi) kèm bảng vị trí → seq
// ổn định. Lỗi theo trường của việc thứ i được đổi tên thành "step.<trường>" khi i là việc focus.
func (s SeasonSnapshot) Effective() (Content, []int, error) {
	steps := s.effectiveSteps()
	ids := make([]int, len(steps))
	taskNext := map[int]int{}
	for i := range steps {
		ids[i] = steps[i].Seq
		steps[i].Seq = i + 1
		taskNext[steps[i].StageSeq]++
		steps[i].TaskSeq = taskNext[steps[i].StageSeq]
	}
	c, err := NormalizeContent(Content{Stages: s.Stages, Steps: steps})
	if err != nil {
		return Content{}, nil, err
	}
	return c, ids, nil
}

// Schedule tính lịch việc từ quy trình hiệu lực; seq của lịch là seq ổn định, TaskSeq là thứ tự hiển thị.
func (s SeasonSnapshot) Schedule(anchor, asOf time.Time, actualHarvest *time.Time) (SchedulePlan, error) {
	c, ids, err := s.Effective()
	if err != nil {
		return SchedulePlan{}, err
	}
	plan, err := CalculateSchedule(c, anchor, asOf, actualHarvest)
	if err != nil {
		return SchedulePlan{}, err
	}
	for i := range plan.Stages {
		for j := range plan.Stages[i].Steps {
			st := &plan.Stages[i].Steps[j]
			st.Seq = ids[st.Seq-1]
			st.Custom = s.IsCustom(st.Seq)
		}
	}
	return plan, nil
}

// planStepError gắn lỗi theo trường của một việc trong nội dung hiệu lực vào "step.<trường>".
func planStepError(err error, pos int) error {
	var fes FieldErrors
	var se StepOrderError
	switch {
	case errors.As(err, &fes):
	case errors.As(err, &se):
		return FieldErrors{{"step", MsgStepOrder}}
	default:
		if fe, ok := AsFieldError(err); ok {
			fes = FieldErrors{fe}
		} else {
			return err
		}
	}
	prefix := "steps[" + strconv.Itoa(pos) + "]."
	out := FieldErrors{}
	for _, fe := range fes {
		if rest, ok := strings.CutPrefix(fe.Field, prefix); ok {
			out = append(out, FieldError{"step." + rest, fe.Reason})
		} else if !strings.HasPrefix(fe.Field, "steps[") {
			out = append(out, fe)
		}
	}
	if len(out) == 0 {
		return FieldErrors{{"step", "quy trình sau khi đổi không hợp lệ"}}
	}
	return out
}

// Apply áp một thay đổi lên bản chụp. resolved là các việc đã xong; logged là các việc đã có nhật ký (kể cả "chưa xong,
// làm tiếp"). Trả bản chụp mới và câu mô tả thay đổi cho lịch sử (ai, ngày do sổ ghi).
func (s SeasonSnapshot) Apply(ch PlanChange, resolved, logged map[int]bool) (SeasonSnapshot, string, error) {
	c := s.custom()
	c.Added = slices.Clone(c.Added)
	c.Edits = slices.Clone(c.Edits)
	c.Skipped = slices.Clone(c.Skipped)
	c.Order = slices.Clone(c.Order)
	next := SeasonSnapshot{Stages: s.Stages, Steps: s.Steps, Custom: &c}
	stageName := func(seq int) string {
		if seq >= 1 && seq <= len(s.Stages) {
			return s.Stages[seq-1].Name
		}
		return strconv.Itoa(seq)
	}
	var target Step
	if ch.Kind != PlanAdd {
		var ok bool
		if target, ok = s.Step(ch.StepSeq); !ok {
			return s, "", ErrPlanStepUnknown
		}
		if resolved[ch.StepSeq] {
			return s, "", ErrPlanStepResolved
		}
		if s.SkippedSet()[ch.StepSeq] {
			return s, "", ErrPlanStepSkipped
		}
	}
	custom := s.IsCustom(ch.StepSeq)
	var summary string
	switch ch.Kind {
	case PlanAdd:
		if ch.Step == nil {
			return s, "", FieldErrors{{"step", "nhập việc cần thêm"}}
		}
		seq := s.maxSeq() + 1
		if seq > MaxPlanStepSeq {
			return s, "", FieldErrors{{"step", "đã đạt giới hạn số việc tự thêm của vụ"}}
		}
		if len(s.effectiveSteps()) >= MaxSteps {
			return s, "", FieldErrors{{"step", "tối đa " + strconv.Itoa(MaxSteps) + " việc"}}
		}
		n := Step{Seq: seq, Name: ch.Step.Name, StageSeq: ch.Step.StageSeq, ActivityCode: ch.Step.ActivityCode,
			Window: ch.Step.Window, Materials: ch.Step.Materials, Instruction: ch.Step.Instruction}
		if n.StageSeq < 1 || n.StageSeq > len(s.Stages) {
			return s, "", FieldErrors{{"step.stage_seq", "chọn giai đoạn của quy trình"}}
		}
		c.Added = append(c.Added, n)
		c.LastSeq = seq
		summary = fmt.Sprintf("Thêm việc %q vào giai đoạn %q", strings.TrimSpace(n.Name), stageName(n.StageSeq))
	case PlanEdit:
		if ch.Window == nil {
			return s, "", FieldErrors{{"window.kind", MsgWindowKindNeeded}}
		}
		if custom {
			for i := range c.Added {
				if c.Added[i].Seq == ch.StepSeq {
					c.Added[i].Window, c.Added[i].Materials = ch.Window, ch.Materials
				}
			}
		} else {
			c.Edits = slices.DeleteFunc(c.Edits, func(e StepEdit) bool { return e.Seq == ch.StepSeq })
			c.Edits = append(c.Edits, StepEdit{Seq: ch.StepSeq, Window: ch.Window, Materials: ch.Materials})
		}
		summary = fmt.Sprintf("Sửa ngày/vật tư của việc %q", target.Name)
	case PlanMove:
		if ch.Direction != "up" && ch.Direction != "down" {
			return s, "", FieldErrors{{"direction", "chọn lên hoặc xuống"}}
		}
		var stage []int
		for _, st := range s.effectiveSteps() {
			if st.StageSeq == target.StageSeq {
				stage = append(stage, st.Seq)
			}
		}
		i := slices.Index(stage, ch.StepSeq)
		j := i - 1
		if ch.Direction == "down" {
			j = i + 1
		}
		if j < 0 || j >= len(stage) {
			return s, "", FieldErrors{{"direction", "việc đã ở đầu hoặc cuối giai đoạn"}}
		}
		stage[i], stage[j] = stage[j], stage[i]
		c.Order = slices.DeleteFunc(c.Order, func(o StageOrder) bool { return o.StageSeq == target.StageSeq })
		c.Order = append(c.Order, StageOrder{StageSeq: target.StageSeq, Steps: stage})
		word := "lên"
		if ch.Direction == "down" {
			word = "xuống"
		}
		summary = fmt.Sprintf("Đổi chỗ việc %q %s trong giai đoạn %q", target.Name, word, stageName(target.StageSeq))
	case PlanRemove:
		if custom {
			if logged[ch.StepSeq] {
				return s, "", ErrPlanStepResolved
			}
			c.Added = slices.DeleteFunc(c.Added, func(a Step) bool { return a.Seq == ch.StepSeq })
			for i := range c.Order {
				c.Order[i].Steps = slices.DeleteFunc(slices.Clone(c.Order[i].Steps), func(seq int) bool { return seq == ch.StepSeq })
			}
			summary = fmt.Sprintf("Xóa việc tự thêm %q", target.Name)
		} else {
			c.Skipped = append(c.Skipped, ch.StepSeq)
			summary = fmt.Sprintf("Bỏ việc %q (Bỏ qua)", target.Name)
		}
	default:
		return s, "", FieldErrors{{"kind", "chọn thêm, sửa, đổi chỗ hoặc bỏ việc"}}
	}
	// Kiểm quy trình sau khi đổi bằng đúng bộ luật của SOP (cửa sổ Từ ≤ Đến, liều > 0, tên không trùng...).
	steps := next.effectiveSteps()
	focus := slices.IndexFunc(steps, func(st Step) bool {
		if ch.Kind == PlanAdd {
			return st.Seq == c.LastSeq
		}
		return st.Seq == ch.StepSeq
	})
	if _, _, err := next.Effective(); err != nil {
		return s, "", planStepError(err, focus)
	}
	return next, summary, nil
}
