package handler_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
)

// AH-2 AC4: giống đã gắn quy trình SOP hoặc vụ được tính là đang được tham chiếu (chỉ ẩn, không xóa);
// giống chưa dùng thì không.
func TestCatalogUsage_VarietyInSeasonSopHarvest(t *testing.T) {
	f := newFixture(t)
	ctx := tenant.WithID(context.Background(), f.tenant)
	inUse := func(ctx context.Context, code string) bool {
		t.Helper()
		used, err := f.mod.svc.VarietyInUse(ctx, code)
		if err != nil {
			t.Fatalf("VarietyInUse(%s): %v", code, err)
		}
		return used
	}

	if inUse(ctx, "OM5451") {
		t.Fatal("giống chưa gắn gì không được tính là đang dùng")
	}
	// Chỉ gắn SOP (chưa có vụ) → đã được tham chiếu.
	f.publishedSOP("SOP OM5451", "OM5451")
	if !inUse(ctx, "OM5451") {
		t.Fatal("giống đã gắn SOP phải được tính là đang dùng")
	}
	if inUse(ctx, "ST25") {
		t.Fatal("giống khác chưa gắn gì")
	}
	// Mở vụ với giống đó → vẫn đang dùng.
	r := f.farmer(f.h1).open(f.p1, map[string]any{"variety_code": "OM5451"})
	if r.status != http.StatusCreated {
		t.Fatalf("mở vụ: %d %s", r.status, r.raw)
	}
	if !inUse(ctx, "OM5451") {
		t.Fatal("giống đã gắn vụ phải được tính là đang dùng")
	}
	// Tenant khác không thấy dữ liệu của tenant này (RLS).
	if inUse(tenant.WithID(context.Background(), ids.New()), "OM5451") {
		t.Fatal("tenant khác không được thấy tham chiếu")
	}
}
