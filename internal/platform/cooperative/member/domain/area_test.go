package domain

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestHouseholdAreaAvailability(t *testing.T) {
	for _, tt := range []struct {
		name, land, used, remaining, reason string
		active, allowed                     bool
	}{
		{"unused", "1.25", "0", "1.25", "", true, true},
		{"partial", "1.25", "0.45", "0.8", "", true, true},
		{"legacy sub-cent remainder", "1.25", "1.245", "0.005", "Diện tích còn lại không đủ 0,01 ha để mở vụ.", true, false},
		{"minimum new season", "1.25", "1.24", "0.01", "", true, true},
		{"full", "1.25", "1.25", "0", "Hết diện tích để mở vụ.", true, false},
		{"legacy overbooked", "1.25", "2", "0", "Hết diện tích để mở vụ.", true, false},
		{"inactive", "1.25", "0.45", "0.8", "Nông hộ đã ngừng tham gia HTX.", false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			land := decimal.RequireFromString(tt.land)
			got := HouseholdAreaAvailability(&land, decimal.RequireFromString(tt.used), tt.active)
			if got.Remaining == nil || got.Remaining.String() != tt.remaining || got.CanOpen != tt.allowed || got.Reason != tt.reason {
				t.Fatalf("got %+v, want remaining=%s allowed=%v reason=%q", got, tt.remaining, tt.allowed, tt.reason)
			}
		})
	}
	got := HouseholdAreaAvailability(nil, decimal.Zero, true)
	if got.Remaining != nil || got.CanOpen || got.Reason != "Chưa có diện tích đất của nông hộ." {
		t.Fatalf("unknown land must not become zero or permit opening: %+v", got)
	}
}
