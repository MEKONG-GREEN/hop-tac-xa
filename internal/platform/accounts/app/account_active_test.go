package app

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
)

type downDB struct{}

func (downDB) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	return nil, errors.New("db down")
}

// AH-301 (FN-01/EX-06): không tra được trạng thái tài khoản → từ chối (fail-closed), kể cả request đọc.
func TestRequireActiveAccount_FailClosed(t *testing.T) {
	caller := authz.WithPrincipal(context.Background(), authz.Principal{ActorRef: uuid.New(), TenantID: uuid.New()})
	cases := []struct {
		name   string
		ctx    context.Context
		svc    *Service
		status int
		code   string
	}{
		{"CSDL lỗi", caller, &Service{DB: downDB{}}, http.StatusServiceUnavailable, "accounts.dependency_unavailable"},
		{"chưa nối CSDL", caller, &Service{}, http.StatusServiceUnavailable, "accounts.dependency_unavailable"},
		{"không có Principal", context.Background(), &Service{DB: downDB{}}, http.StatusUnauthorized, "unauthenticated"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.svc.RequireActiveAccount(tc.ctx)
			var p *problem.Problem
			if !errors.As(err, &p) || p.Status != tc.status || p.Code != tc.code {
				t.Fatalf("muốn %d %s, có %v", tc.status, tc.code, err)
			}
		})
	}
}
