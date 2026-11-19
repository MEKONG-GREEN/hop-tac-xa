package domain

import (
	"errors"
	"strings"
	"unicode/utf8"

	partyport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/party/port"
)

var ErrManagedUnitProvince = errors.New("chọn tỉnh / thành phố")
var ErrManagedUnitLevel = errors.New("chọn cấp Tỉnh hoặc Xã")

// NormalizeUnitName is also the comparison rule for file and database names.
// Compare in Go: PostgreSQL clusters using LC_CTYPE=C do not fold Vietnamese.
func NormalizeUnitName(name string) string { return strings.Join(strings.Fields(name), " ") }

func UnitNameKey(name string) string { return strings.ToLower(NormalizeUnitName(name)) }

// ValidateManagedUnit accepts the two operational levels only. Historical
// district versions remain available through the existing gazetteer APIs.
func ValidateManagedUnit(name string, level partyport.Level, parent string) error {
	if name == "" || utf8.RuneCountInString(name) > 200 {
		return FieldError{Field: "name", Err: ErrUnitName}
	}
	if level != partyport.LevelProvince && level != partyport.LevelCommune {
		return FieldError{Field: "level", Err: ErrManagedUnitLevel}
	}
	if level == partyport.LevelCommune && !unitCodeRe.MatchString(parent) {
		return FieldError{Field: "parent_code", Err: ErrManagedUnitProvince}
	}
	if level == partyport.LevelProvince && parent != "" {
		return FieldError{Field: "parent_code", Err: ErrUnitParent}
	}
	return nil
}
