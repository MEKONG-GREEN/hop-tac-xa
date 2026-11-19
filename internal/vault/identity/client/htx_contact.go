package client

import (
	"context"
	"github.com/google/uuid"
)

// HTXContact là liên hệ của một HTX, độc lập với định danh người/nhà nông.
// Vault tự kiểm vai/HTX và che dữ liệu cho Member, không tin cờ của caller.
type HTXContact struct {
	FullName string `json:"full_name" pii:"true"`
	Phone    string `json:"phone" pii:"true"`
	Masked   bool   `json:"masked"`
}

type HTXContacts interface {
	ReadHTXContact(context.Context, Purpose, uuid.UUID, bool) (HTXContact, error)
	WriteHTXContact(context.Context, Purpose, uuid.UUID, uuid.UUID, HTXContact) error
}

func AsHTXContacts(c Client) (HTXContacts, bool) { p, ok := c.(HTXContacts); return p, ok }

type HTXContactRequest struct {
	Purpose string     `json:"purpose"`
	Contact HTXContact `json:"contact"`
	Masked  bool       `json:"masked"`
}

func (c *httpClient) ReadHTXContact(ctx context.Context, purpose Purpose, ref uuid.UUID, masked bool) (HTXContact, error) {
	if purpose == "" {
		return HTXContact{}, ProblemPurposeRequired
	}
	var out HTXContact
	err := c.do(ctx, call{method: "POST", path: "/v1/htx-contacts/" + ref.String() + "/read", purpose: purpose, in: HTXContactRequest{Purpose: string(purpose), Masked: masked}, out: &out})
	return out, err
}

func (c *httpClient) WriteHTXContact(ctx context.Context, purpose Purpose, ref, command uuid.UUID, contact HTXContact) error {
	if purpose == "" {
		return ProblemPurposeRequired
	}
	return c.do(ctx, call{method: "POST", path: "/v1/htx-contacts/" + ref.String() + "/commands/" + command.String(), purpose: purpose, idemOp: "htx-contact", in: HTXContactRequest{Purpose: string(purpose), Contact: contact}})
}

func (l local) ReadHTXContact(ctx context.Context, purpose Purpose, ref uuid.UUID, masked bool) (HTXContact, error) {
	if purpose == "" {
		return HTXContact{}, ProblemPurposeRequired
	}
	p, ok := AsHTXContacts(l.s)
	if !ok {
		return HTXContact{}, ProblemProfilesUnsupported
	}
	return p.ReadHTXContact(ctx, purpose, ref, masked)
}

func (l local) WriteHTXContact(ctx context.Context, purpose Purpose, ref, command uuid.UUID, contact HTXContact) error {
	if purpose == "" {
		return ProblemPurposeRequired
	}
	p, ok := AsHTXContacts(l.s)
	if !ok {
		return ProblemProfilesUnsupported
	}
	return p.WriteHTXContact(ctx, purpose, ref, command, contact)
}
