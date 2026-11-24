-- AH-192 (NTF.3, NTF-04): nhắc nhẹ khi vụ nông hộ đang canh tác quá 3 ngày chưa có nhật ký. Mốc tính từ ngày có bản
-- ghi gần nhất (hoặc ngày mở vụ); mỗi lần vượt ngưỡng chỉ nhắc một lần: khóa chính (vụ, mốc). Có bản ghi mới thì mốc
-- đổi và lần vượt ngưỡng sau tạo một lượt mới. notice_id = SourceID gửi pf/notify, gửi lại không tạo bản trùng.
SET LOCAL lock_timeout = '5s';

CREATE TABLE pf_coop.log_missing_reminder (
  tenant_id       uuid        NOT NULL,
  season_ref      uuid        NOT NULL,
  since_date      date        NOT NULL,
  notice_id       uuid        NOT NULL,
  cooperative_ref uuid        NOT NULL,
  household_ref   uuid        NOT NULL,
  days            integer     NOT NULL CHECK (days > 3 AND days <= 3650),
  business_date   date        NOT NULL,
  status          text        NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sent', 'dropped')),
  drop_reason     text        NULL CHECK (drop_reason IS NULL OR drop_reason IN ('logged_or_season_closed', 'no_active_member')),
  claimed_at      timestamptz NULL,
  finished_at     timestamptz NULL,
  created_at      timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, season_ref, since_date),
  UNIQUE (tenant_id, notice_id),
  CHECK ((status = 'pending') = (finished_at IS NULL)),
  CHECK ((status = 'dropped') = (drop_reason IS NOT NULL))
);
CREATE INDEX log_missing_reminder_pending ON pf_coop.log_missing_reminder (tenant_id) WHERE status = 'pending';
SELECT shared.apply_tenant_rls('pf_coop.log_missing_reminder');
ALTER TABLE pf_coop.log_missing_reminder OWNER TO own_cooperative;
REVOKE ALL ON pf_coop.log_missing_reminder FROM PUBLIC;
GRANT SELECT, INSERT, UPDATE ON pf_coop.log_missing_reminder TO r_cooperative;

-- Job cooperative_task_reminders tìm tenant còn lượt nhắc chờ bằng ag_ops_jobs (chỉ cột lọc).
GRANT SELECT (tenant_id, status) ON pf_coop.log_missing_reminder TO ag_ops_jobs;

-- FN-03/DATA-03: tham chiếu HTX mới tham gia chốt ghi/xóa Nút HTX.
SELECT shared.publish_cooperative_references('pf_coop','pf_cooperative_pub','r_cooperative_pub');
