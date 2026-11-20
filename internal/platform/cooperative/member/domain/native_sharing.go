package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// NativeSharing is a directional request: Requester may read Owner only after
// approval. A reverse request is independent. There is no delete transition.
type NativeSharing struct {
	RequesterName string     `json:"requester_name,omitempty"`
	OwnerName     string     `json:"owner_name,omitempty"`
	Ref           uuid.UUID  `json:"ref"`
	Requester     uuid.UUID  `json:"requester_cooperative_ref"`
	Owner         uuid.UUID  `json:"owner_cooperative_ref"`
	State         string     `json:"state"`
	Revision      int64      `json:"revision"`
	RequestedAt   time.Time  `json:"requested_at"`
	ProcessedAt   *time.Time `json:"processed_at"`
	ActorRef      uuid.UUID  `json:"actor_ref"`
}

var ErrSharingTransition = errors.New("yêu cầu không cho phép thao tác này")

// NextSharingState checks both the acting cooperative and current state.
func NextSharingState(r NativeSharing, acting uuid.UUID, action string) (string, error) {
	if acting == uuid.Nil || (acting != r.Requester && acting != r.Owner) {
		return "", ErrSharingTransition
	}
	switch {
	case r.State == "requested" && acting == r.Owner && action == "approve":
		return "approved", nil
	case r.State == "requested" && acting == r.Owner && action == "reject":
		return "rejected", nil
	case r.State == "requested" && acting == r.Requester && action == "revoke":
		return "revoked", nil
	case r.State == "approved" && action == "revoke":
		return "revoked", nil
	default:
		return "", ErrSharingTransition
	}
}
