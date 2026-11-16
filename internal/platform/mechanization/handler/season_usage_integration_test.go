package handler_test

import (
	"context"
	"testing"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
)

// AH-7 AC4: mùa vụ đã có diện tích vụ của cơ giới hóa được tính là đang được tham chiếu (chỉ ẩn, không xóa),
// không phân biệt hoa thường; mùa vụ chưa có dữ liệu thì không.
func TestSeasonInUse(t *testing.T) {
	f := newFixture(t, defaultNow)
	ctx := tenant.WithID(context.Background(), f.tenant)
	inUse := func(ctx context.Context, code string) bool {
		t.Helper()
		used, err := f.mod.SeasonInUse(ctx, code)
		if err != nil {
			t.Fatalf("SeasonInUse(%s): %v", code, err)
		}
		return used
	}

	if inUse(ctx, "DX") {
		t.Fatal("mùa vụ chưa có dữ liệu không được tính là đang dùng")
	}
	f.adminUser().area(f.c1, "DX", 2026, "120.5", false)
	if !inUse(ctx, "DX") || !inUse(ctx, "dx") {
		t.Fatal("mùa vụ đã có diện tích vụ phải được tính là đang dùng, không phân biệt hoa thường")
	}
	if inUse(ctx, "HT") {
		t.Fatal("mùa vụ khác chưa có dữ liệu")
	}
	// Tenant khác không thấy dữ liệu của tenant này (RLS).
	if inUse(tenant.WithID(context.Background(), ids.New()), "DX") {
		t.Fatal("tenant khác không được thấy tham chiếu")
	}
}
