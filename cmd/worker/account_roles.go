package main

import (
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts"
	accport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/clock"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
)

// Mọi consumer/job của worker dùng cùng danh mục vai gốc của nền tảng như API (cmd/api-platform/assembly).
func workerAccounts(db tenant.Beginner, clk clock.Clock, deps accounts.Deps) (*accounts.Module, error) {
	deps.Roles = accport.NativeRoleCatalog()
	return accounts.New(db, clk, deps), nil
}
