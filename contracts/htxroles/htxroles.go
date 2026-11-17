// Package htxroles nạp ma trận quyền Mục V của BRD Quản trị phân quyền App HTX (FN-05, ADM-01) — dữ liệu cố định
// trong mã, ở MỘT chỗ (htx-roles.v1.json), để máy chủ kiểm và web dựng menu, nút từ cùng một nguồn. Bản TS của web
// sinh từ chính tệp này (scripts/gen-role-matrix.mjs). Đối chiếu vai BRD ↔ vai trong mã, phạm vi dữ liệu và quy tắc
// cho các ticket sau: docs/conventions/vai-tro-app-htx.md.
//
// Ma trận chỉ nói vai được làm GÌ với từng chức năng; phạm vi dữ liệu ("HTX của mình", "HTX được chia sẻ") do từng
// API kiểm (AH-273, FN-09). Ẩn menu, ẩn nút trên giao diện không thay cho kiểm quyền ở máy chủ (TECH-01).
package htxroles

import (
	_ "embed"
	"encoding/json"
	"slices"
	"strings"
	"sync"
)

//go:embed htx-roles.v1.json
var raw []byte

// Ba vai cố định của App HTX (FN-05/BR-01) và tên vai realm tương ứng.
const (
	Admin          = "platform_admin" // Admin
	Representative = "coop_manager"   // Đại diện HTX
	Member         = "htx_member"     // Thành viên HTX
)

// Action là một ký hiệu quyền của ma trận.
type Action byte

// Ký hiệu quyền của ma trận Mục V.
const (
	Create        Action = 'C' // tạo
	Read          Action = 'R' // xem
	Update        Action = 'U' // sửa
	Delete        Action = 'D' // xóa
	Lock          Action = 'L' // khóa hoặc mở khóa
	PasswordReset Action = 'P' // đặt lại mật khẩu cho người khác
	Export        Action = 'X' // xuất file
)

// Role là một vai của ma trận.
type Role struct {
	Key string `json:"key"` // tên vai realm
	BRD string `json:"brd"` // tên vai trong BRD
}

// Function là một dòng của ma trận: mã chức năng BRD và quyền của từng vai ("" = không có quyền).
type Function struct {
	Key    string            `json:"key"`
	Name   string            `json:"name"`
	Grants map[string]string `json:"grants"`
}

// Matrix là nội dung htx-roles.v1.json.
type Matrix struct {
	Version   int               `json:"version"`
	Source    string            `json:"source"`
	Actions   map[string]string `json:"actions"`
	Roles     []Role            `json:"roles"`
	Functions []Function        `json:"functions"`
	// MemberWriteRoutes: route ghi ("POST /v1/…", mẫu đường dẫn của gin) mà Thành viên HTX được gọi. Ngoài danh
	// sách này, máy chủ chặn mọi request ghi của tài khoản chỉ là Thành viên HTX (cmd/api-platform/assembly).
	MemberWriteRoutes []string `json:"member_write_routes"`
}

// Load trả ma trận đã phân tích (một lần). Tệp nhúng sai định dạng là lỗi lập trình — panic.
var Load = sync.OnceValue(func() Matrix {
	var m Matrix
	if err := json.Unmarshal(raw, &m); err != nil {
		panic("htxroles: htx-roles.v1.json không hợp lệ: " + err.Error())
	}
	return m
})

// Grants trả chuỗi quyền của vai với chức năng fn ("" khi không có quyền hoặc không có trong ma trận).
func Grants(role, fn string) string {
	for _, f := range Load().Functions {
		if f.Key == fn {
			return f.Grants[role]
		}
	}
	return ""
}

// Can cho biết vai được thao tác a với chức năng fn.
func Can(role, fn string, a Action) bool {
	return strings.IndexByte(Grants(role, fn), byte(a)) >= 0
}

// With trả các vai được thao tác a với chức năng fn, theo thứ tự vai của ma trận.
func With(fn string, a Action) []string {
	var out []string
	for _, r := range Load().Roles {
		if Can(r.Key, fn, a) {
			out = append(out, r.Key)
		}
	}
	return out
}

// IsMemberOnly cho biết người có các vai (has) dùng quyền của Thành viên HTX: có htx_member mà không có
// coop_manager hay platform_admin. Tài khoản kiêm Đại diện HTX và Thành viên HTX dùng quyền Đại diện HTX
// (FN-05/BR-02; chọn vai làm việc khi đăng nhập thuộc AH-270).
func IsMemberOnly(has func(role string) bool) bool {
	return has(Member) && !has(Representative) && !has(Admin)
}

// MemberWriteRoutes trả bản sao danh sách route ghi Thành viên HTX được gọi.
func MemberWriteRoutes() []string {
	return slices.Clone(Load().MemberWriteRoutes)
}
