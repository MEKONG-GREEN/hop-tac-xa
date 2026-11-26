package domain

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestAH153_CompareYield(t *testing.T) {
	d := func(s string) *decimal.Decimal { v := decimal.RequireFromString(s); return &v }
	diff, pct := CompareYield(d("7.50"), d("8.40"))
	if diff.String() != "0.9" || pct.String() != "12" {
		t.Fatalf("%v %v", diff, pct)
	}
	diff, pct = CompareYield(d("9"), d("6"))
	if diff.String() != "-3" || pct.StringFixed(1) != "-33.3" {
		t.Fatalf("%v %v", diff, pct)
	}
	if diff, pct = CompareYield(nil, d("6")); diff != nil || pct != nil {
		t.Fatal("thiếu dự kiến")
	}
	if diff, pct = CompareYield(d("0"), d("1")); diff == nil || pct != nil {
		t.Fatal("dự kiến 0")
	}
	if got := TonnesFromKg(decimal.RequireFromString("9755")); got.StringFixed(2) != "9.76" {
		t.Fatal(got)
	}
}
