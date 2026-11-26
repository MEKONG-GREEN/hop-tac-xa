package domain

// AH-87 (FN-04.2, FN-04.3, KH-05, KH-26): sản lượng lúa, rơm dự kiến của vụ theo hộ.
//
//   - Lúa dự kiến (tấn) hệ thống tính = diện tích vụ × năng suất quy ước.
//   - Rơm dự kiến (tấn) hệ thống tính = lúa dự kiến đang có hiệu lực × hệ số rơm (%) / 100; chỉ vụ lúa.
//   - Giá trị có hiệu lực = giá trị Thành viên HTX nhập, nếu không có thì giá trị hệ thống tính.
//   - Tính bằng decimal, làm tròn thông thường (half-up, từ 0,005 lên) về 2 chữ số lẻ ở bước cuối.

import (
	"errors"
	"regexp"

	"github.com/shopspring/decimal"
)

// TonneScale là số chữ số thập phân của tấn (KH-26).
const TonneScale = 2

var (
	// DefaultConventionalYield: năng suất lúa quy ước mặc định (t/ha) khi chưa có tham số đã duyệt (KH-05).
	DefaultConventionalYield = decimal.NewFromInt(6)
	// DefaultStrawPct: hệ số rơm mặc định (%) khi giống chưa có hệ số FN-11 (AH-282).
	DefaultStrawPct = decimal.NewFromInt(35)
	// MaxTonnes: giới hạn của cột lưu (×1000 vẫn vừa yield_summary.expected_kg numeric(14,3)).
	MaxTonnes = decimal.RequireFromString("99999999.99")

	tonnePattern = regexp.MustCompile(`^[0-9]{1,30}(?:\.[0-9]{1,2})?$`)
	hundred      = decimal.NewFromInt(100)
)

// YieldEstimate là trạng thái đầy đủ của sản lượng dự kiến một vụ theo hộ.
type YieldEstimate struct {
	AreaHa, BaseYield         decimal.Decimal
	IsRice                    bool
	StrawPct                  *decimal.Decimal
	ComputedRice              decimal.Decimal
	ComputedStraw             *decimal.Decimal
	EnteredRice, EnteredStraw *decimal.Decimal
}

// RoundTonnes làm tròn half-up về 2 chữ số lẻ (số không âm nên Round của decimal là half-up).
func RoundTonnes(d decimal.Decimal) decimal.Decimal { return d.Round(TonneScale) }

// NewYieldEstimate tính giá trị hệ thống từ diện tích, năng suất quy ước và hệ số rơm (nil với vụ không phải lúa),
// giữ các giá trị nhập đã có.
func NewYieldEstimate(area, base decimal.Decimal, strawPct *decimal.Decimal, enteredRice, enteredStraw *decimal.Decimal) YieldEstimate {
	e := YieldEstimate{AreaHa: area, BaseYield: base, IsRice: strawPct != nil, StrawPct: strawPct,
		EnteredRice: enteredRice, EnteredStraw: enteredStraw}
	if !e.IsRice {
		e.EnteredStraw = nil
	}
	e.ComputedRice = RoundTonnes(area.Mul(base))
	e.Recompute()
	return e
}

// Recompute tính lại rơm hệ thống theo lúa đang có hiệu lực (trả lời dev-agent 23/11/2026, câu 1).
func (e *YieldEstimate) Recompute() {
	if !e.IsRice || e.StrawPct == nil {
		e.ComputedStraw = nil
		return
	}
	straw := RoundTonnes(e.Rice().Mul(*e.StrawPct).Div(hundred))
	e.ComputedStraw = &straw
}

// Rice là lúa dự kiến có hiệu lực.
func (e YieldEstimate) Rice() decimal.Decimal {
	if e.EnteredRice != nil {
		return *e.EnteredRice
	}
	return e.ComputedRice
}

// Straw là rơm dự kiến có hiệu lực (nil với vụ không phải lúa).
func (e YieldEstimate) Straw() *decimal.Decimal {
	if !e.IsRice {
		return nil
	}
	if e.EnteredStraw != nil {
		return e.EnteredStraw
	}
	return e.ComputedStraw
}

// Errors of ParseTonnes; câu chữ hiển thị theo trường.
var (
	ErrTonnesFormat = errors.New("nhập số tấn không âm, tối đa 2 chữ số thập phân")
	ErrTonnesRange  = errors.New("số tấn vượt giới hạn lưu trữ (tối đa 99999999.99)")
)

// ParseTonnes kiểm một giá trị tấn do người dùng nhập: số thập phân không âm, tối đa 2 chữ số lẻ, trong giới hạn cột.
func ParseTonnes(raw string) (decimal.Decimal, error) {
	if !tonnePattern.MatchString(raw) {
		return decimal.Zero, ErrTonnesFormat
	}
	d, err := decimal.NewFromString(raw)
	if err != nil {
		return decimal.Zero, ErrTonnesFormat
	}
	if d.GreaterThan(MaxTonnes) {
		return decimal.Zero, ErrTonnesRange
	}
	return d, nil
}
