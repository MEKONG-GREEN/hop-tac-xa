package htxroles

// Mã và câu thông báo khi vai làm việc không dùng được (FN-TK-01, AH-222). Máy chủ API và Vault cùng trả các mã này;
// web và app nhận biết theo mã.
const (
	// CodeNoActiveRole: tài khoản không còn vai nào của App HTX (FN-TK-01/EX-02).
	CodeNoActiveRole = "accounts.no_active_role"
	MsgNoActiveRole  = "Tài khoản chưa được gán vai trò. Liên hệ quản trị viên."
	// CodeWorkingRoleRevoked: vai đang chọn đã bị ngừng; client đưa về màn Chọn vai trò (FN-TK-01/EX-03, AC-06).
	CodeWorkingRoleRevoked = "accounts.working_role_revoked"
	MsgWorkingRoleRevoked  = "Vai trò đang dùng đã bị ngừng. Chọn lại vai trò làm việc."
)

// Revocable cho biết vai làm việc có thể bị gỡ khỏi tài khoản trong lúc đang dùng: chỉ Đại diện HTX và Thành viên HTX.
// Admin không bao giờ gỡ hay gộp với vai khác (FN-07/BR-02, BR-03), nên một lựa chọn Admin không có trong token là giả.
func Revocable(role string) bool {
	return role == Representative || role == Member
}
