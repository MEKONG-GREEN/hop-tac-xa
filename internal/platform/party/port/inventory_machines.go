package port

import (
	"context"

	"github.com/google/uuid"
)

type InventoryMachineGroup struct {
	TypeRef  uuid.UUID
	TypeName string
	Quantity int64
}

type InventoryMachineSummary struct {
	Quantity int64
	Groups   []InventoryMachineGroup
}

// InventoryMachineSource là số lượng theo danh mục loại máy động của AH-260,
// không phải số bản ghi máy KN của pf/mechanization. Nil/chưa có entry = chưa
// có nguồn; entry Quantity=0 là nguồn đã xác nhận không có máy.
type InventoryMachineSource interface {
	InventoryMachines(context.Context, []uuid.UUID) (map[uuid.UUID]InventoryMachineSummary, error)
}
