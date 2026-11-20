package port

import (
	"context"
	"github.com/google/uuid"
)

// OwnedImages verifies upload receipts without opening images or disclosing
// metadata. The domain caller must authorize its target household first.
// Every image must belong to the current actor/tenant and the exact subject.
type OwnedImages interface {
	VerifyOwnedImages(context.Context, uuid.UUID, []uuid.UUID, int64) error
}
