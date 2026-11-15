package app

import (
	"errors"
	"testing"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/masterdata/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
)

// AH-2: sửa giống từ màn mới (không gửi năng suất) không làm mất năng suất cũ.
func TestCarryLegacyAttrs(t *testing.T) {
	cur := domain.Item{Catalog: port.CatalogRiceVariety, Attrs: map[string]string{
		"crop_code": "LUA", "growth_days": "95", "expected_yield_t_per_ha": "6.75", "description": "cũ"}}
	upd := domain.Item{Catalog: port.CatalogRiceVariety, Attrs: map[string]string{"crop_code": "LUA", "growth_days": "100"}}
	got := carryLegacyAttrs(upd, cur)
	if got.Attrs["expected_yield_t_per_ha"] != "6.75" || got.Attrs["description"] != "cũ" || got.Attrs["growth_days"] != "100" {
		t.Fatalf("giữ thuộc tính cũ, lấy giá trị mới: %v", got.Attrs)
	}
	if _, leaked := upd.Attrs["expected_yield_t_per_ha"]; leaked {
		t.Fatal("không được sửa map của request")
	}
	upd.Attrs["description"] = "mới"
	if got := carryLegacyAttrs(upd, cur); got.Attrs["description"] != "mới" {
		t.Fatalf("giá trị gửi lên thắng giá trị cũ: %v", got.Attrs)
	}
	crop := carryLegacyAttrs(domain.Item{Catalog: port.CatalogCrop, Attrs: map[string]string{"is_rice": "false"}},
		domain.Item{Catalog: port.CatalogCrop, Attrs: map[string]string{"is_rice": "true", "unit": "tấn", "is_group": "true"}})
	if crop.Attrs["unit"] != "tấn" || crop.Attrs["is_group"] != "true" || crop.Attrs["is_rice"] != "false" {
		t.Fatalf("loại cây: %v", crop.Attrs)
	}
}

func TestCheckCropShape(t *testing.T) {
	for name, attrs := range map[string]map[string]string{
		"có nhóm cha":    {"is_rice": "true", port.AttrParentCode: "RAU_MAU"},
		"thiếu là lúa":   {},
		"attrs rỗng nil": nil,
	} {
		var p *problem.Problem
		if err := checkCropShape(attrs); !errors.As(err, &p) || len(p.Errors) != 1 {
			t.Errorf("%s: phải bị từ chối theo trường: %v", name, err)
		}
	}
	if err := checkCropShape(map[string]string{"is_rice": "false"}); err != nil {
		t.Fatal(err)
	}
}

func TestGuardWritable(t *testing.T) {
	if err := guardWritable(domain.Item{Catalog: port.CatalogCrop, Attrs: map[string]string{"is_rice": "true"}}); err != nil {
		t.Fatalf("loại cây ghi được: %v", err)
	}
	if err := guardWritable(domain.Item{Catalog: port.CatalogCrop, Attrs: map[string]string{port.AttrParentCode: "LUA"}}); !errors.Is(err, ErrCatalogItemReadOnly) {
		t.Fatalf("mục cũ có cha chỉ xem: %v", err)
	}
	if err := guardWritable(domain.Item{Catalog: port.CatalogRiceVariety, Deleted: true}); !errors.Is(err, ErrItemNotFound) {
		t.Fatalf("mục đã xóa coi như không có: %v", err)
	}
}

func TestPrepareCreateGeneratesCode(t *testing.T) {
	s := &Service{}
	if _, err := s.prepareCreate(domain.Item{Catalog: port.CatalogRiceVariety, Code: "OM5451"}); err == nil {
		t.Fatal("client không được gửi mã")
	}
	it, err := s.prepareCreate(domain.Item{Catalog: port.CatalogRiceVariety})
	if err != nil || len(it.Code) != 36 {
		t.Fatalf("mã sinh là UUID: %q %v", it.Code, err)
	}
}
