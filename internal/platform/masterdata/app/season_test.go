package app

import (
	"context"
	"errors"
	"regexp"
	"testing"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/masterdata/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
)

// AH-7: mã mùa vụ do hệ thống sinh phải qua được luật mã vụ của pf/cooperative ([A-Za-z0-9_]{1,40}) và đã ở dạng chuẩn.
func TestNewSeasonCode(t *testing.T) {
	c := newSeasonCode()
	if !regexp.MustCompile(`^[0-9A-F]{32}$`).MatchString(c) {
		t.Fatalf("mã phải là 32 ký tự HEX viết HOA: %q", c)
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_]{1,40}$`).MatchString(c) {
		t.Fatalf("mã không qua luật mã vụ của cooperative: %q", c)
	}
	if got := domain.CanonicalCode(port.CatalogSeason, c); got != c {
		t.Fatalf("mã đã phải ở dạng chuẩn: %q → %q", c, got)
	}
	if other := newSeasonCode(); other == c {
		t.Fatal("hai lần sinh phải ra hai mã khác nhau")
	}
}

// AH-7 AC1: client không được gửi mã; không gửi thì hệ thống sinh.
func TestPrepareCreateSeason(t *testing.T) {
	s := &Service{}
	_, err := s.prepareCreate(domain.Item{Catalog: port.CatalogSeason, Code: "DX", Name: "Đông Xuân"})
	var p *problem.Problem
	if !errors.As(err, &p) || len(p.Errors) != 1 || p.Errors[0].Field != "code" {
		t.Fatalf("gửi code phải bị từ chối ở trường code: %v", err)
	}
	got, err := s.prepareCreate(domain.Item{Catalog: port.CatalogSeason, Name: "Đông Xuân"})
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^[0-9A-F]{32}$`).MatchString(got.Code) {
		t.Fatalf("mã sinh ra sai dạng: %q", got.Code)
	}
	crop, err := s.prepareCreate(domain.Item{Catalog: port.CatalogRiceVariety, Name: "OM18"})
	if err != nil || len(crop.Code) != 36 {
		t.Fatalf("giống vẫn dùng UUID 36 ký tự: %q %v", crop.Code, err)
	}
}

func TestAdminManaged(t *testing.T) {
	for _, c := range []string{port.CatalogCrop, port.CatalogRiceVariety, port.CatalogSeason} {
		if !adminManaged(c) {
			t.Errorf("%s do Admin quản lý ở màn riêng", c)
		}
	}
	if adminManaged(port.CatalogMaterial) {
		t.Error("vật tư không thuộc nhóm này")
	}
}

func TestSystemReasonsSeason(t *testing.T) {
	hide, del := systemReasons(port.CatalogSeason)
	for _, r := range []string{hide, del} {
		if len([]rune(r)) < domain.MinReasonLen {
			t.Errorf("lý do hệ thống phải qua kiểm độ dài ghi sổ: %q", r)
		}
	}
	if hide == del {
		t.Error("lý do ẩn và xóa phải khác nhau")
	}
	if h, d := systemReasons(port.CatalogCrop); h != systemHideReason || d != systemDeleteReason {
		t.Errorf("loại cây giữ lý do cũ: %q %q", h, d)
	}
}

func TestImportRejectReasonSeason(t *testing.T) {
	if got := importRejectReason(port.CatalogSeason); got != "Thêm tại màn Danh mục mùa vụ" {
		t.Fatalf("lý do nhập Excel mùa vụ: %q", got)
	}
	if got := importRejectReason(port.CatalogCrop); got != "Thêm tại màn Danh mục cây trồng & giống" {
		t.Fatalf("lý do nhập Excel loại cây: %q", got)
	}
}

type fakeSeasonUsage struct {
	counts map[string]int
	err    error
}

func (f fakeSeasonUsage) SeasonsUsing(context.Context, []string) (map[string]int, error) {
	return f.counts, f.err
}

func TestSeasonsInUse(t *testing.T) {
	ctx := context.Background()
	codes := []string{"A", "B"}
	if (&Service{}).SeasonsInUse(ctx, port.CatalogSeason, codes) != nil {
		t.Error("chưa nối nguồn thì không có cột")
	}
	s := &Service{Seasons: fakeSeasonUsage{counts: map[string]int{"A": 3}}}
	if s.SeasonsInUse(ctx, port.CatalogCrop, codes) != nil {
		t.Error("danh mục khác mùa vụ không có cột")
	}
	got := s.SeasonsInUse(ctx, port.CatalogSeason, codes)
	if got["A"] != 3 || got["B"] != 0 || len(got) != 2 {
		t.Errorf("mã thiếu phải điền 0: %v", got)
	}
	bad := &Service{Seasons: fakeSeasonUsage{err: errors.New("down")}}
	if bad.SeasonsInUse(ctx, port.CatalogSeason, codes) != nil {
		t.Error("lỗi nguồn không chặn danh sách")
	}
}
