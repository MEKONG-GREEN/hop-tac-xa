-- AH-126 (FN-05.4, FN-05-BR-07): sửa, xóa mềm nhật ký việc của vụ theo hộ và bỏ tích việc đã xong.
--
-- Sửa và xóa đều là bản ghi bù chỉ ghi thêm của sổ farm_log (supersedes_id + correction_reason ≥ 10 ký tự, cùng
-- log_ref; GPS và event_time giữ như bản đầu). Xóa mềm/bỏ tích ghi task_completion.state = 'voided': bản ghi bị ẩn
-- khỏi checklist và tiến độ, lịch sử các phiên bản vẫn đọc được. Trạng thái 'voided' chỉ có ở bản ghi bù.
-- Không sửa dòng cũ, không DELETE.

SET LOCAL lock_timeout = '5s';

ALTER TABLE pf_coop.farm_log DROP CONSTRAINT farm_log_entry_shape;
ALTER TABLE pf_coop.farm_log ADD CONSTRAINT farm_log_entry_shape CHECK ((
 (entry_kind IS NULL AND task_completion IS NULL AND plot_ref IS NOT NULL
   AND location_status IN ('match','mismatch','missing')
   AND activity_code IN ('lam_dat','gieo_sa','tuoi_nuoc','bon_phan','phun_thuoc','kiem_tra','thu_hoach'))
 OR
 (entry_kind='household' AND plot_ref IS NULL AND household_ref IS NOT NULL AND sop_step_seq>=1
   AND batch_ref IS NULL AND jsonb_typeof(task_completion)='object'
   AND task_completion->>'state' IN ('done','partial','skipped','voided')
   AND (task_completion->>'step_seq')::integer=sop_step_seq
   AND (task_completion->>'stage_seq')::integer>=1
   AND (task_completion->>'sop_version_id')::uuid IS NOT NULL
   AND (task_completion->>'season_event_id')::uuid IS NOT NULL
   AND jsonb_typeof(task_completion->'step')='object'
   AND (task_completion->'step'->>'seq')::integer=sop_step_seq
   AND (task_completion->'step'->>'stage_seq')::integer=(task_completion->>'stage_seq')::integer
   AND task_completion->'step'->>'activity_code'=activity_code
   AND task_completion->>'done_on' ~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}$'
   AND (task_completion->>'done_on')::date <= (event_time AT TIME ZONE 'Asia/Ho_Chi_Minh')::date
   AND activity_code IN ('lam_dat','gieo_sa','tuoi_nuoc','bon_phan','phun_thuoc','kiem_tra','thu_hoach','cat_tia','khac')
   AND ((gps IS NULL AND location_status='missing') OR (gps IS NOT NULL AND location_status='unverified')))
) IS TRUE) NOT VALID;
ALTER TABLE pf_coop.farm_log VALIDATE CONSTRAINT farm_log_entry_shape;

ALTER TABLE pf_coop.farm_log ADD CONSTRAINT farm_log_household_void_is_correction CHECK (
  entry_kind IS DISTINCT FROM 'household' OR task_completion->>'state' <> 'voided' OR supersedes_id IS NOT NULL
) NOT VALID;
ALTER TABLE pf_coop.farm_log VALIDATE CONSTRAINT farm_log_household_void_is_correction;

ALTER TABLE pf_coop.household_task_current DROP CONSTRAINT household_task_current_state_check;
ALTER TABLE pf_coop.household_task_current ADD CONSTRAINT household_task_current_state_check
  CHECK (state IN ('done','partial','skipped','voided')) NOT VALID;
ALTER TABLE pf_coop.household_task_current VALIDATE CONSTRAINT household_task_current_state_check;
