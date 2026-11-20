package app

import (
	"context"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/google/uuid"
)

func (s *Service) CooperativeAccountUsers(ctx context.Context, org uuid.UUID) ([]uuid.UUID, error) {
	p, err := authz.Require(ctx, authz.RolePlatformAdmin)
	if err != nil {
		return nil, err
	}
	if s.Dir == nil {
		return nil, ErrDependency
	}
	var refs []uuid.UUID
	seen := map[uuid.UUID]bool{}
	const pageSize = 100
	for page := 0; page < 1000; page++ {
		users, err := s.Dir.FindUsers(ctx, port.Query{Attributes: map[string]string{port.AttrTenantID: p.TenantID.String(), port.AttrOrgRef: org.String()}, First: page * pageSize, Max: pageSize})
		if err != nil {
			return nil, dirErr(err)
		}
		for _, user := range users {
			if user.TenantID != p.TenantID || user.OrgRef != org || user.ID == uuid.Nil {
				return nil, ErrDependency.WithDetail("IAM trả phạm vi tài khoản không nhất quán")
			}
			if !seen[user.ID] {
				refs = append(refs, user.ID)
				seen[user.ID] = true
			}
		}
		if len(users) < pageSize {
			return refs, nil
		}
	}
	return nil, ErrDependency.WithDetail("Chưa kiểm kê hết tài khoản của HTX")
}
