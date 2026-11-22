package app

// AH-301 (FN-01/EX-06): tài khoản bị khóa trong lúc đang dùng bị chặn ở thao tác kế tiếp, không chờ token hết hạn.
// Middleware của module gọi RequireActiveAccount ở MỌI request đã đăng nhập của hai bề mặt; trạng thái đọc thẳng từ
// pf_accounts.account (một giao dịch chỉ đọc theo khóa chính) nên khóa/mở khóa có hiệu lực ngay ở mọi bản sao.
// Không gọi port module khác, không gọi Keycloak (transactions-and-io).

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/store"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
)

// RequireActiveAccount trả accounts.account_locked (câu EX-03) khi người gọi đang ở trạng thái Đã khóa (mọi
// lock_cause). Tài khoản không có dòng trong pf (Admin khởi tạo, tài khoản tạo ngoài nền tảng) coi là đang hoạt
// động. Không tra được trạng thái → ErrDependency cho MỌI request, kể cả đọc (fail-closed): phép tra dùng chính
// PostgreSQL mà handler nào cũng cần, cho qua chỉ mở khe cho tài khoản bị khóa.
func (s *Service) RequireActiveAccount(ctx context.Context) error {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return problem.Unauthenticated
	}
	if s.DB == nil {
		return ErrDependency
	}
	var acc *domain.Account
	if err := tenant.WithTenantTxOptions(ctx, s.DB, p.TenantID, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		var e error
		acc, e = store.Account(ctx, tx, p.ActorRef, false)
		return e
	}); err != nil {
		return ErrDependency.WithCause(err)
	}
	if acc != nil && !acc.Enabled {
		return accountLockedEX03()
	}
	// AH-276 (QTPQ-Q-22, AC-N2): Admin đổi HTX của tài khoản realm HTX → token còn mang HTX cũ bị từ chối ở thao tác
	// kế tiếp, người dùng đăng nhập lại và chỉ thấy HTX mới. Chỉ áp cho Đại diện/Thành viên HTX của realm HTX.
	if acc != nil && len(s.NativeHTXKey) > 0 && acc.OrgRef != uuid.Nil && p.OrgRef != acc.OrgRef && p.KN == nil &&
		p.SelectedSession == nil && !p.Has(htxroles.Admin) && (p.Has(htxroles.Representative) || p.Has(htxroles.Member)) {
		return problem.Unauthenticated.WithDetail("HTX của tài khoản đã thay đổi. Vui lòng đăng nhập lại.")
	}
	return nil
}
