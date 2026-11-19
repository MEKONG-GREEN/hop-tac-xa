package client

import (
	"context"

	"github.com/google/uuid"
)

// RegistryContacts là phần mở rộng của Vault cho FN-03. Caller kiểm vai/phạm vi
// HTX trước. Command là UUIDv7 của ý định đã ghi sổ; apply một lần bền vững ngay
// cả khi receipt HTTP hết hạn. Không trả định danh trong phản hồi ghi.
type RegistryContacts interface {
	ValidateRegistryContact(context.Context, Purpose, uuid.UUID) error
	AmendRegistryContact(context.Context, Purpose, uuid.UUID, uuid.UUID, Identity) (uuid.UUID, error)
}

func AsRegistryContacts(c Client) (RegistryContacts, bool) {
	p, ok := c.(RegistryContacts)
	return p, ok
}

type RegistryContactRequest struct {
	Purpose  string `json:"purpose,omitempty"`
	FullName string `json:"full_name" pii:"true"`
	Phone    string `json:"phone" pii:"true"`
}

var _ RegistryContacts = (*httpClient)(nil)
var _ RegistryContacts = local{}

func (c *httpClient) ValidateRegistryContact(ctx context.Context, purpose Purpose, ref uuid.UUID) error {
	if purpose == "" {
		return ProblemPurposeRequired
	}
	return c.do(ctx, call{method: "POST", path: subjectPath(ref, "/registry-contact/validate"), purpose: purpose,
		in: PurposeRequest{Purpose: string(purpose)}})
}

func (c *httpClient) AmendRegistryContact(ctx context.Context, purpose Purpose, ref, command uuid.UUID, id Identity) (uuid.UUID, error) {
	if purpose == "" {
		return uuid.Nil, ProblemPurposeRequired
	}
	var out WriteResponse
	err := c.do(ctx, call{method: "POST", path: subjectPath(ref, "/registry-contact/revisions/"+command.String()), purpose: purpose,
		idemOp: "registry-contact", in: RegistryContactRequest{Purpose: string(purpose), FullName: id.FullName, Phone: id.Phone}, out: &out})
	if err != nil {
		return uuid.Nil, err
	}
	return uuid.Parse(out.RevisionRef)
}

func (l local) ValidateRegistryContact(ctx context.Context, purpose Purpose, ref uuid.UUID) error {
	if purpose == "" {
		return ProblemPurposeRequired
	}
	p, ok := AsRegistryContacts(l.s)
	if !ok {
		return ProblemProfilesUnsupported
	}
	return p.ValidateRegistryContact(ctx, purpose, ref)
}

func (l local) AmendRegistryContact(ctx context.Context, purpose Purpose, ref, command uuid.UUID, id Identity) (uuid.UUID, error) {
	if purpose == "" {
		return uuid.Nil, ProblemPurposeRequired
	}
	p, ok := AsRegistryContacts(l.s)
	if !ok {
		return uuid.Nil, ProblemProfilesUnsupported
	}
	return p.AmendRegistryContact(ctx, purpose, ref, command, id)
}
