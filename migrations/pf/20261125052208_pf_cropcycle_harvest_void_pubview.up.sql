-- AH-189: pubview v1_harvest_declaration không đếm khai báo đã hủy (cột voided do migration
-- 20261125050205_pf_cooperative_household_harvest_edit thêm vào sổ harvest_declaration).
SET LOCAL lock_timeout = '5s';
-- Pubview: bỏ mọi dòng đứng trước (hoặc là) một dòng hủy trong cùng chuỗi, để "dòng mới nhất mỗi chuỗi" của bên
--    đọc không đếm khai báo đã hủy; khai báo lại sau khi hủy vẫn hiện. Giữ nguyên danh sách cột (v1).
CREATE OR REPLACE VIEW pf_cropcycle_pub.v1_harvest_declaration AS
  SELECT h.tenant_id, h.id, h.stream_key, h.seq, h.recorded_at, h.mass_kg, h.area_ha, h.commune_code, h.season_code, h.season_year
    FROM pf_coop.harvest_declaration h
   WHERE NOT EXISTS (SELECT 1 FROM pf_coop.harvest_declaration v
                      WHERE v.tenant_id = h.tenant_id AND v.stream_key = h.stream_key AND v.voided AND v.seq >= h.seq);
