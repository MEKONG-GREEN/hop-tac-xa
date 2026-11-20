package port

import (
	"context"
	"github.com/google/uuid"
)

// CooperativePrivateReferences chỉ trả có dữ liệu chặn xóa, không công bố
// bản ghi song phương qua view/chỉ số. Phải gọi ngoài transaction Party.
type CooperativePrivateReferences interface {
	CooperativePrivateReferences(context.Context, uuid.UUID) (bool, error)
}
