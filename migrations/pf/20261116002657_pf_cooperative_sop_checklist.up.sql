-- AH-8 (Soạn và phát hành SOP dạng checklist): mở rộng mô hình SOP của pf/cooperative, không dựng mô hình thứ hai.
--   - sop_template: thêm giống (variety_code, tùy chọn) và tên mốc gốc (anchor_name); crop là mã danh mục cây trồng
--     (LUA hoặc UUIDv7 theo AH-2) thay cho mã snake_case cũ; mẫu chưa phát hành lần nào được đổi loại cây.
--   - sop_version: thêm stages (giai đoạn); bỏ luật "người phát hành khác người soạn" (R-56) của SOP — phát hành là
--     một bước của vai soạn (quyết định nhóm dự án 15/11/2026).
-- Forward-only; chạy được trên cơ sở dữ liệu đang có dòng SOP cũ (dòng cũ giữ crop 'lua', anchor_name mặc định).
SET LOCAL lock_timeout = '5s';

-- 1. sop_template
ALTER TABLE pf_coop.sop_template ADD COLUMN variety_code text NULL
  CHECK (variety_code IS NULL OR variety_code ~ '^[A-Za-z0-9_.-]{1,40}$');
ALTER TABLE pf_coop.sop_template ADD COLUMN anchor_name text NOT NULL DEFAULT 'Ngày xuống giống'
  CHECK (char_length(btrim(anchor_name)) BETWEEN 1 AND 100);
ALTER TABLE pf_coop.sop_template ALTER COLUMN anchor_name DROP DEFAULT;

DO $$
DECLARE
  c record;
  found boolean := false;
BEGIN
  FOR c IN
    SELECT conname FROM pg_constraint
    WHERE conrelid = 'pf_coop.sop_template'::regclass AND contype = 'c'
      AND pg_get_constraintdef(oid) LIKE '%crop%~%'
  LOOP
    EXECUTE format('ALTER TABLE pf_coop.sop_template DROP CONSTRAINT %I', c.conname);
    found := true;
  END LOOP;
  IF NOT found THEN
    RAISE EXCEPTION 'sop_template: không tìm thấy ràng buộc dạng của cột crop';
  END IF;
END $$;
ALTER TABLE pf_coop.sop_template ADD CONSTRAINT sop_template_crop_code CHECK (crop ~ '^[A-Za-z0-9_.-]{1,40}$') NOT VALID;
ALTER TABLE pf_coop.sop_template VALIDATE CONSTRAINT sop_template_crop_code;
ALTER TABLE pf_coop.sop_template ALTER COLUMN crop DROP DEFAULT;

CREATE OR REPLACE FUNCTION pf_coop.fn_sop_template_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.status = 'retired' THEN
    RAISE EXCEPTION 'sop_template %: mẫu đã ngừng dùng là bất biến', OLD.ref
      USING ERRCODE = 'insufficient_privilege';
  END IF;
  IF NEW.tenant_id <> OLD.tenant_id OR NEW.ref <> OLD.ref OR NEW.code <> OLD.code OR NEW.scope <> OLD.scope
     OR NEW.cooperative_ref IS DISTINCT FROM OLD.cooperative_ref
     OR (NEW.crop <> OLD.crop AND OLD.published_version IS NOT NULL)
     OR NEW.source_template_ref IS DISTINCT FROM OLD.source_template_ref
     OR NEW.created_by <> OLD.created_by OR NEW.created_at <> OLD.created_at
     OR NEW.latest_version < OLD.latest_version
     OR (OLD.published_version IS NOT NULL AND (NEW.published_version IS NULL OR NEW.published_version < OLD.published_version)) THEN
    RAISE EXCEPTION 'sop_template %: mã, phạm vi, nguồn và lịch sử phiên bản không đổi được', OLD.ref
      USING ERRCODE = 'insufficient_privilege';
  END IF;
  RETURN NEW;
END $$;

-- 2. sop_version
ALTER TABLE pf_coop.sop_version ADD COLUMN stages jsonb NOT NULL DEFAULT '[]'::jsonb
  CHECK (jsonb_typeof(stages) = 'array' AND jsonb_array_length(stages) <= 30);

DO $$
DECLARE
  c record;
  found boolean := false;
BEGIN
  FOR c IN
    SELECT conname FROM pg_constraint
    WHERE conrelid = 'pf_coop.sop_version'::regclass AND contype = 'c'
      AND pg_get_constraintdef(oid) LIKE '%published_by <> authored_by%'
  LOOP
    EXECUTE format('ALTER TABLE pf_coop.sop_version DROP CONSTRAINT %I', c.conname);
    found := true;
  END LOOP;
  IF NOT found THEN
    RAISE EXCEPTION 'sop_version: không tìm thấy ràng buộc published_by <> authored_by';
  END IF;
END $$;
