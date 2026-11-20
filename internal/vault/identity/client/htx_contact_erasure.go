package client

import (
	"context"
	"github.com/google/uuid"
)

// HTXContactEraser chỉ tiêu hủy liên hệ riêng HTX, không xóa chủ thể người/hộ
// có thể được dùng ở nơi khác. Check là đọc metadata, dùng trước khi xóa Nút.
type HTXContactEraser interface {
	ValidateHTXContactErasure(context.Context, Purpose, uuid.UUID) error
	EraseHTXContact(context.Context, Purpose, uuid.UUID, uuid.UUID) error
}

func AsHTXContactEraser(c Client) (HTXContactEraser, bool) {
	p, ok := c.(HTXContactEraser)
	return p, ok
}

func (c *httpClient) ValidateHTXContactErasure(ctx context.Context, purpose Purpose, ref uuid.UUID) error {
	if purpose == "" {
		return ProblemPurposeRequired
	}
	return c.do(ctx, call{method: "POST", path: "/v1/htx-contacts/" + ref.String() + "/erasure-check", purpose: purpose, in: PurposeRequest{Purpose: string(purpose)}})
}

func (c *httpClient) EraseHTXContact(ctx context.Context, purpose Purpose, ref, command uuid.UUID) error {
	if purpose == "" {
		return ProblemPurposeRequired
	}
	return c.do(ctx, call{method: "POST", path: "/v1/htx-contacts/" + ref.String() + "/erasures/" + command.String(), purpose: purpose, idemOp: "htx-contact-erase", in: PurposeRequest{Purpose: string(purpose)}})
}

func (l local) ValidateHTXContactErasure(ctx context.Context, purpose Purpose, ref uuid.UUID) error {
	if purpose == "" {
		return ProblemPurposeRequired
	}
	p, ok := AsHTXContactEraser(l.s)
	if !ok {
		return ProblemProfilesUnsupported
	}
	return p.ValidateHTXContactErasure(ctx, purpose, ref)
}

func (l local) EraseHTXContact(ctx context.Context, purpose Purpose, ref, command uuid.UUID) error {
	if purpose == "" {
		return ProblemPurposeRequired
	}
	p, ok := AsHTXContactEraser(l.s)
	if !ok {
		return ProblemProfilesUnsupported
	}
	return p.EraseHTXContact(ctx, purpose, ref, command)
}
