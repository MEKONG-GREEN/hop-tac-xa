package domain

// AH-152 (FN-06.5, KH-06, KH-27): tổng kết "Ghi lại vụ" — năng suất, tổng vật tư theo liều thực tế, số việc sớm /
// đúng hạn / trễ / bỏ qua / chưa làm; điều kiện hoàn thành vụ: mọi việc của giai đoạn cuối (trừ chính việc tổng kết) đã
// Xong hoặc Bỏ qua. Hàm thuần, không đọc DB.

import (
	"sort"

	"github.com/shopspring/decimal"
)

// SummaryMaterial là tổng một vật tư (theo tên và đơn vị) trên các bản ghi việc đang hiệu lực.
type SummaryMaterial struct {
	Name string          `json:"name"`
	Unit string          `json:"unit"`
	Qty  decimal.Decimal `json:"qty"`
}

// SummaryTasks đếm việc của quy trình (không tính việc tổng kết).
type SummaryTasks struct {
	Early   int `json:"early"`
	OnTime  int `json:"on_time"`
	Late    int `json:"late"`
	Skipped int `json:"skipped"`
	NotDone int `json:"not_done"`
	Total   int `json:"total"`
}

// AggregateMaterials cộng vật tư theo (tên, đơn vị); bỏ dòng số lượng không dương; sắp theo tên rồi đơn vị.
func AggregateMaterials(inputs []Input) []SummaryMaterial {
	type key struct{ name, unit string }
	sum := map[key]decimal.Decimal{}
	for _, in := range inputs {
		if !in.Qty.IsPositive() || in.Name == "" {
			continue
		}
		k := key{in.Name, in.Unit}
		sum[k] = sum[k].Add(in.Qty)
	}
	out := make([]SummaryMaterial, 0, len(sum))
	for k, q := range sum {
		out = append(out, SummaryMaterial{Name: k.name, Unit: k.unit, Qty: q})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Unit < out[j].Unit
	})
	return out
}

// SummarizeTasks đếm việc theo kết quả và trả các việc giai đoạn cuối chưa Xong/Bỏ qua (trừ việc tổng kết
// summarySeq; 0 = không có). lastResolved cho biết giai đoạn cuối có ít nhất một việc (ngoài việc tổng kết) và mọi việc
// đó đã Xong hoặc Bỏ qua — dùng để nhắc khai báo sản lượng (AC4).
func SummarizeTasks(c Checklist, summarySeq int) (t SummaryTasks, unresolved []int, lastResolved bool) {
	last := 0
	for _, s := range c.Stages {
		if s.Seq > last {
			last = s.Seq
		}
	}
	unresolved = []int{}
	lastCount := 0
	for _, s := range c.Stages {
		for _, task := range s.Tasks {
			if task.Seq == summarySeq {
				continue
			}
			t.Total++
			resolved := true
			switch {
			case task.CompletionStatus == "done_early":
				t.Early++
			case task.CompletionStatus == "done_late":
				t.Late++
			case task.DoneOn != nil:
				t.OnTime++
			case task.CompletionStatus == "skipped":
				t.Skipped++
			default:
				t.NotDone++
				resolved = false
			}
			if s.Seq == last {
				lastCount++
				if !resolved {
					unresolved = append(unresolved, task.Seq)
				}
			}
		}
	}
	return t, unresolved, lastCount > 0 && len(unresolved) == 0
}

// SummaryStepSeq trả số việc được đánh dấu tổng kết vụ trong quy trình của vụ (0 = không có).
func SummaryStepSeq(snap SeasonSnapshot) int {
	for _, st := range snap.effectiveSteps() {
		if st.SeasonSummary {
			return st.Seq
		}
	}
	return 0
}
