package port

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// HTXProfilePatch không có PII; không cho ghi diện tích tổng hợp/diện tích KN.
// CommandRef là ID ý định bất biến đã ghi ở org_event, dùng cho retry hai bước.
type HTXProfilePatch struct {
	Ref, CommandRef uuid.UUID
	CommuneCode     string
	HeadOffice      string
	ActorRef        uuid.UUID
	EventTime       time.Time
	IdemKey         string
}

// HTXProfileEditor do pf/cooperative hiện thực; caller gọi ngoài transaction.
type HTXProfileEditor interface {
	ApplyHTXProfile(context.Context, HTXProfilePatch) error
}
