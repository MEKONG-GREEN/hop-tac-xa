package app

import (
	"context"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	port "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
)

// Dòng của ma trận Mục VI BRD Kế hoạch mùa vụ v3.9 trong bảng quyền dùng chung contracts/htxroles (AH-39).
const (
	fnSOP        = "KHMV-FN-01"     // danh mục cây trồng, giống và SOP
	fnSeason     = "KHMV-FN-03"     // mở vụ, sửa vụ, dừng/hủy vụ
	fnSeasonView = "KHMV-FN-03-XEM" // xem chi tiết vụ, tiến độ, kế hoạch mùa vụ
	fnLog        = "KHMV-FN-05"     // nhật ký canh tác
	fnHarvest    = "KHMV-FN-06"     // khai báo sản lượng (gồm nhập hàng loạt, nhập từ tệp — FN-06.2)
)

// Vai ngoài ma trận giữ quyền như trước AH-39: nông dân đọc và ghi nông hộ của mình (ngừng tài khoản nông dân thuộc
// AH-309), cán bộ KN chỉ đọc theo phạm vi xã.
var (
	legacyReadRoles  = append([]string{port.RoleFarmer}, knRoles...)
	legacyWriteRoles = []string{port.RoleFarmer}
)

// matrixAccess tính quyền của người gọi với thao tác act trên dòng fn của ma trận KHMV. Tài khoản có vai ma trận
// (Admin, Đại diện HTX, Thành viên HTX) dùng quyền của vai đang có hiệu lực (htxroles.Effective — tài khoản kiêm Đại
// diện HTX và Thành viên HTX dùng quyền Đại diện HTX cho tới AH-270), phạm vi: Admin toàn tenant, hai vai còn lại HTX
// của org_ref (KH-02; thiếu org_ref → không thấy, không ghi gì). Tài khoản không có vai ma trận theo legacy (vai cũ);
// không có legacy → 403.
func (s *Service) matrixAccess(ctx context.Context, fn string, act htxroles.Action, legacy ...string) (access, error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return access{}, problem.Unauthenticated
	}
	eff := htxroles.Effective(p.Has)
	if eff == "" {
		if len(legacy) == 0 {
			return access{}, problem.Forbidden
		}
		return s.access(ctx, legacy...)
	}
	if !htxroles.Can(eff, fn, act) {
		if htxroles.Can(eff, fn, htxroles.Read) {
			return access{}, ErrReadOnly
		}
		return access{}, problem.Forbidden
	}
	if eff == htxroles.Admin {
		return access{p: p, kind: accAll, sc: authz.Scope{All: true}}, nil
	}
	return access{p: p, kind: accCoop}, nil
}
