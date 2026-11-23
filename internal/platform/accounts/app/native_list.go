package app

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"slices"
	"strings"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/store"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
)

// NativeUsers returns a complete bounded snapshot for FN-07's shared list UX.
// Never return a partial snapshot as if counts and filters were complete.
func (s *Service) NativeUsers(ctx context.Context) ([]UserView, error) {
	a, err := s.nativeAccountCaller(ctx)
	if err != nil {
		return nil, err
	}
	return s.nativeUsers(ctx, a)
}

// nativeUsers: Admin thấy mọi tài khoản realm HTX của tenant; Đại diện HTX (a.coop) chỉ thấy Thành viên HTX của HTX mình
// và dòng của chính mình (FN-08/AC-01, BR-01).
func (s *Service) nativeUsers(ctx context.Context, a *actor) ([]UserView, error) {
	const maximum = 2000
	items := make([]UserView, 0)
	for offset := 0; offset <= maximum; offset += 100 {
		users, err := s.Dir.FindUsers(ctx, port.Query{Attributes: map[string]string{port.AttrTenantID: a.p.TenantID.String()}, First: offset, Max: 100})
		if err != nil {
			return nil, dirErr(err)
		}
		if offset+len(users) > maximum {
			return nil, ErrDependency.WithDetail("Danh sách vượt giới hạn an toàn; cần phân trang IAM ở máy chủ")
		}
		for _, u := range users {
			if u.TenantID != a.p.TenantID {
				return nil, ErrOutOfScope
			}
			all, err := s.Dir.RealmRoles(ctx, u.ID)
			if err != nil {
				return nil, dirErr(err)
			}
			roles := make([]string, 0, 2)
			for _, r := range []string{htxroles.Admin, htxroles.Representative, htxroles.Member} {
				if slices.Contains(all, r) {
					roles = append(roles, r)
				}
			}
			if len(roles) == 0 {
				continue
			}
			if !domain.NativeAccountRoles(roles) {
				return nil, ErrState.WithDetail("Tài khoản HTX có tổ hợp vai không hợp lệ")
			}
			if roles[0] != htxroles.Admin {
				o, err := s.org(ctx, a, u.OrgRef)
				if err != nil {
					return nil, err
				}
				if o == nil || string(o.Kind) != "cooperative" {
					return nil, ErrState.WithDetail("Tài khoản HTX thiếu HTX hợp lệ")
				}
			}
			items = append(items, UserView{User: u, Roles: roles})
		}
		if len(users) < 100 {
			break
		}
	}
	refs := make([]uuid.UUID, 0, len(items))
	for _, item := range items {
		refs = append(refs, item.User.ID)
	}
	var accounts map[uuid.UUID]domain.Account
	if err := tenant.WithTenantTx(ctx, s.DB, a.p.TenantID, func(tx pgx.Tx) error { var err error; accounts, err = store.Accounts(ctx, tx, refs); return err }); err != nil {
		return nil, err
	}
	for i := range items {
		if account, ok := accounts[items[i].User.ID]; ok {
			items[i].Tracked = true
			items[i].LockCause = account.LockCause
			items[i].User.Enabled = items[i].User.Enabled && account.Enabled
		}
	}
	if a.coop != uuid.Nil {
		items = slices.DeleteFunc(items, func(u UserView) bool { return !coopVisible(a, u) })
	}
	slices.SortStableFunc(items, func(a, b UserView) int { return strings.Compare(a.User.Username, b.User.Username) })
	return items, nil
}

// NativeUser là màn chi tiết tài khoản của Admin: SĐT đầy đủ, mỗi lần xem ghi sổ truy cập (FN-TK-02/BR-04, AH-223).
func (s *Service) NativeUser(ctx context.Context, id string) (UserView, error) {
	a, err := s.nativeAccountCaller(ctx)
	if err != nil {
		return UserView{}, err
	}
	return s.nativeUserDetail(ctx, a, id)
}

// CoopNativeUser là màn chi tiết của Đại diện HTX (FN-08): chỉ tài khoản trong phạm vi coopVisible.
func (s *Service) CoopNativeUser(ctx context.Context, id string) (UserView, error) {
	a, err := s.coopAccountCaller(ctx)
	if err != nil {
		return UserView{}, err
	}
	return s.nativeUserDetail(ctx, a, id)
}

// nativeUserDetail trả tài khoản kèm SĐT đầy đủ sau khi đã ghi pf_accounts.pii_view. Ghi sổ lỗi → không trả (đóng khi
// lỗi): không có lượt xem SĐT đầy đủ nào không để lại dấu.
func (s *Service) nativeUserDetail(ctx context.Context, a *actor, id string) (UserView, error) {
	users, err := s.nativeUsers(ctx, a)
	if err != nil {
		return UserView{}, err
	}
	idx := slices.IndexFunc(users, func(u UserView) bool { return u.User.ID.String() == id })
	if idx < 0 {
		return UserView{}, problem.NotFound
	}
	u := users[idx]
	if u.User.Phone == "" {
		return u, nil
	}
	if err := s.recordPhoneView(ctx, a.p.TenantID, a.p.ActorRef, u.User.ID); err != nil {
		return UserView{}, err
	}
	return u, nil
}

// recordPhoneView ghi một dòng pf_accounts.pii_view (ai xem SĐT đầy đủ của tài khoản nào, lúc nào). Gọi trước khi trả
// SĐT; lỗi thì không trả (đóng khi lỗi — AH-223 BR-04, AH-9 AC3).
func (s *Service) recordPhoneView(ctx context.Context, tid, actorRef, user uuid.UUID) error {
	view := domain.PIIView{ID: ids.New(), TenantID: tid, UserRef: user, Field: domain.PIIFieldPhone, EventTime: s.now(), ActorRef: actorRef}
	view.IdemKey = "piiview:" + view.ID.String()
	return tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error { return store.InsertPIIView(ctx, tx, &view) })
}
