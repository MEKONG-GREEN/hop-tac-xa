// Package authz: danh tính người gọi và phân quyền nghiệp vụ ở backend (R-55).
//
// Gateway chỉ làm TLS, định tuyến, giới hạn tần suất. Mọi kiểm quyền nằm ở
// đây và ở app/ của module — không ở UI.
package authz

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
)

// Vai dùng chung. Vai nghiệp vụ của module khai báo trong module.
const (
	RoleParamSetter   = "param_setter"   // R-56
	RoleParamApprover = "param_approver" // R-56
	RolePlatformAdmin = "platform_admin"
)

// Principal là người gọi đã xác thực.
type Principal struct {
	// roleCeiling can only reduce authenticated roles for a selected working role.
	// Original Roles, actor, tenant, organization and MFA remain unchanged.
	roleCeiling []string
	ActorRef    uuid.UUID // = sub của token; mã tham chiếu, không phải định danh
	TenantID    uuid.UUID // từ claim tenant_id của token (R-16)
	// OrgRef là đơn vị công tác của người gọi (claim org_ref): TTKN TW, TTKN tỉnh,
	// tổ khuyến nông xã, HTX, Cục KTHT… Phạm vi dữ liệu theo cây phân cấp tính từ
	// đây ở tầng ứng dụng (ADR-0009, R-33). uuid.Nil nếu token không có.
	OrgRef   uuid.UUID
	Roles    []string
	MFA      bool      // token có amr chứa yếu tố thứ hai, hoặc phiên đã qua OTP email (httpx.AuthenticateAttested)
	AuthTime time.Time // thời điểm đăng nhập
	// SessionID là phiên Keycloak của token (claim sid, dự phòng session_state); rỗng nếu token không mang.
	SessionID string
	// SelectedSession is supplied only by a registered server-side owner, never client input.
	SelectedSession *Session `json:"-"`
	// KN là ngữ cảnh phiên KN v7.7 (header X-AG-Context) do pf/accounts giải ở server; nil khi
	// request không mang header (kn.go).
	KN *KNContext
}

// Has cho biết người gọi có vai r.
func (p Principal) Has(r string) bool {
	return slices.Contains(p.Roles, r) && (p.roleCeiling == nil || slices.Contains(p.roleCeiling, r))
}

// WithRoleCeiling intersects authorization with a server-validated selection.
// It cannot introduce a role absent from the signed authentication principal.
func (p Principal) WithRoleCeiling(allowed ...string) Principal {
	ceiling := make([]string, 0, len(allowed))
	for _, r := range allowed {
		if p.Has(r) && !slices.Contains(ceiling, r) {
			ceiling = append(ceiling, r)
		}
	}
	p.roleCeiling = ceiling
	return p
}

func (p Principal) EffectiveRoles() []string {
	roles := make([]string, 0, len(p.Roles))
	for _, r := range p.Roles {
		if p.Has(r) {
			roles = append(roles, r)
		}
	}
	return roles
}

type ctxKey struct{}

// WithPrincipal gắn Principal vào context — chỉ middleware xác thực gọi.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, ctxKey{}, p)
}

// FromContext lấy Principal.
func FromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(ctxKey{}).(Principal)
	return p, ok
}

// Require trả lỗi nếu người gọi không có ít nhất một trong các vai.
func Require(ctx context.Context, roles ...string) (Principal, error) {
	p, ok := FromContext(ctx)
	if !ok {
		return Principal{}, problem.Unauthenticated
	}
	for _, r := range roles {
		if p.Has(r) {
			return p, nil
		}
	}
	return p, problem.Forbidden
}

// ErrSelfApproval: người phê duyệt trùng người đặt.
var ErrSelfApproval = errors.New("authz: tự phê duyệt")

// EnsureDistinctApprover chặn một người phê duyệt thay đổi do chính mình đặt
// (R-56). Gọi ở app/, không ở UI.
func EnsureDistinctApprover(setter uuid.UUID, approver Principal) error {
	return EnsureDistinctApproverFor(setter, approver, RoleParamApprover)
}

// EnsureDistinctApproverFor là EnsureDistinctApprover với vai phê duyệt riêng của
// module (vd waterway_publisher phát hành phiên bản mạng — người phát hành không
// được là người dựng). Gọi ở app/, không ở UI.
func EnsureDistinctApproverFor(setter uuid.UUID, approver Principal, role string) error {
	if !approver.Has(role) {
		return problem.Forbidden.WithDetail("cần vai " + role)
	}
	if setter == approver.ActorRef {
		return problem.SelfApproval.WithCause(ErrSelfApproval)
	}
	return nil
}
