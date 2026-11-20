package port

import (
	"context"
	"github.com/google/uuid"
)

// CooperativeAccountUsers gồm mọi tài khoản còn ở IAM, cả đã khóa và tài
// khoản cũ chưa có projection PF. Party union với lịch sử qua pubview owner.
type CooperativeAccountUsers interface {
	CooperativeAccountUsers(context.Context, uuid.UUID) ([]uuid.UUID, error)
}
