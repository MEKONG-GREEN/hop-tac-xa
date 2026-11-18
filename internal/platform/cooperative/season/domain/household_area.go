package domain

import (
	"errors"
	"regexp"

	"github.com/shopspring/decimal"
)

var (
	ErrHouseholdSeasonArea     = errors.New("Diện tích phải lớn hơn 0, tối đa hai chữ số thập phân và không vượt 9999999999,99 ha.")
	householdSeasonAreaPattern = regexp.MustCompile(`^(0|[1-9][0-9]{0,9})(\.[0-9]{1,2})?$`)
	householdSeasonMaxArea     = decimal.RequireFromString("9999999999.99")
)

// ParseHouseholdSeasonArea validates the request string before PostgreSQL can
// coerce numeric(14,4), including values whose fifth decimal place would vanish.
func ParseHouseholdSeasonArea(raw string) (decimal.Decimal, error) {
	if !householdSeasonAreaPattern.MatchString(raw) {
		return decimal.Zero, ErrHouseholdSeasonArea
	}
	area, err := decimal.NewFromString(raw)
	if err != nil {
		return decimal.Zero, ErrHouseholdSeasonArea
	}
	if err := ValidateHouseholdSeasonArea(area); err != nil {
		return decimal.Zero, err
	}
	return area, nil
}

// ValidateHouseholdSeasonArea also guards typed, non-HTTP callers of the use case.
func ValidateHouseholdSeasonArea(area decimal.Decimal) error {
	if !area.IsPositive() || area.GreaterThan(householdSeasonMaxArea) || !area.Equal(area.Truncate(2)) {
		return ErrHouseholdSeasonArea
	}
	return nil
}
