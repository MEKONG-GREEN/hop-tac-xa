-- AH-100 (FN-06): khai báo sản lượng của vụ theo hộ dùng chung sổ chỉ ghi thêm harvest_declaration.
-- Vụ theo hộ không có thửa: plot_ref NULL, household_ref bắt buộc; vị trí không đối chiếu ranh thửa nên chỉ có
-- 'missing' (không có GPS) hoặc 'unverified' (có GPS), giống nhật ký việc theo hộ (AH-88). Dòng cũ giữ nguyên, không
-- đổi chuỗi băm; luồng theo thửa giữ ràng buộc cũ.
SET LOCAL lock_timeout = '5s';
ALTER TABLE pf_coop.harvest_declaration ALTER COLUMN plot_ref DROP NOT NULL;
DO $$ DECLARE c record; BEGIN
 FOR c IN SELECT conname FROM pg_constraint WHERE conrelid='pf_coop.harvest_declaration'::regclass AND contype='c'
   AND pg_get_constraintdef(oid) LIKE '%location_status%' LOOP
  EXECUTE format('ALTER TABLE pf_coop.harvest_declaration DROP CONSTRAINT %I',c.conname);
 END LOOP;
END $$;
ALTER TABLE pf_coop.harvest_declaration ADD CONSTRAINT harvest_declaration_entry_shape CHECK ((
 (plot_ref IS NOT NULL AND location_status IN ('match','mismatch','missing'))
 OR
 (plot_ref IS NULL AND household_ref IS NOT NULL AND moisture_pct IS NULL AND distance_m IS NULL
   AND ((gps IS NULL AND location_status='missing') OR (gps IS NOT NULL AND location_status='unverified')))
) IS TRUE) NOT VALID;
ALTER TABLE pf_coop.harvest_declaration VALIDATE CONSTRAINT harvest_declaration_entry_shape;
