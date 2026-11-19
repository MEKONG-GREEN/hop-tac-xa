package port

import "context"

// UnitUsage supplies organization counts without reading another module's
// tables or holding the masterdata transaction while calling that module.
type UnitUsage interface {
	CooperativeCountsByUnits(context.Context, []string) (map[string]int64, error)
}
