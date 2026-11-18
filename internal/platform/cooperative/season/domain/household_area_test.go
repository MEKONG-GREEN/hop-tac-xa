package domain

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestParseHouseholdSeasonArea(t *testing.T) {
	for _, tt := range []struct{ raw, want string }{
		{"0.01", "0.01"}, {"0.10", "0.1"}, {"1", "1"}, {"1.50", "1.5"}, {"9999999999.99", "9999999999.99"},
	} {
		t.Run(tt.raw, func(t *testing.T) {
			got, err := ParseHouseholdSeasonArea(tt.raw)
			if err != nil || got.String() != tt.want {
				t.Fatalf("got %s, %v", got, err)
			}
		})
	}
	for _, raw := range []string{"", "0", "0.0", "0.00", "-1", "0.001", "1.500", "0.25001", "01.25", ".25", "1.", "1,25", " 1.25", "1e2", "NaN", "10000000000"} {
		t.Run("reject/"+raw, func(t *testing.T) {
			if _, err := ParseHouseholdSeasonArea(raw); err == nil {
				t.Fatalf("accepted %q", raw)
			}
		})
	}
}

func TestValidateHouseholdSeasonArea(t *testing.T) {
	// Reject before numeric(14,4) could silently round an input such as 0.25001.
	for _, raw := range []string{"0", "-0.01", "0.001", "0.25001", "10000000000"} {
		if err := ValidateHouseholdSeasonArea(decimal.RequireFromString(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	if err := ValidateHouseholdSeasonArea(decimal.RequireFromString("1.5000")); err != nil {
		t.Fatal("internal decimal values with insignificant trailing zeroes remain valid", err)
	}
}
