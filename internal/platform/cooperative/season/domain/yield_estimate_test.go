package domain

import (
	"errors"
	"testing"

	"github.com/shopspring/decimal"
)

func dp(s string) *decimal.Decimal {
	d := decimal.RequireFromString(s)
	return &d
}

func TestAH87_YieldEstimateCompute(t *testing.T) {
	// 1,25 ha × 6 t/ha = 7,50 t; rơm 35 % = 2,625 → 2,63 (half-up).
	e := NewYieldEstimate(decimal.RequireFromString("1.25"), decimal.NewFromInt(6), dp("35"), nil, nil)
	if e.ComputedRice.String() != "7.5" || e.ComputedStraw.String() != "2.63" || !e.IsRice {
		t.Fatalf("computed %s / %v", e.ComputedRice, e.ComputedStraw)
	}
	if e.Rice().String() != "7.5" || e.Straw().String() != "2.63" {
		t.Fatalf("effective %s / %v", e.Rice(), e.Straw())
	}
	// Mốc 0,005: 0,01 ha × 0,5 t/ha = 0,005 → 0,01; 0,0049… → 0,00.
	if got := NewYieldEstimate(decimal.RequireFromString("0.01"), decimal.RequireFromString("0.5"), nil, nil, nil).ComputedRice; got.String() != "0.01" {
		t.Fatalf("half-up 0.005 → %s", got)
	}
	if got := RoundTonnes(decimal.RequireFromString("0.0049999")); !got.IsZero() {
		t.Fatalf("0.0049999 → %s", got)
	}
	if got := RoundTonnes(decimal.RequireFromString("2.345")); got.String() != "2.35" {
		t.Fatalf("2.345 → %s", got)
	}
}

func TestAH87_StrawFollowsEnteredRice(t *testing.T) {
	e := NewYieldEstimate(decimal.NewFromInt(2), decimal.NewFromInt(6), dp("40"), dp("10"), nil)
	// Lúa nhập 10 t → rơm hệ thống 4 t (không theo 12 t hệ thống tính).
	if e.ComputedRice.String() != "12" || e.ComputedStraw.String() != "4" || e.Rice().String() != "10" {
		t.Fatalf("got rice %s computed straw %v", e.Rice(), e.ComputedStraw)
	}
	// Rơm nhập giữ nguyên khi lúa đổi.
	e.EnteredStraw = dp("3.5")
	e.EnteredRice = nil
	e.Recompute()
	if e.ComputedStraw.String() != "4.8" || e.Straw().String() != "3.5" {
		t.Fatalf("straw %v / %v", e.ComputedStraw, e.Straw())
	}
}

func TestAH87_NonRiceHasNoStraw(t *testing.T) {
	e := NewYieldEstimate(decimal.NewFromInt(1), decimal.NewFromInt(6), nil, nil, dp("1"))
	if e.IsRice || e.ComputedStraw != nil || e.EnteredStraw != nil || e.Straw() != nil {
		t.Fatalf("non-rice straw: %+v", e)
	}
}

func TestAH87_ParseTonnes(t *testing.T) {
	for _, ok := range []string{"0", "0.5", "6.50", "12", "99999999.99"} {
		if _, err := ParseTonnes(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for raw, want := range map[string]error{
		"-1": ErrTonnesFormat, "abc": ErrTonnesFormat, "1.234": ErrTonnesFormat, "6.500": ErrTonnesFormat, "": ErrTonnesFormat,
		"1e3": ErrTonnesFormat, " 1": ErrTonnesFormat, "1,5": ErrTonnesFormat, ".5": ErrTonnesFormat,
		"100000000": ErrTonnesRange, "99999999.991": ErrTonnesFormat,
	} {
		if _, err := ParseTonnes(raw); !errors.Is(err, want) {
			t.Errorf("%q: got %v want %v", raw, err, want)
		}
	}
}
