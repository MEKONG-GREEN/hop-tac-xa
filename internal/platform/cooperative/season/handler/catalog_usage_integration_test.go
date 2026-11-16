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

// AH-8: giống và loại cây đã là thông tin chung của một mẫu SOP (kể cả nháp, chưa gắn giống) được tính là đang được
// tham chiếu — chỉ ẩn, không xóa.
func TestCatalogUsage_CropAndVarietyInSOPTemplate(t *testing.T) {
	f := newFixture(t)
	ctx := tenant.WithID(context.Background(), f.tenant)
	if used, err := f.mod.svc.CropInUse(ctx, "LUA"); err != nil || used {
		t.Fatalf("loại cây chưa có SOP: %v %v", used, err)
	}
	if used, err := f.mod.svc.VarietyInUse(ctx, "ST25"); err != nil || used {
		t.Fatalf("giống chưa có SOP: %v %v", used, err)
	}
	f.createDraft("SOP nháp cho ST25", map[string]any{"variety_code": "ST25"})
	if used, err := f.mod.svc.CropInUse(ctx, "LUA"); err != nil || !used {
		t.Fatalf("loại cây là loại cây của SOP phải đang dùng: %v %v", used, err)
	}
	if used, err := f.mod.svc.VarietyInUse(ctx, "ST25"); err != nil || !used {
		t.Fatalf("giống riêng của SOP phải đang dùng: %v %v", used, err)
	}
	if used, err := f.mod.svc.CropInUse(ctx, "CAF"); err != nil || used {
		t.Fatalf("loại cây khác chưa dùng: %v %v", used, err)
	}
	if used, err := f.mod.svc.CropInUse(tenant.WithID(context.Background(), ids.New()), "LUA"); err != nil || used {
		t.Fatalf("tenant khác không thấy tham chiếu: %v %v", used, err)
	}
}

// AH-21: SOP nháp đã đánh dấu xóa biến mất với người dùng nên không còn chặn việc xóa giống và loại cây.
func TestCatalogUsage_DeletedSOPDoesNotCount_AH21(t *testing.T) {
	f := newFixture(t)
	ctx := tenant.WithID(context.Background(), f.tenant)
	tmpl, _ := f.createDraft("SOP nháp rồi xóa", map[string]any{"variety_code": "ST25"})
	if used, err := f.mod.svc.VarietyInUse(ctx, "ST25"); err != nil || !used {
		t.Fatalf("trước khi xóa giống phải đang dùng: %v %v", used, err)
	}
	if r := f.sopAdmin.adminPost(sopsPath+"/"+tmpl+"/delete", nil); r.status != http.StatusOK {
		t.Fatalf("xóa SOP nháp: %d %s", r.status, r.raw)
	}
	if used, err := f.mod.svc.VarietyInUse(ctx, "ST25"); err != nil || used {
		t.Fatalf("SOP đã xóa không được chặn giống: %v %v", used, err)
	}
	if used, err := f.mod.svc.CropInUse(ctx, "LUA"); err != nil || used {
		t.Fatalf("SOP đã xóa không được chặn loại cây: %v %v", used, err)
	}
}

// AH-7 AC4: mùa vụ đã có vụ được tính là đang được tham chiếu (chỉ ẩn, không xóa), không phân biệt hoa thường;
// cột "Đang dùng" đếm số vụ.
func TestCatalogUsage_SeasonInSeasonEvent(t *testing.T) {
	f := newFixture(t)
	ctx := tenant.WithID(context.Background(), f.tenant)
	inUse := func(ctx context.Context, code string) bool {
		t.Helper()
		used, err := f.mod.svc.SeasonInUse(ctx, code)
		if err != nil {
			t.Fatalf("SeasonInUse(%s): %v", code, err)
		}
		return used
	}

	if inUse(ctx, "HT") {
		t.Fatal("mùa vụ chưa có vụ không được tính là đang dùng")
	}
	if got, err := f.mod.svc.SeasonCounts(ctx, []string{"HT", "DX"}); err != nil || len(got) != 0 {
		t.Fatalf("chưa có vụ thì không có số đếm: %v %v", got, err)
	}
	f.publishedSOP("SOP OM5451", "OM5451") // mở vụ cần SOP đã ban hành cho giống
	r := f.farmer(f.h1).open(f.p1, map[string]any{"season_code": "HT"})
	if r.status != http.StatusCreated {
		t.Fatalf("mở vụ: %d %s", r.status, r.raw)
	}
	if !inUse(ctx, "HT") || !inUse(ctx, "ht") {
		t.Fatal("mùa vụ đã gắn vụ phải được tính là đang dùng, không phân biệt hoa thường")
	}
	if inUse(ctx, "DX") {
		t.Fatal("mùa vụ khác chưa gắn gì")
	}
	if got, err := f.mod.svc.SeasonCounts(ctx, []string{"ht", "DX"}); err != nil || len(got) != 1 || got["HT"] != 1 {
		t.Fatalf("số vụ theo mùa vụ (khóa HOA): %v %v", got, err)
	}
	// Tenant khác không thấy dữ liệu của tenant này (RLS).
	other := tenant.WithID(context.Background(), ids.New())
	if inUse(other, "HT") {
		t.Fatal("tenant khác không được thấy tham chiếu")
	}
	if got, err := f.mod.svc.SeasonCounts(other, []string{"HT"}); err != nil || len(got) != 0 {
		t.Fatalf("tenant khác không có số đếm: %v %v", got, err)
	}
}
