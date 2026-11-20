package port

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
)

// StrawFactors supplies FN-11 percentages for CT-5. A zero timestamp requests
// the current coefficient; an earlier timestamp never uses a later revision.
// Missing configuration, including before the first revision, means 35%.
// This port does not calculate collection or season-detail forecasts.
type StrawFactors interface {
	StrawFactorAt(context.Context, string, time.Time) (decimal.Decimal, error)
}
