-- AH-152 (FN-06.5, FN-06-B4, EX-05, BR-05): tổng kết "Ghi lại vụ" và hoàn thành vụ theo hộ.
-- Trạng thái mới "closed" ("Đã hoàn thành vụ") chỉ ở vụ theo hộ: vụ đã khai báo sản lượng chuyển tiếp bằng sự kiện
-- closed (không phải bản bù; khai báo và hoàn thành là hai sự kiện liên tiếp, Q-42). Vụ đóng không quay lại.
-- Các luồng ghi đã chặn mặc định mọi trạng thái ngoài open/completed (allowlist). Sổ chỉ ghi thêm, dòng cũ giữ nguyên.
SET LOCAL lock_timeout = '5s';

-- 1. Sổ vụ: hành động closed — chỉ vụ theo hộ, không mang harvest_id (khai báo vẫn ở bản chiếu).
DO $$ DECLARE c record; BEGIN
 FOR c IN SELECT conname FROM pg_constraint WHERE conrelid='pf_coop.season_event'::regclass AND contype='c'
   AND conname IN ('season_event_action_check') LOOP
  EXECUTE format('ALTER TABLE pf_coop.season_event DROP CONSTRAINT %I', c.conname);
 END LOOP;
END $$;
ALTER TABLE pf_coop.season_event
  ADD CONSTRAINT season_event_action_check CHECK (action IN ('opened','corrected','completed','cancelled','reopened','closed')) NOT VALID,
  ADD CONSTRAINT season_event_closed_household CHECK (action <> 'closed' OR entry_kind = 'household') NOT VALID;
ALTER TABLE pf_coop.season_event VALIDATE CONSTRAINT season_event_action_check;
ALTER TABLE pf_coop.season_event VALIDATE CONSTRAINT season_event_closed_household;

-- 2. Bản chiếu vụ: trạng thái closed giữ khai báo hiện hành (harvest_id).
DO $$ DECLARE c record; BEGIN
 FOR c IN SELECT conname FROM pg_constraint WHERE conrelid='pf_coop.season_current'::regclass AND contype='c'
   AND (pg_get_constraintdef(oid) LIKE '%''cancelled''%' OR pg_get_constraintdef(oid) LIKE '%harvest_id IS NOT NULL%') LOOP
  EXECUTE format('ALTER TABLE pf_coop.season_current DROP CONSTRAINT %I', c.conname);
 END LOOP;
END $$;
ALTER TABLE pf_coop.season_current
  ADD CONSTRAINT season_current_status_check CHECK (status IN ('open','completed','cancelled','closed')) NOT VALID,
  ADD CONSTRAINT season_current_harvest_shape CHECK ((status IN ('completed','closed')) = (harvest_id IS NOT NULL)) NOT VALID,
  ADD CONSTRAINT season_current_closed_household CHECK (status <> 'closed' OR entry_kind = 'household') NOT VALID;
ALTER TABLE pf_coop.season_current VALIDATE CONSTRAINT season_current_status_check;
ALTER TABLE pf_coop.season_current VALIDATE CONSTRAINT season_current_harvest_shape;
ALTER TABLE pf_coop.season_current VALIDATE CONSTRAINT season_current_closed_household;

-- 3. Sổ tổng kết: bản chụp cố định lúc hoàn thành vụ (KH-27) — năng suất, vật tư, số việc, chi phí, ghi chú.
CREATE TABLE pf_coop.household_season_summary_event (
 id uuid NOT NULL, tenant_id uuid NOT NULL, stream_key text NOT NULL, seq bigint NOT NULL,
 season_ref uuid NOT NULL, cooperative_ref uuid NOT NULL, household_ref uuid NOT NULL,
 season_event_id uuid NOT NULL,
 summary jsonb NOT NULL CHECK (jsonb_typeof(summary) = 'object'),
 cost_vnd numeric(12,0) NULL CHECK (cost_vnd IS NULL OR cost_vnd >= 0),
 note text NULL CHECK (note IS NULL OR (length(note) BETWEEN 1 AND 1000)),
 event_time timestamptz NOT NULL, recorded_at timestamptz NOT NULL DEFAULT now(),
 actor_ref uuid NOT NULL, device_id text NULL, idem_key text NOT NULL, rule_version text NOT NULL,
 supersedes_id uuid NULL, correction_reason text NULL,
 prev_hash bytea NOT NULL, row_hash bytea NOT NULL,
 PRIMARY KEY (id, recorded_at),
 CHECK (stream_key = 'season_summary:' || season_ref::text),
 CHECK (supersedes_id IS NULL AND correction_reason IS NULL)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX household_season_summary_current ON pf_coop.household_season_summary_event(tenant_id, stream_key, seq DESC);
CREATE INDEX household_season_summary_idem ON pf_coop.household_season_summary_event(tenant_id, idem_key);
SELECT shared.apply_ledger('pf_coop.household_season_summary_event', 'pf_coop.household_season_summary_event');
-- Fail-closed: không có vụ theo hộ khớp HTX/hộ thì không thấy, không ghi được dòng nào.
CREATE POLICY p_scope ON pf_coop.household_season_summary_event AS RESTRICTIVE FOR ALL TO r_cooperative
 USING(EXISTS(SELECT 1 FROM pf_coop.season_current s WHERE s.tenant_id = household_season_summary_event.tenant_id
   AND s.season_ref = household_season_summary_event.season_ref AND s.entry_kind = 'household'
   AND s.cooperative_ref = household_season_summary_event.cooperative_ref AND s.household_ref = household_season_summary_event.household_ref))
 WITH CHECK(EXISTS(SELECT 1 FROM pf_coop.season_current s WHERE s.tenant_id = household_season_summary_event.tenant_id
   AND s.season_ref = household_season_summary_event.season_ref AND s.entry_kind = 'household'
   AND s.cooperative_ref = household_season_summary_event.cooperative_ref AND s.household_ref = household_season_summary_event.household_ref));
ALTER TABLE pf_coop.household_season_summary_event OWNER TO own_cooperative;
REVOKE ALL ON pf_coop.household_season_summary_event FROM PUBLIC;
GRANT SELECT, INSERT ON pf_coop.household_season_summary_event TO r_cooperative;

-- DATA-03 (AH-283): tham chiếu HTX mới tham gia chốt ghi/xóa Nút HTX.
SELECT shared.publish_cooperative_references('pf_coop','pf_cooperative_pub','r_cooperative_pub');
