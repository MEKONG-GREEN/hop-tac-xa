package app

import (
	"testing"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/metrics/defs"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/metrics/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/metrics/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
)

// FN-06/BR-02 (AH-306): chỉ số đã ghi sổ tính theo xã/tỉnh/tenant, gộp nhiều HTX — phiên bản mới nhất (API values xét
// quyền theo phiên bản này) không mở cho Đại diện HTX hay Thành viên HTX. Các vai khuyến nông, CGH vẫn xem cc.* theo xã.
func TestFN06_NoCoopRoleViewsAggregatedMetrics(t *testing.T) {
	c, err := domain.Load(defs.FS)
	if err != nil {
		t.Fatal(err)
	}
	as := func(role string) authz.Principal { return authz.Principal{Roles: []string{role}} }
	for _, k := range c.Keys() {
		vers := c.Versions(k)
		latest := vers[len(vers)-1]
		for _, r := range []string{htxroles.Representative, htxroles.Member} {
			if canView(as(r), latest) {
				t.Errorf("%s v%d: vai %s xem được số gộp nhiều HTX", k, latest.Version, r)
			}
		}
	}
	for _, k := range []port.Key{"cc.season_area_ha", "cc.yield_t_per_ha"} {
		vers := c.Versions(k)
		latest := vers[len(vers)-1]
		for _, r := range []string{"kn_province_admin", "kn_national_admin", "cgh_viewer"} {
			if !canView(as(r), latest) {
				t.Errorf("%s v%d: vai %s phải xem được", k, latest.Version, r)
			}
		}
	}
}
