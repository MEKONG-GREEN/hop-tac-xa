-- AH-191 (NTF.2, NTF-BR-03/04): lượt nhắc việc đến hạn/trễ của vụ nông hộ. Mỗi việc tối đa một nhắc "due" và một
-- nhắc "overdue" (khóa chính); chạy lại job hoặc khởi động lại worker không tạo lượt mới. Các việc cùng vụ, cùng loại,
-- cùng lượt dùng chung notice_id = SourceID gửi pf/notify (KH-21: mỗi vụ một thông báo), gửi lại không tạo bản trùng.
-- Chỉ nhãn nghiệp vụ (tên việc, ngày), không PII.
SET LOCAL lock_timeout = '5s';

CREATE TABLE pf_coop.task_reminder (
  tenant_id       uuid        NOT NULL,
  season_ref      uuid        NOT NULL,
  step_seq        integer     NOT NULL CHECK (step_seq BETWEEN 1 AND 1000),
  kind            text        NOT NULL CHECK (kind IN ('due', 'overdue')),
  notice_id       uuid        NOT NULL,
  cooperative_ref uuid        NOT NULL,
  household_ref   uuid        NOT NULL,
  task_name       text        NOT NULL CHECK (length(task_name) BETWEEN 1 AND 200),
  due_date        date        NOT NULL,
  business_date   date        NOT NULL,
  status          text        NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sent', 'dropped')),
  drop_reason     text        NULL CHECK (drop_reason IS NULL OR drop_reason IN ('task_resolved_or_season_closed', 'no_active_member')),
  claimed_at      timestamptz NULL,
  finished_at     timestamptz NULL,
  created_at      timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, season_ref, step_seq, kind),
  CHECK ((status = 'pending') = (finished_at IS NULL)),
  CHECK ((status = 'dropped') = (drop_reason IS NOT NULL))
);
CREATE INDEX task_reminder_pending ON pf_coop.task_reminder (tenant_id, notice_id) WHERE status = 'pending';
SELECT shared.apply_tenant_rls('pf_coop.task_reminder');
ALTER TABLE pf_coop.task_reminder OWNER TO own_cooperative;
REVOKE ALL ON pf_coop.task_reminder FROM PUBLIC;
GRANT SELECT, INSERT, UPDATE ON pf_coop.task_reminder TO r_cooperative;

-- Job cooperative_task_reminders ở worker tìm tenant bằng ag_ops_jobs (chỉ các cột cần lọc — mẫu pf 000114); việc thật
-- chạy bằng r_cooperative trong ngữ cảnh từng tenant (R-05).
GRANT USAGE ON SCHEMA pf_coop TO ag_ops_jobs;
GRANT SELECT (tenant_id, entry_kind, status) ON pf_coop.season_current TO ag_ops_jobs;
GRANT SELECT (tenant_id, status) ON pf_coop.task_reminder TO ag_ops_jobs;
