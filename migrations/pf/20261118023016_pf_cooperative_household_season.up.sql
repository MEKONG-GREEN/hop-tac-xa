-- AH-62: nền dữ liệu vụ theo hộ, chưa bật đường ghi API mới (AH-63/AH-64).
-- Cột thêm vào sổ NULL, không DEFAULT và có đăng ký epoch: không sửa dòng
-- hoặc băm lịch sử. NULL entry_kind là vụ theo thửa cũ, giữ nguyên ràng buộc.
SET LOCAL lock_timeout = '5s';

ALTER TABLE pf_coop.season_event
  ADD COLUMN entry_kind text NULL,
  ADD COLUMN crop_code text NULL,
  ADD COLUMN sop_snapshot jsonb NULL;
SELECT shared.register_ledger_columns('pf_coop.season_event', ARRAY['entry_kind', 'crop_code', 'sop_snapshot']);

ALTER TABLE pf_coop.season_current
  ADD COLUMN entry_kind text NULL,
  ADD COLUMN crop_code text NULL,
  ADD COLUMN sop_snapshot jsonb NULL;

ALTER TABLE pf_coop.season_event
  ALTER COLUMN plot_ref DROP NOT NULL,
  ALTER COLUMN plot_code DROP NOT NULL,
  ALTER COLUMN expected_harvest_date DROP NOT NULL,
  ALTER COLUMN harvest_open_date DROP NOT NULL,
  ADD CONSTRAINT season_event_entry_shape CHECK ((
    (entry_kind IS NULL AND crop_code IS NULL AND sop_snapshot IS NULL
      AND plot_ref IS NOT NULL AND plot_code IS NOT NULL
      AND expected_harvest_date IS NOT NULL AND harvest_open_date IS NOT NULL)
    OR
    (entry_kind = 'household' AND plot_ref IS NULL AND plot_code IS NULL
      AND household_ref IS NOT NULL AND crop_code ~ '^[A-Za-z0-9_.-]{1,40}$'
      AND area_ha = round(area_ha, 2)
      AND ((expected_harvest_date IS NULL) = (harvest_open_date IS NULL))
      AND jsonb_typeof(sop_snapshot) = 'object'
      AND sop_snapshot @> jsonb_build_object('template_ref', sop_template_ref::text,
        'version_id', sop_version_id::text, 'version_no', sop_version_no, 'crop_code', crop_code)
      AND jsonb_typeof(sop_snapshot->'stages') = 'array'
      AND jsonb_typeof(sop_snapshot->'steps') = 'array'
      AND length(btrim(sop_snapshot->>'anchor_name')) > 0)
  ) IS TRUE) NOT VALID;
ALTER TABLE pf_coop.season_event VALIDATE CONSTRAINT season_event_entry_shape;

ALTER TABLE pf_coop.season_current
  ALTER COLUMN plot_ref DROP NOT NULL,
  ALTER COLUMN plot_code DROP NOT NULL,
  ALTER COLUMN expected_harvest_date DROP NOT NULL,
  ALTER COLUMN harvest_open_date DROP NOT NULL,
  ADD CONSTRAINT season_current_entry_shape CHECK ((
    (entry_kind IS NULL AND crop_code IS NULL AND sop_snapshot IS NULL
      AND plot_ref IS NOT NULL AND plot_code IS NOT NULL
      AND expected_harvest_date IS NOT NULL AND harvest_open_date IS NOT NULL)
    OR
    (entry_kind = 'household' AND plot_ref IS NULL AND plot_code IS NULL
      AND household_ref IS NOT NULL AND crop_code ~ '^[A-Za-z0-9_.-]{1,40}$'
      AND area_ha = round(area_ha, 2)
      AND ((expected_harvest_date IS NULL) = (harvest_open_date IS NULL))
      AND jsonb_typeof(sop_snapshot) = 'object'
      AND sop_snapshot @> jsonb_build_object('template_ref', sop_template_ref::text,
        'version_id', sop_version_id::text, 'version_no', sop_version_no, 'crop_code', crop_code)
      AND jsonb_typeof(sop_snapshot->'stages') = 'array'
      AND jsonb_typeof(sop_snapshot->'steps') = 'array'
      AND length(btrim(sop_snapshot->>'anchor_name')) > 0)
  ) IS TRUE) NOT VALID;
ALTER TABLE pf_coop.season_current VALIDATE CONSTRAINT season_current_entry_shape;

-- Giữ season_current_one_open: nhiều NULL plot_ref không xung đột, còn vụ cũ
-- vẫn chỉ có một vụ mở mỗi thửa. Chỉ mục phục vụ tổng diện tích theo hộ của AH-50.
CREATE INDEX season_current_household_open_area ON pf_coop.season_current
  (tenant_id, cooperative_ref, household_ref) INCLUDE (area_ha) WHERE status = 'open';

-- Bảng chiếu sản lượng dùng cùng định danh vụ, cho phép vụ không gắn thửa.
ALTER TABLE pf_coop.yield_summary
  ALTER COLUMN plot_ref DROP NOT NULL,
  ADD CONSTRAINT yield_summary_subject CHECK (plot_ref IS NOT NULL OR household_ref IS NOT NULL) NOT VALID;
ALTER TABLE pf_coop.yield_summary VALIDATE CONSTRAINT yield_summary_subject;

COMMENT ON COLUMN pf_coop.season_event.entry_kind IS 'NULL: vụ theo thửa cũ; household: vụ theo hộ + diện tích, không tạo thửa giả';
COMMENT ON COLUMN pf_coop.season_event.sop_snapshot IS 'Ảnh chụp SOP tại lúc ghi, gồm định danh phiên bản, loại cây, mốc gốc, giai đoạn và việc';
