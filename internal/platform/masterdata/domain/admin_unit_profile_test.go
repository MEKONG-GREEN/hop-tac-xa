package domain

import (
	"strings"
	"testing"

	partyport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/party/port"
)

func TestManagedUnitNameAndHierarchy(t *testing.T) {
	if UnitNameKey("  XÃ\tTÂN  HÒA  ") != UnitNameKey("Xã Tân Hòa") {
		t.Fatal("Vietnamese case/whitespace must compare equally on every database locale")
	}
	for _, tc := range []struct {
		name   string
		level  partyport.Level
		parent string
		valid  bool
	}{
		{"An Giang", partyport.LevelProvince, "", true},
		{"Xã Tân Hòa", partyport.LevelCommune, "91", true},
		{"Xã Tân Hòa", partyport.LevelCommune, "", false},
		{"An Giang", partyport.LevelProvince, "91", false},
		{"Huyện cũ", partyport.LevelDistrict, "91", false},
		{"", partyport.LevelProvince, "", false},
		{strings.Repeat("ấ", 201), partyport.LevelProvince, "", false},
	} {
		if err := ValidateManagedUnit(tc.name, tc.level, tc.parent); (err == nil) != tc.valid {
			t.Errorf("%+v: %v", tc, err)
		}
	}
}
