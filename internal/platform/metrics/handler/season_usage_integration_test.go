package handler_test

import (
	"context"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/metrics/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/clock"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
)

// AH-7 AC4: mùa vụ đã có số liệu thống kê theo chiều vụ "MÃ-NĂM" được tính là đang được tham chiếu (chỉ ẩn, không
// xóa), không phân biệt hoa thường; tiền tố của mã hoặc mùa vụ khác thì không.
func TestSeasonInUse(t *testing.T) {
	f := newFixture(t, clock.NewFixed(time.Now()))
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
		t.Fatal("mùa vụ chưa có số liệu không được tính là đang dùng")
	}
	key := port.Key("cgh.coop_sufficiency_pct")
	f.src.scopes = []port.Scope{{Dimension: port.SeasonDimension("DX", 2026)}}
	f.src.set(key, "", "DX-2026", "62.5")
	if r, err := f.mod.Service().RecordDay(ctx, f.tenant, yesterday(), []port.Key{key}); err != nil || r.Inserted != 1 {
		t.Fatalf("ghi số liệu: %+v %v", r, err)
	}
	if !inUse(ctx, "DX") || !inUse(ctx, "dx") {
		t.Fatal("mùa vụ đã có số liệu phải được tính là đang dùng, không phân biệt hoa thường")
	}
	if inUse(ctx, "D") {
		t.Fatal("tiền tố của mã không được tính là đang dùng")
	}
	if inUse(ctx, "HT") {
		t.Fatal("mùa vụ khác chưa có số liệu")
	}
	// Tenant khác không thấy dữ liệu của tenant này (RLS).
	if inUse(tenant.WithID(context.Background(), ids.New()), "DX") {
		t.Fatal("tenant khác không được thấy tham chiếu")
	}
}
