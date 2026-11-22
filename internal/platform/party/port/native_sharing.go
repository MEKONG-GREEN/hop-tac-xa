package port

import (
	"context"
	"github.com/google/uuid"
)

// NativeCooperativeSharing supplies only the live source references that the
// signed Representative may read. Implementations never expand write scope.
type NativeCooperativeSharing interface {
	NativeSharedCooperatives(context.Context) ([]uuid.UUID, error)
}
