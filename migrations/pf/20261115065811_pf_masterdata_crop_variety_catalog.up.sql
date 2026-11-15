-- AH-2 (Quản lý loại cây và giống): mục danh mục có thể "đã xóa" (đánh dấu, không xóa bản ghi) khi chưa được dùng ở
-- đâu; sổ catalog_change nhận thêm hành động 'deleted'; chỉ mục tra giống theo loại cây (attrs.crop_code).
-- Forward-only: bản ứng dụng cũ vẫn đọc được cột mới (NULL) và các hành động cũ.
SET LOCAL lock_timeout = '5s';
ALTER TABLE pf_masterdata.catalog_item ADD COLUMN deleted_at timestamptz NULL;
ALTER TABLE pf_masterdata.catalog_item ADD CONSTRAINT catalog_item_deleted_inactive
  CHECK (deleted_at IS NULL OR NOT active) NOT VALID;
ALTER TABLE pf_masterdata.catalog_item VALIDATE CONSTRAINT catalog_item_deleted_inactive;
ALTER TABLE pf_masterdata.catalog_change ADD CONSTRAINT catalog_change_action_v2
  CHECK (action IN ('created', 'updated', 'deactivated', 'reactivated', 'deleted')) NOT VALID;
ALTER TABLE pf_masterdata.catalog_change VALIDATE CONSTRAINT catalog_change_action_v2;
ALTER TABLE pf_masterdata.catalog_change DROP CONSTRAINT catalog_change_action_check;
CREATE INDEX catalog_item_variety_crop ON pf_masterdata.catalog_item (tenant_id, (attrs->>'crop_code'))
  WHERE catalog = 'rice_variety';
