package domain

import (
	"testing"

	"github.com/google/uuid"
)

func TestNativeSharingDirectionAndTerminalHistory(t *testing.T) {
	a, b, outsider := uuid.New(), uuid.New(), uuid.New()
	for _, tc := range []struct {
		state, action string
		actor         uuid.UUID
		want          string
	}{
		{"requested", "approve", b, "approved"},
		{"requested", "approve", a, ""},
		{"requested", "reject", b, "rejected"},
		{"requested", "reject", a, ""},
		{"requested", "revoke", a, "revoked"},
		{"requested", "revoke", b, ""},
		{"approved", "revoke", a, "revoked"},
		{"approved", "revoke", b, "revoked"},
		{"approved", "approve", b, ""},
		{"approved", "revoke", outsider, ""},
		{"rejected", "approve", b, ""},
		{"revoked", "approve", b, ""},
		{"approved", "delete", a, ""},
	} {
		got, err := NextSharingState(NativeSharing{Requester: a, Owner: b, State: tc.state}, tc.actor, tc.action)
		if got != tc.want || (err != nil) != (tc.want == "") {
			t.Fatalf("%+v: %q %v", tc, got, err)
		}
	}
}
