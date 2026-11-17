package handler

// AH-301 (FN-01/EX-06): Middleware() chặn tài khoản Đã khóa ở mọi route của hai bề mặt bằng accountActiveGuard.

import (
	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/httpx"
)

// accountActiveGuard trả 403 accounts.account_locked (câu EX-03) khi người gọi đang Đã khóa, 503 khi không tra được
// trạng thái. Trả false khi đã ghi lỗi vào phản hồi.
func (h Handler) accountActiveGuard(c *gin.Context) bool {
	if err := h.Svc.RequireActiveAccount(c.Request.Context()); err != nil {
		httpx.Fail(c, err)
		return false
	}
	return true
}
