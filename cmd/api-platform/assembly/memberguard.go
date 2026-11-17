package assembly

import (
	"net/http"
	"slices"

	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/httpx"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
)

// memberWriteGuard chặn mọi request ghi (POST/PUT/PATCH/DELETE) của tài khoản chỉ là Thành viên HTX (FN-05/BR-03,
// TECH-01; htxroles.IsMemberOnly). Đây là lớp thứ hai sau kiểm vai ở dịch vụ: route ghi nào được mở cho Thành viên
// HTX phải có trong allow ("<METHOD> <mẫu đường dẫn gin>", htxroles.MemberWriteRoutes) VÀ có vai ở dịch vụ.
// Route không khớp (404) để gin trả như cũ.
func memberWriteGuard(allow []string) gin.HandlerFunc {
	return func(c *gin.Context) {
		p, ok := authz.FromContext(c.Request.Context())
		if !ok || !htxroles.IsMemberOnly(p.Has) || readOnlyMethod(c.Request.Method) || c.FullPath() == "" ||
			slices.Contains(allow, c.Request.Method+" "+c.FullPath()) {
			c.Next()
			return
		}
		httpx.Fail(c, problem.Forbidden.WithDetail("Thành viên HTX chỉ được xem (FN-05/BR-03)"))
	}
}

func readOnlyMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions
}
