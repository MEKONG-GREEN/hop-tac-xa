package handler_test

import (
	"net/http"
	"testing"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/port"
)

func (f *knFix) probeCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.probe)
}

func wantLockedEX03(t *testing.T, r resp, what string) {
	t.Helper()
	wantCode(t, r, http.StatusForbidden, "accounts.account_locked", what)
	if r.body["detail"] != domain.MsgAccountLockedFN01 {
		t.Fatalf("%s: muốn câu EX-03, có %v", what, r.body["detail"])
	}
}

// AC-N2 (FN-01/EX-06, AH-301): tài khoản bị khóa trong lúc đang dùng → thao tác kế tiếp bằng CÙNG token còn hạn bị
// từ chối ở mọi route của hai bề mặt, không trả dữ liệu, không thay đổi dữ liệu.
func TestFN01_ACN2_KhoaTaiKhoanChanOThaoTacKeTiep(t *testing.T) {
	f := newKNFixture(t)
	anID, an, cr := f.createKN("Trần Văn Khóa", asg(port.KNTinh, f.p91, []string{deptTT}))
	ctx := f.ctxID(an, asgIDs(cr)[port.KNTinh], deptTT)
	wantCode(t, f.req(f.app, an, http.MethodGet, "/v1/probe", nil, nil), http.StatusOK, "", "trước khi khóa")

	if r := f.adminDo(f.root, http.MethodPost, "/v1/accounts/users/"+anID.String()+"/lock", map[string]any{"reason": goodReason}); r.status != http.StatusOK {
		t.Fatalf("khóa: %d %v", r.status, r.body)
	}
	before := f.probeCount()
	wantLockedEX03(t, f.req(f.app, an, http.MethodGet, "/v1/probe", nil, nil), "đọc sau khi khóa")
	wantLockedEX03(t, f.req(f.app, an, http.MethodPost, "/v1/probe/x/write", map[string]any{"a": 1}, nil), "ghi sau khi khóa")
	wantLockedEX03(t, f.withCtx(an, http.MethodGet, "/v1/probe", ctx, nil), "ngữ cảnh đã chọn trước khi khóa")
	wantLockedEX03(t, f.req(f.app, an, http.MethodGet, "/v1/me/context", nil, nil), "route của chính pf/accounts")
	if n := f.probeCount(); n != before {
		t.Fatalf("handler không được chạy khi tài khoản bị khóa: %d → %d", before, n)
	}

	// Admin API: Quản trị tài khoản bị khóa không dùng tiếp được token đang giữ.
	wantCode(t, f.adminDo(f.accAdmin, http.MethodGet, "/v1/accounts/users", nil), http.StatusOK, "", "Admin API trước khi khóa")
	if r := f.adminDo(f.root, http.MethodPost, "/v1/accounts/users/"+f.accAdminID.String()+"/lock", map[string]any{"reason": goodReason}); r.status != http.StatusOK {
		t.Fatalf("khóa Quản trị tài khoản: %d %v", r.status, r.body)
	}
	wantLockedEX03(t, f.adminDo(f.accAdmin, http.MethodGet, "/v1/accounts/users", nil), "Admin API sau khi khóa")

	// Tài khoản không có dòng trong pf (Admin khởi tạo, chỉ có ở Keycloak) vẫn dùng bình thường.
	wantCode(t, f.req(f.app, f.root, http.MethodGet, "/v1/probe", nil, nil), http.StatusOK, "", "tài khoản không có dòng pf")
}

// AC mở khóa (FN-01/EX-06, AH-301): mở khóa → token mới của người đó đọc và ghi lại bình thường.
func TestFN01_ACN2_MoKhoaDungLaiBinhThuong(t *testing.T) {
	f := newKNFixture(t)
	anID, an, _ := f.createKN("Lê Thị Mở", asg(port.KNTinh, f.p91, []string{deptTT}))
	path := "/v1/accounts/users/" + anID.String()
	if r := f.adminDo(f.root, http.MethodPost, path+"/lock", map[string]any{"reason": goodReason}); r.status != http.StatusOK {
		t.Fatalf("khóa: %d %v", r.status, r.body)
	}
	wantLockedEX03(t, f.req(f.app, an, http.MethodGet, "/v1/probe", nil, nil), "khi đang khóa")
	if r := f.adminDo(f.root, http.MethodPost, path+"/unlock", map[string]any{"reason": "Đã xác minh, cán bộ quay lại công tác"}); r.status != http.StatusOK {
		t.Fatalf("mở khóa: %d %v", r.status, r.body)
	}
	again := f.tok(anID, f.tenant, false, port.RoleKNUser)
	before := f.probeCount()
	wantCode(t, f.req(f.app, again, http.MethodGet, "/v1/probe", nil, nil), http.StatusOK, "", "đọc sau khi mở khóa")
	wantCode(t, f.req(f.app, again, http.MethodPost, "/v1/probe/x/write", map[string]any{"a": 1}, nil), http.StatusOK, "", "ghi sau khi mở khóa")
	if n := f.probeCount(); n != before+2 {
		t.Fatalf("sau mở khóa handler phải chạy: %d → %d", before, n)
	}
}
