package port

import (
	"context"

	"github.com/google/uuid"
)

// JobTaskReminders là job nhắc việc đến hạn và trễ hạn lúc 07:00 giờ Việt Nam (AH-191, NTF.2).
const JobTaskReminders = "cooperative_task_reminders"

// TaskDueV1 là một thông báo NTF.2 của một vụ (KH-21: mỗi vụ một thông báo cho mỗi loại nhắc trong một lượt).
// Chỉ nhãn nghiệp vụ: mã nông hộ, tên việc, ngày — không họ tên, SĐT, CCCD. SourceID giữ nguyên khi gửi lại nên
// bên thông báo không tạo bản trùng.
type TaskDueV1 struct {
	SourceID       uuid.UUID
	CooperativeRef uuid.UUID
	SeasonRef      uuid.UUID
	HouseholdCode  string
	TaskName       string
	DueDate        string
	Overdue        bool
	Recipients     []uuid.UUID
}

// TaskReminderNotifier gửi NTF.2 tới Thành viên HTX; gọi ngoài giao dịch của pf/cooperative.
type TaskReminderNotifier interface {
	TaskDue(ctx context.Context, tenant uuid.UUID, notice TaskDueV1) error
}
