package handler_test

import (
	"context"
	"testing"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
)

// AH-2 AC4: giống đang là thẻ của tài liệu tin tức thì được tính là đang được tham chiếu (chỉ ẩn, không xóa).
func TestCatalogUsage_DocumentTag(t *testing.T) {
	f := newFixture(t)
	a := f.actors()
	createDoc(t, a.natEditor, procedureBody("Quy trình gắn thẻ giống OM18")) // tags: rice_variety:OM18, farm_stage:sowing

	usage := f.mod.CatalogUsage()
	ctx := tenant.WithID(context.Background(), f.tenant)
	for _, c := range []struct {
		catalog, code string
		want          bool
	}{
		{"rice_variety", "OM18", true},
		{"rice_variety", "OM5451", false},
		{"farm_stage", "sowing", true},
		{"crop", "OM18", false}, // crop không phải danh mục nhãn
	} {
		used, err := usage.CatalogItemInUse(ctx, c.catalog, c.code)
		if err != nil || used != c.want {
			t.Errorf("CatalogItemInUse(%s, %s) = %v, %v; muốn %v", c.catalog, c.code, used, err, c.want)
		}
	}
	if used, err := usage.CatalogItemInUse(tenant.WithID(context.Background(), ids.New()), "rice_variety", "OM18"); err != nil || used {
		t.Fatalf("tenant khác không được thấy thẻ: %v %v", used, err)
	}
}
