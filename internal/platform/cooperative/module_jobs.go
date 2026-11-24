package cooperative

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/port"
	seasonapp "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/app"
	seasonstore "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/store"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/clock"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/schedule"
)

// ReminderHour, ReminderLateHour: job nhắc việc chạy 07:00 giờ Việt Nam (NTF-05); khởi động lại worker trước
// ReminderLateHour vẫn chạy bù lượt của ngày, sau đó thì chờ 07:00 hôm sau (không gửi nhắc vào giữa ngày).
const (
	ReminderHour     = 7
	ReminderLateHour = 10
)

// UseTaskReminders nối cổng gửi NTF.2 (AH-191); danh bạ thành viên dùng chung với lịch thời vụ (UseCalendars).
func (m *Module) UseTaskReminders(n port.TaskReminderNotifier) { m.season.TaskNotifier = n }

// TaskRemindersReady kiểm phụ thuộc của job nhắc việc khi khởi động worker (fail-closed).
func (m *Module) TaskRemindersReady() error { return m.season.TaskRemindersReady() }

// ReminderTenants tìm tenant có vụ nông hộ đang canh tác hoặc còn lượt nhắc chờ, bằng pool ag_ops_jobs (chỉ cột
// tenant_id, entry_kind, status); việc thật chạy bằng r_cooperative trong ngữ cảnh từng tenant (R-05).
func ReminderTenants(pool *pgxpool.Pool) seasonapp.TenantLister {
	return func(ctx context.Context) ([]uuid.UUID, error) { return seasonstore.ReminderTenants(ctx, pool) }
}

// RunTaskReminders chạy một lượt nhắc (kiểm thử, công cụ vận hành).
func (m *Module) RunTaskReminders(ctx context.Context, tenants seasonapp.TenantLister) (seasonapp.ReminderResult, error) {
	return m.season.RunTaskReminders(ctx, tenants)
}

// Jobs trả job định kỳ của pf/cooperative (chỉ chạy ở leader): nhắc việc đến hạn/trễ lúc 07:00.
func (m *Module) Jobs(tenants seasonapp.TenantLister, clk clock.Clock, log *slog.Logger) []schedule.Job {
	return []schedule.Job{{
		Name: port.JobTaskReminders, Spec: schedule.Daily{Hour: ReminderHour, Minute: 0}, RunOnStart: true, Timeout: time.Hour,
		Run: func(ctx context.Context) error {
			if h := clk.Now().In(clock.Zone()).Hour(); h < ReminderHour || h >= ReminderLateHour {
				return nil // khởi động ngoài khung giờ nhắc: chờ lượt 07:00
			}
			r, err := m.season.RunTaskReminders(ctx, tenants)
			if log != nil {
				outcome := "ok"
				if err != nil {
					outcome = "error"
				}
				log.Info("nhắc việc đến hạn/trễ", "event", "job."+port.JobTaskReminders, "tenants", r.Tenants, "seasons", r.Seasons,
					"notices", r.Notices, "sent", r.Sent, "dropped", r.Dropped, "failed", r.Failed, "outcome", outcome)
			}
			return err
		},
	}}
}
