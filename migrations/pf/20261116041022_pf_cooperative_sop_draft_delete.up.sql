-- AH-21 (Nhân bản SOP và quản lý phiên bản) / Q-46, quyết định nhóm dự án ngày 16/11/2026: SOP chưa phát hành lần nào
-- được đánh dấu đã xóa (deleted_at), không xóa dòng (RB-1, ADR-0011) — cùng cách với pf_masterdata.catalog_item.deleted_at
-- của AH-2. Mẫu đã đánh dấu xóa là bất biến (không sửa, không khôi phục); tên và mã dùng lại được ngay.
-- Forward-only; bản ứng dụng cũ vẫn đọc được cột mới (NULL). Giữ nguyên trg_no_delete và trg_no_truncate, không cấp DELETE.
SET LOCAL lock_timeout = '5s';

ALTER TABLE pf_coop.sop_template ADD COLUMN deleted_at timestamptz NULL;

-- Chỉ mẫu nháp chưa phát hành lần nào mới đánh dấu xóa được (điều kiện giữ cả ở máy chủ lẫn cơ sở dữ liệu).
ALTER TABLE pf_coop.sop_template ADD CONSTRAINT sop_template_deleted_draft
  CHECK (deleted_at IS NULL OR (status = 'draft' AND published_version IS NULL)) NOT VALID;
ALTER TABLE pf_coop.sop_template VALIDATE CONSTRAINT sop_template_deleted_draft;

CREATE OR REPLACE FUNCTION pf_coop.fn_sop_template_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.deleted_at IS NOT NULL THEN
    RAISE EXCEPTION 'sop_template %: mẫu đã xóa là bất biến', OLD.ref
      USING ERRCODE = 'insufficient_privilege';
  END IF;
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

-- Tên và mã của mẫu đã xóa dùng lại được: ràng buộc duy nhất chuyển thành chỉ mục duy nhất chỉ tính mẫu còn sống.
ALTER TABLE pf_coop.sop_template DROP CONSTRAINT sop_template_tenant_id_name_key_key;
ALTER TABLE pf_coop.sop_template DROP CONSTRAINT sop_template_tenant_id_code_key;
CREATE UNIQUE INDEX sop_template_name_live ON pf_coop.sop_template (tenant_id, name_key) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX sop_template_code_live ON pf_coop.sop_template (tenant_id, code) WHERE deleted_at IS NULL;
