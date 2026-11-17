package port

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
)

// Thông báo cho Thành viên HTX (AH-22, BRD §V NTF). Thành viên HTX là vai htx_member (AH-39, dòng KHMV-NTF của ma
// trận contracts/htxroles). Bốn loại dưới đây do cổng MemberNotifier tạo; việc phát sinh từ sự kiện nghiệp vụ
// thuộc Story khác (phát lịch AH-51, nhắc việc AH-191, nhắc nhật ký AH-192, đổi giống AH-175).
const (
	KindSeasonCalendarPublished = "season_calendar_published" // NTF.1: lịch thời vụ được phát
	KindSeasonTaskDue           = "season_task_due"           // NTF.2: việc đến hạn hoặc trễ hạn
	KindSeasonLogMissing        = "season_log_missing"        // NTF.3: vụ quá 3 ngày chưa có nhật ký
	KindSeasonProcessChanged    = "season_process_changed"    // NTF.4: quy trình của vụ đổi do đổi giống
)

// SourceMemberNotice là tiền tố source_event_type của thông báo tạo qua cổng; giá trị thật là
// SourceMemberNotice + "." + kind.
const SourceMemberNotice = "pf.notify.member_notice"

// MaxMemberRecipients là số người nhận tối đa của một lần gọi cổng.
const MaxMemberRecipients = 500

// Lỗi của cổng MemberNotifier (docs/errors.md).
var (
	ErrMemberNoticeInvalid = problem.New(http.StatusUnprocessableEntity, "notify.member_notice_invalid", "Nội dung thông báo cho Thành viên HTX không hợp lệ")
	ErrRecipientNotMember  = problem.New(http.StatusUnprocessableEntity, "notify.recipient_not_member", "Người nhận không phải Thành viên HTX đang hoạt động của HTX này")
)

// MemberNoticeBase là phần chung của mọi thông báo cho Thành viên HTX.
//
// SourceID là UUIDv7 do bên phát sinh cấp và giữ nguyên khi gọi lại: cùng SourceID và người nhận thì không tạo
// bản thứ hai. CooperativeRef là HTX có nông hộ của vụ (NTF.2–4) hoặc HTX của lịch (NTF.1). Người nhận: NTF.2–4 gửi
// mọi Thành viên đang hoạt động của HTX (KH-02); NTF.1 gửi các Thành viên do người phát lịch chọn.
type MemberNoticeBase struct {
	SourceID       uuid.UUID
	CooperativeRef uuid.UUID
	Recipients     []uuid.UUID
}

// Nội dung thông báo không được chứa họ tên, số điện thoại, CCCD (AGENTS quy tắc 7): chỉ dùng tên loại cây,
// giống, tên việc, số ngày và mã nông hộ.

// CalendarPublishedNotice là NTF.1. SowFrom, SowTo dạng YYYY-MM-DD.
type CalendarPublishedNotice struct {
	MemberNoticeBase
	CalendarRef    uuid.UUID
	CropName       string
	SeasonName     string
	Year           int
	SowFrom, SowTo string
}

// TaskDueNotice là NTF.2. DueDate dạng YYYY-MM-DD; Overdue = việc trễ hạn.
type TaskDueNotice struct {
	MemberNoticeBase
	SeasonRef, TaskRef uuid.UUID
	HouseholdCode      string
	TaskName           string
	DueDate            string
	Overdue            bool
}

// LogMissingNotice là NTF.3: vụ đã Days ngày chưa có nhật ký.
type LogMissingNotice struct {
	MemberNoticeBase
	SeasonRef     uuid.UUID
	HouseholdCode string
	Days          int
}

// ProcessChangedNotice là NTF.4: quy trình của vụ đổi do đổi giống, ChangedTasks việc thay đổi.
type ProcessChangedNotice struct {
	MemberNoticeBase
	SeasonRef     uuid.UUID
	HouseholdCode string
	VarietyName   string
	ChangedTasks  int
}

// MemberNotifier là cổng tạo thông báo cho Thành viên HTX. Bên gọi phải gọi NGOÀI DB transaction của mình (cổng
// gọi Keycloak để kiểm người nhận); tenantID lấy từ Principal hoặc phong bì đã xác thực, cùng mẫu StaffDirectory.
// Có một người nhận không hợp lệ thì cả lần gọi bị từ chối và không tạo gì. Gọi lại với cùng SourceID là an toàn.
type MemberNotifier interface {
	CalendarPublished(ctx context.Context, tenantID uuid.UUID, n CalendarPublishedNotice) error
	TaskDue(ctx context.Context, tenantID uuid.UUID, n TaskDueNotice) error
	LogMissing(ctx context.Context, tenantID uuid.UUID, n LogMissingNotice) error
	ProcessChanged(ctx context.Context, tenantID uuid.UUID, n ProcessChangedNotice) error
}

// MemberDirectory kiểm một tài khoản có phải Thành viên HTX đang hoạt động của HTX: tài khoản bật, cùng tenant,
// org_ref bằng HTX và có vai htx_member. pf/notify khai cổng, gốc lắp ráp hiện thực qua pf/accounts.
type MemberDirectory interface {
	IsActiveMember(ctx context.Context, tenantID, cooperativeRef, actorRef uuid.UUID) (bool, error)
}
