package app

import (
	"context"
	"errors"
	"testing"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/workflow/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/clock"
)

// Approvals chỉ ghi trong giao dịch của bên gọi: ctx không mang giao dịch (db.JoinTx) → ErrNoCallerTx.
func TestApprovalsRequireCallerTx_W61(t *testing.T) {
	s := &Service{Clock: clock.System{}}
	if _, err := s.Submit(context.Background(), port.SubmitInput{}); !errors.Is(err, port.ErrNoCallerTx) {
		t.Fatalf("submit: %v", err)
	}
	if _, err := s.Decide(context.Background(), port.DecideInput{}); !errors.Is(err, port.ErrNoCallerTx) {
		t.Fatalf("decide: %v", err)
	}
}
