package domain

import "testing"

func TestMaskPhoneList(t *testing.T) {
	for in, want := range map[string]string{
		"0912345678": "09•• ••• 678",
		"":           "",
		"12345":      "••••",
	} {
		if got := MaskPhoneList(in); got != want {
			t.Errorf("MaskPhoneList(%q) = %q, muốn %q", in, got, want)
		}
	}
}
