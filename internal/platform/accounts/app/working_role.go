package app

import (
	"net/http"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
)

// FN-TK-01 (AH-222) trên realm HTX: tài khoản không còn vai và vai làm việc bị ngừng trong lúc đang dùng.
var (
	// ErrNoActiveRole: FN-TK-01/EX-02 — đăng nhập đúng mật khẩu nhưng tài khoản không còn vai nào của App HTX.
	ErrNoActiveRole = problem.New(http.StatusForbidden, htxroles.CodeNoActiveRole, htxroles.MsgNoActiveRole)
	// ErrWorkingRoleRevoked: FN-TK-01/EX-03, AC-06 — vai đang chọn (X-HTX-Role) không còn trong token đã làm mới.
	ErrWorkingRoleRevoked = problem.New(http.StatusConflict, htxroles.CodeWorkingRoleRevoked, htxroles.MsgWorkingRoleRevoked)
)

// WorkingRoleRejected là lỗi khi vai làm việc gửi lên không thuộc token: vai có thể bị ngừng (Đại diện HTX, Thành viên
// HTX) → 409 để client đưa về màn Chọn vai trò; giá trị khác (kể cả Admin — không bao giờ gỡ khỏi tài khoản) → 403.
func WorkingRoleRejected(selected string) *problem.Problem {
	if htxroles.Revocable(selected) {
		return ErrWorkingRoleRevoked
	}
	return problem.Forbidden
}
