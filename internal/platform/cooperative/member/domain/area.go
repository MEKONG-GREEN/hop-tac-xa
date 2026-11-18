package domain

import "github.com/shopspring/decimal"

// AreaAvailability keeps unknown land distinct from zero. Calculate using the
// stored decimals; rounding for display must never grant capacity to open a season.
type AreaAvailability struct {
	Remaining *decimal.Decimal
	CanOpen   bool
	Reason    string
}

func HouseholdAreaAvailability(land *decimal.Decimal, used decimal.Decimal, active bool) AreaAvailability {
	var result AreaAvailability
	if land != nil {
		remaining := decimal.Max(decimal.Zero, land.Sub(used))
		result.Remaining = &remaining
	}
	switch {
	case !active:
		result.Reason = "Nông hộ đã ngừng tham gia HTX."
	case land == nil:
		result.Reason = "Chưa có diện tích đất của nông hộ."
	case !result.Remaining.IsPositive():
		result.Reason = "Hết diện tích để mở vụ."
	case result.Remaining.LessThan(decimal.New(1, -2)):
		result.Reason = "Diện tích còn lại không đủ 0,01 ha để mở vụ."
	default:
		result.CanOpen = true
	}
	return result
}
