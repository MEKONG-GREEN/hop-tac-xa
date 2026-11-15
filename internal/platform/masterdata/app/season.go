package app

import (
	"context"
	"strings"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
)

// Luật của danh mục mùa vụ — AH-7. Dùng chung luồng với loại cây, giống (AH-2): mã do hệ thống sinh, so trùng tên
// theo NameKey, ẩn không cần lý do, "xóa" chưa dùng thì đánh dấu xóa, đang dùng thì chỉ ẩn.

// Lý do hệ thống ghi khi Admin ẩn/xóa mùa vụ không nhập lý do.
const (
	seasonHideReason   = "Ẩn tại màn Danh mục mùa vụ"
	seasonDeleteReason = "Đã xóa tại màn Danh mục mùa vụ"
)

// adminManaged: danh mục Admin quản lý ở màn riêng (loại cây, giống, mùa vụ) — mã do hệ thống sinh, có thao tác xóa.
func adminManaged(c string) bool { return cropVariety(c) || c == port.CatalogSeason }

// newSeasonCode sinh mã mùa vụ: UUIDv7 bỏ gạch nối, viết HOA (32 ký tự). Không dùng UUID 36 ký tự như loại cây,
// giống vì pf/cooperative chỉ nhận mã vụ dạng [A-Za-z0-9_]{1,40} (và CHECK của season_event cũng vậy), còn mã mùa vụ
// luôn được chuẩn hóa về chữ HOA (domain.CanonicalCode).
func newSeasonCode() string {
	return strings.ToUpper(strings.ReplaceAll(ids.New().String(), "-", ""))
}

// systemReasons là lý do hệ thống khi ẩn, khi xóa mà Admin không nhập lý do.
func systemReasons(catalog string) (hide, del string) {
	if catalog == port.CatalogSeason {
		return seasonHideReason, seasonDeleteReason
	}
	return systemHideReason, systemDeleteReason
}

// importRejectReason là lý do từ chối dòng nhập Excel của danh mục do Admin quản lý ở màn riêng.
func importRejectReason(catalog string) string {
	if catalog == port.CatalogSeason {
		return "Thêm tại màn Danh mục mùa vụ"
	}
	return "Thêm tại màn Danh mục cây trồng & giống"
}

// SeasonsInUse trả số vụ đang dùng mỗi mùa vụ (cột "Đang dùng"); nil khi không phải danh mục mùa vụ hoặc chưa nối
// nguồn. Mã không có vụ nào thì 0.
func (s *Service) SeasonsInUse(ctx context.Context, catalog string, codes []string) map[string]int {
	if catalog != port.CatalogSeason || s.Seasons == nil || len(codes) == 0 {
		return nil
	}
	m, err := s.Seasons.SeasonsUsing(ctx, codes)
	if err != nil {
		return nil // cột phụ: lỗi nguồn không chặn danh sách
	}
	out := make(map[string]int, len(codes))
	for _, c := range codes {
		out[c] = m[c]
	}
	return out
}
