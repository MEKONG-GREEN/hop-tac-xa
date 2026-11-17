package app

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	port "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/store"
	partyport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/party/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
)

type farmerHouseholdsFunc func(context.Context, uuid.UUID) ([]uuid.UUID, error)

func (f farmerHouseholdsFunc) HouseholdsOf(ctx context.Context, actor uuid.UUID) ([]uuid.UUID, error) {
	return f(ctx, actor)
}

// TestKHMV_MatrixAccessMatchesSharedMatrix: quyền của lớp dịch vụ vụ mùa khớp bảng quyền dùng chung (AH-39, NFR-05):
// tài khoản có vai ma trận được thao tác đúng khi htxroles.Allows (vai có hiệu lực — kiêm hai vai dùng quyền Đại
// diện HTX), Admin toàn tenant, Đại diện HTX/Thành viên HTX theo HTX của org_ref; bị từ chối thì 403 read_only khi
// vai có quyền xem, 403 forbidden khi không. Vai ngoài ma trận (nông dân, cán bộ KN) giữ quyền cũ.
func TestKHMV_MatrixAccessMatchesSharedMatrix(t *testing.T) {
	hh := ids.New()
	s := &Service{
		Farmers: farmerHouseholdsFunc(func(context.Context, uuid.UUID) ([]uuid.UUID, error) { return []uuid.UUID{hh}, nil }),
		Scope:   nativeScopeFunc(func(context.Context) (authz.Scope, error) { return authz.Scope{Units: []string{"31117"}}, nil }),
	}
	fns := []string{fnSOP, fnSeason, fnSeasonView, fnLog, fnHarvest}
	acts := []htxroles.Action{htxroles.Create, htxroles.Read, htxroles.Update, htxroles.Delete}
	matrixRoles := [][]string{
		{htxroles.Admin}, {htxroles.Representative}, {htxroles.Member},
		{htxroles.Representative, htxroles.Member}, {htxroles.Admin, htxroles.Member},
		{htxroles.Member, partyport.RoleKNOfficer}, {htxroles.Representative, partyport.RoleKNOfficer},
	}
	for _, roles := range matrixRoles {
		p := authz.Principal{ActorRef: ids.New(), TenantID: ids.New(), OrgRef: ids.New(), Roles: roles}
		ctx := nativeScopeContext(p)
		eff := htxroles.Effective(p.Has)
		for _, fn := range fns {
			for _, act := range acts {
				a, err := s.matrixAccess(ctx, fn, act, legacyWriteRoles...)
				switch {
				case htxroles.Allows(p.Has, fn, act):
					if err != nil {
						t.Errorf("%v %s/%c: cần được phép, có %v", roles, fn, act, err)
						continue
					}
					want := accCoop
					if eff == htxroles.Admin {
						want = accAll
					}
					if a.kind != want {
						t.Errorf("%v %s/%c: kind = %v, cần %v", roles, fn, act, a.kind, want)
					}
				case htxroles.Can(eff, fn, htxroles.Read):
					if !errors.Is(err, ErrReadOnly) {
						t.Errorf("%v %s/%c: cần cropcycle.read_only, có %v", roles, fn, act, err)
					}
				default:
					if !errors.Is(err, problem.Forbidden) {
						t.Errorf("%v %s/%c: cần forbidden, có %v", roles, fn, act, err)
					}
				}
			}
		}
	}

	t.Run("legacy roles", func(t *testing.T) {
		farmer := nativeScopeContext(authz.Principal{ActorRef: ids.New(), TenantID: ids.New(), Roles: []string{port.RoleFarmer}})
		if a, err := s.matrixAccess(farmer, fnLog, htxroles.Create, legacyWriteRoles...); err != nil || a.kind != accFarmer || !slices.Equal(a.households, []uuid.UUID{hh}) {
			t.Fatalf("nông dân giữ quyền ghi nông hộ của mình (AH-309): %+v %v", a, err)
		}
		if _, err := s.matrixAccess(farmer, fnHarvest, htxroles.Create); !errors.Is(err, problem.Forbidden) {
			t.Fatalf("nhập sản lượng hàng loạt (không có vai cũ): nông dân bị từ chối, có %v", err)
		}
		kn := nativeScopeContext(authz.Principal{ActorRef: ids.New(), TenantID: ids.New(), OrgRef: ids.New(), Roles: []string{partyport.RoleKNOfficer}})
		if a, err := s.matrixAccess(kn, fnSeasonView, htxroles.Read, legacyReadRoles...); err != nil || a.kind != accKN {
			t.Fatalf("cán bộ KN đọc theo xã: %+v %v", a, err)
		}
		if _, err := s.matrixAccess(kn, fnSeason, htxroles.Create, legacyWriteRoles...); !errors.Is(err, problem.Forbidden) {
			t.Fatalf("cán bộ KN không ghi: %v", err)
		}
		other := nativeScopeContext(authz.Principal{ActorRef: ids.New(), TenantID: ids.New(), Roles: []string{port.RolePlotViewer}})
		if _, err := s.matrixAccess(other, fnSeasonView, htxroles.Read, legacyReadRoles...); !errors.Is(err, problem.Forbidden) {
			t.Fatalf("vai khác: cần forbidden, có %v", err)
		}
		if _, err := s.matrixAccess(context.Background(), fnSeasonView, htxroles.Read, legacyReadRoles...); !errors.Is(err, problem.Unauthenticated) {
			t.Fatalf("chưa đăng nhập: cần unauthenticated, có %v", err)
		}
	})
}

// TestKHMV_NoCoopAccessIsEmpty: Đại diện HTX, Thành viên HTX chưa gắn HTX (org_ref rỗng) không thấy, không ghi gì
// (fail-closed).
func TestKHMV_NoCoopAccessIsEmpty(t *testing.T) {
	s := &Service{}
	for _, role := range []string{htxroles.Representative, htxroles.Member} {
		ctx := nativeScopeContext(authz.Principal{ActorRef: ids.New(), TenantID: ids.New(), Roles: []string{role}})
		a, err := s.matrixAccess(ctx, fnSeasonView, htxroles.Read)
		if err != nil {
			t.Fatalf("%s: %v", role, err)
		}
		if f := a.filter(); f.Mode != store.FilterNone {
			t.Errorf("%s không gắn HTX: bộ lọc cần FilterNone, có %+v", role, f)
		}
		if a.sees("31117", ids.New(), ids.New()) || a.sees("31117", uuid.Nil, uuid.Nil) {
			t.Errorf("%s không gắn HTX vẫn thấy bản ghi", role)
		}
		if err := a.canWrite(ids.New(), ids.New()); !errors.Is(err, ErrOutOfScope) {
			t.Errorf("%s không gắn HTX: ghi cần out_of_scope, có %v", role, err)
		}
	}
}
