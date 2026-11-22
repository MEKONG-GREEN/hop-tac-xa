package app

// AH-277 (FN-08, ADM-03): Đại diện HTX quản lý tài khoản Thành viên HTX của HTX mình trên Application API, dùng lại
// luồng FN-07 với phạm vi HTX:
//   - chỉ thấy Thành viên HTX của HTX mình và dòng của chính mình (AC-01, BR-01);
//   - chỉ tạo Thành viên HTX với HTX của mình; gọi thẳng API với HTX hoặc vai khác → từ chối (AC-02, AC-N1);
//   - sửa, khóa, mở khóa Thành viên HTX của HTX mình; không đổi HTX của họ, không cấp vai kiêm cho tài khoản khác;
//   - với tài khoản của chính mình chỉ bật/tắt "Kiêm Thành viên HTX" (AC-06, BR-04, QTPQ-Q-28), không tự khóa.

import (
	"context"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/domain"
	partyport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/party/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
)

func (s *Service) coopAccountCaller(ctx context.Context) (*actor, error) {
	if len(s.NativeHTXKey) < 32 {
		return nil, ErrDependency.WithDetail("chưa cấu hình tài khoản FN-08 trong realm HTX")
	}
	p, err := authz.Require(ctx, htxroles.Representative)
	if err != nil {
		return nil, err
	}
	if p.Has(htxroles.Admin) || p.KN != nil || p.TenantID == uuid.Nil || p.ActorRef == uuid.Nil {
		return nil, problem.Forbidden
	}
	if p.OrgRef == uuid.Nil {
		return nil, problem.NodeScopeRequired
	}
	if tid, err := tenant.FromContext(ctx); err != nil || tid != p.TenantID {
		return nil, problem.Unauthenticated
	}
	if s.Dir == nil || s.Scope == nil || s.Orgs == nil {
		return nil, ErrDependency
	}
	sc, err := s.Scope.Scope(ctx)
	if err != nil {
		return nil, err
	}
	return &actor{p: p, sc: sc, grantable: []string{htxroles.Member}, orgs: map[uuid.UUID]*partyport.Org{}, coop: p.OrgRef}, nil
}

// coopVisible: Thành viên HTX (một vai) của HTX mình, hoặc chính người gọi.
func coopVisible(a *actor, u UserView) bool {
	if u.User.ID == a.p.ActorRef {
		return true
	}
	return u.User.OrgRef == a.coop && slices.Equal(u.Roles, []string{htxroles.Member})
}

func coopCreateAllowed(a *actor, in domain.NativeAccountInput) error {
	if a.coop == uuid.Nil {
		return nil
	}
	if !slices.Equal(in.Roles, []string{htxroles.Member}) {
		return ErrNotGrantable.WithDetail("Đại diện HTX chỉ tạo tài khoản Thành viên HTX")
	}
	if in.CooperativeRef != a.coop {
		return ErrOutOfScope.WithDetail("Chỉ tạo tài khoản cho HTX của bạn")
	}
	return nil
}

// coopUpdateInput áp phạm vi FN-08 lên yêu cầu sửa của Đại diện HTX.
func coopUpdateInput(a *actor, cur UserView, in NativeUpdateInput) (NativeUpdateInput, error) {
	if a.coop == uuid.Nil {
		return in, nil
	}
	if cur.User.ID == a.p.ActorRef {
		// Chỉ ô "Kiêm Thành viên HTX": họ tên, SĐT, email, HTX giữ nguyên (nhờ Admin sửa).
		keep := NativeUpdateInput{FullName: fullNameOf(cur), Phone: cur.User.Phone, Email: cur.User.Email, CooperativeRef: cur.User.OrgRef, Member: in.Member}
		return keep, nil
	}
	if in.CooperativeRef != uuid.Nil && in.CooperativeRef != a.coop {
		return in, ErrOutOfScope.WithDetail("Không đổi HTX của Thành viên HTX")
	}
	if in.Member != nil && *in.Member {
		return in, ErrNotGrantable.WithDetail("Không cấp vai kiêm cho tài khoản khác")
	}
	in.CooperativeRef, in.Member = a.coop, nil
	return in, nil
}

func fullNameOf(u UserView) string {
	return strings.TrimSpace(u.User.LastName + " " + u.User.FirstName)
}

// CoopNativeUsers: danh sách tài khoản của HTX mình (FN-08).
func (s *Service) CoopNativeUsers(ctx context.Context) ([]UserView, error) {
	a, err := s.coopAccountCaller(ctx)
	if err != nil {
		return nil, err
	}
	return s.nativeUsers(ctx, a)
}

// CoopNativeCreate: Đại diện HTX tạo tài khoản Thành viên HTX của HTX mình (mật khẩu tạm chỉ trả một lần).
func (s *Service) CoopNativeCreate(ctx context.Context, input domain.NativeAccountInput, key string) (NativeCreateResult, error) {
	a, err := s.coopAccountCaller(ctx)
	if err != nil {
		return NativeCreateResult{}, err
	}
	return s.nativeCreate(ctx, a, input, key)
}

// CoopNativeUpdate: sửa Thành viên HTX của HTX mình, hoặc bật/tắt kiêm Thành viên HTX cho chính mình.
func (s *Service) CoopNativeUpdate(ctx context.Context, id uuid.UUID, input NativeUpdateInput, key string) (UserView, bool, error) {
	a, err := s.coopAccountCaller(ctx)
	if err != nil {
		return UserView{}, false, err
	}
	return s.nativeUpdate(ctx, a, id, input, key)
}

// CoopNativeSetLocked: khóa / mở khóa Thành viên HTX của HTX mình (không tự khóa).
func (s *Service) CoopNativeSetLocked(ctx context.Context, id uuid.UUID, locked bool, key string) (UserView, bool, error) {
	a, err := s.coopAccountCaller(ctx)
	if err != nil {
		return UserView{}, false, err
	}
	return s.nativeSetLocked(ctx, a, id, locked, key)
}
