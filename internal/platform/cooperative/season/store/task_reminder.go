package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TaskReminder là một lượt nhắc của một việc (AH-191): mỗi việc tối đa một nhắc "due" và một nhắc "overdue" (khóa
// chính chặn trùng). Các việc cùng vụ, cùng loại, cùng lượt dùng chung NoticeID (một thông báo mỗi vụ — KH-21).
type TaskReminder struct {
	SeasonRef, CooperativeRef, HouseholdRef, NoticeID uuid.UUID
	StepSeq                                           int
	Kind, TaskName                                    string
	DueDate, BusinessDate                             time.Time
}

// ReminderTenants liệt kê tenant có vụ nông hộ đang canh tác hoặc còn lượt nhắc chưa xong (pool ag_ops_jobs).
func ReminderTenants(ctx context.Context, pool *pgxpool.Pool) ([]uuid.UUID, error) {
	rows, err := pool.Query(ctx, `SELECT tenant_id FROM pf_coop.season_current WHERE entry_kind='household' AND status='open'
	 UNION SELECT tenant_id FROM pf_coop.task_reminder WHERE status='pending'
	 UNION SELECT tenant_id FROM pf_coop.log_missing_reminder WHERE status='pending'`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}

// OpenHouseholdSeasons trả vụ nông hộ đang canh tác của tenant hiện tại theo season_ref (cursor).
func OpenHouseholdSeasons(ctx context.Context, tx pgx.Tx, after uuid.UUID, limit int) ([]Season, error) {
	rows, err := tx.Query(ctx, "SELECT "+seasonCols+` FROM pf_coop.season_current s
	 WHERE s.entry_kind='household' AND s.status='open' AND s.season_ref>$1 ORDER BY s.season_ref LIMIT $2`, after, limit)
	if err != nil {
		return nil, err
	}
	return collectSeasons(rows)
}

// HouseholdNativeCode là mã nông hộ hiển thị (không phải PII).
func HouseholdNativeCode(ctx context.Context, tx pgx.Tx, household uuid.UUID) (string, error) {
	var code string
	err := tx.QueryRow(ctx, `SELECT native_code FROM pf_coop.household WHERE tenant_id=shared.current_tenant() AND household_ref=$1`, household).Scan(&code)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return code, err
}

// InsertTaskReminders ghi các lượt nhắc mới; việc đã có lượt nhắc cùng loại bị bỏ qua (ON CONFLICT DO NOTHING).
func InsertTaskReminders(ctx context.Context, tx pgx.Tx, rs []TaskReminder) error {
	for _, r := range rs {
		if _, err := tx.Exec(ctx, `INSERT INTO pf_coop.task_reminder (tenant_id, season_ref, step_seq, kind, notice_id, cooperative_ref,
		 household_ref, task_name, due_date, business_date) VALUES (shared.current_tenant(),$1,$2,$3,$4,$5,$6,$7,$8,$9)
		 ON CONFLICT (tenant_id, season_ref, step_seq, kind) DO NOTHING`,
			r.SeasonRef, r.StepSeq, r.Kind, r.NoticeID, r.CooperativeRef, r.HouseholdRef, r.TaskName, r.DueDate, r.BusinessDate); err != nil {
			return err
		}
	}
	return nil
}

// ClaimTaskReminders nhận nguyên tử các lượt nhắc đang chờ (chưa nhận hoặc nhận đã quá staleBefore — tiến trình trước
// dừng giữa chừng). Gửi lại dùng cùng NoticeID nên bên thông báo không tạo bản trùng.
func ClaimTaskReminders(ctx context.Context, tx pgx.Tx, now, staleBefore time.Time) ([]TaskReminder, error) {
	rows, err := tx.Query(ctx, `UPDATE pf_coop.task_reminder SET claimed_at=$1
	 WHERE tenant_id=shared.current_tenant() AND status='pending' AND (claimed_at IS NULL OR claimed_at<$2)
	 RETURNING season_ref, cooperative_ref, household_ref, notice_id, step_seq, kind, task_name, due_date, business_date`, now, staleBefore)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (TaskReminder, error) {
		var r TaskReminder
		err := row.Scan(&r.SeasonRef, &r.CooperativeRef, &r.HouseholdRef, &r.NoticeID, &r.StepSeq, &r.Kind, &r.TaskName, &r.DueDate, &r.BusinessDate)
		return r, err
	})
}

// FinishTaskReminders đóng các lượt nhắc của một việc: sent (đã giao cho pf/notify) hoặc dropped kèm lý do.
func FinishTaskReminders(ctx context.Context, tx pgx.Tx, notice uuid.UUID, steps []int, status, reason string, now time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE pf_coop.task_reminder SET status=$3, drop_reason=nullif($4,''), finished_at=$5
	 WHERE tenant_id=shared.current_tenant() AND notice_id=$1 AND step_seq=ANY($2) AND status='pending'`, notice, steps, status, reason, now)
	return err
}

// LogMissingReminder là một lượt nhắc "vụ quá 3 ngày chưa có nhật ký" (AH-192): một lượt cho mỗi mốc SinceDate.
type LogMissingReminder struct {
	SeasonRef, CooperativeRef, HouseholdRef, NoticeID uuid.UUID
	SinceDate, BusinessDate                           time.Time
	Days                                              int
}

// LastLogDays trả ngày (giờ Việt Nam) có bản ghi nhật ký gần nhất của từng vụ.
func LastLogDays(ctx context.Context, tx pgx.Tx, seasons []uuid.UUID) (map[uuid.UUID]time.Time, error) {
	out := map[uuid.UUID]time.Time{}
	if len(seasons) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, `SELECT season_ref, max((event_time AT TIME ZONE 'Asia/Ho_Chi_Minh')::date)
	 FROM pf_coop.farm_log WHERE tenant_id=shared.current_tenant() AND season_ref=ANY($1) GROUP BY season_ref`, seasons)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var ref uuid.UUID
		var day time.Time
		if err := rows.Scan(&ref, &day); err != nil {
			return nil, err
		}
		out[ref] = day
	}
	return out, rows.Err()
}

// InsertLogMissingReminders ghi lượt nhắc mới; mốc đã có lượt bị bỏ qua.
func InsertLogMissingReminders(ctx context.Context, tx pgx.Tx, rs []LogMissingReminder) error {
	for _, r := range rs {
		if _, err := tx.Exec(ctx, `INSERT INTO pf_coop.log_missing_reminder (tenant_id, season_ref, since_date, notice_id, cooperative_ref,
		 household_ref, days, business_date) VALUES (shared.current_tenant(),$1,$2,$3,$4,$5,$6,$7)
		 ON CONFLICT (tenant_id, season_ref, since_date) DO NOTHING`,
			r.SeasonRef, r.SinceDate, r.NoticeID, r.CooperativeRef, r.HouseholdRef, r.Days, r.BusinessDate); err != nil {
			return err
		}
	}
	return nil
}

// ClaimLogMissingReminders nhận nguyên tử các lượt nhắc đang chờ (như ClaimTaskReminders).
func ClaimLogMissingReminders(ctx context.Context, tx pgx.Tx, now, staleBefore time.Time) ([]LogMissingReminder, error) {
	rows, err := tx.Query(ctx, `UPDATE pf_coop.log_missing_reminder SET claimed_at=$1
	 WHERE tenant_id=shared.current_tenant() AND status='pending' AND (claimed_at IS NULL OR claimed_at<$2)
	 RETURNING season_ref, cooperative_ref, household_ref, notice_id, since_date, business_date, days`, now, staleBefore)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (LogMissingReminder, error) {
		var r LogMissingReminder
		err := row.Scan(&r.SeasonRef, &r.CooperativeRef, &r.HouseholdRef, &r.NoticeID, &r.SinceDate, &r.BusinessDate, &r.Days)
		return r, err
	})
}

// FinishLogMissingReminder đóng một lượt nhắc: sent hoặc dropped kèm lý do.
func FinishLogMissingReminder(ctx context.Context, tx pgx.Tx, notice uuid.UUID, status, reason string, now time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE pf_coop.log_missing_reminder SET status=$2, drop_reason=nullif($3,''), finished_at=$4
	 WHERE tenant_id=shared.current_tenant() AND notice_id=$1 AND status='pending'`, notice, status, reason, now)
	return err
}
