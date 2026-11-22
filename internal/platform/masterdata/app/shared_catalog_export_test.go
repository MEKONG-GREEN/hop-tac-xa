package app

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/masterdata/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tabular"
	"github.com/google/uuid"
)

// VTKM-Q-07 (AH-274): Đại diện HTX, Thành viên HTX và Admin xuất Excel chỉ đọc; vai khác bị từ chối trước khi
// đọc dữ liệu; chỉ danh mục dùng chung Vật tư/Máy móc.
func TestExportSharedCatalogGate(t *testing.T) {
	s := &Service{}
	with := func(roles ...string) context.Context {
		return authz.WithPrincipal(context.Background(), authz.Principal{ActorRef: uuid.New(), TenantID: uuid.New(), Roles: roles})
	}
	if _, err := s.ExportSharedCatalog(context.Background(), "material"); !errors.Is(err, problem.Unauthenticated) {
		t.Fatalf("chưa xác thực: %v", err)
	}
	if _, err := s.ExportSharedCatalog(with("kn_officer"), "material"); !errors.Is(err, problem.Forbidden) {
		t.Fatalf("vai ngoài App HTX: %v", err)
	}
	for _, role := range []string{htxroles.Admin, htxroles.Representative, htxroles.Member} {
		if _, err := s.ExportSharedCatalog(with(role), "crop"); !errors.Is(err, ErrCatalogNotFound) {
			t.Fatalf("%s xuất danh mục ngoài phạm vi: %v", role, err)
		}
	}
	if !slices.Equal(SharedCatalogs, []string{"material", "machine_type"}) {
		t.Fatalf("danh mục dùng chung: %v", SharedCatalogs)
	}
}

func TestSharedCatalogXLSXContent(t *testing.T) {
	data, err := sharedCatalogXLSX("material", []domain.Item{
		{Code: "PHAN_URE", Name: "Phân Urê", Attrs: map[string]string{"unit": "kg", "group": "Phân bón"}},
		{Code: "THUOC_A", Name: "Thuốc A", Attrs: map[string]string{"group": "Thuốc BVTV"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	tables, err := tabular.ReadXLSXSheets(data, tabular.Limits{}, "Vật tư")
	if err != nil {
		t.Fatal(err)
	}
	tb := tables["Vật tư"]
	if !slices.Equal(tb.Header, []string{"Mã", "Tên", "Nhóm", "Đơn vị tính"}) || len(tb.Rows) != 2 {
		t.Fatalf("tiêu đề/số dòng: %v %d", tb.Header, len(tb.Rows))
	}
}
