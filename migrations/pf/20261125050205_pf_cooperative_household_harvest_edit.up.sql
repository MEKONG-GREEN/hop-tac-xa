-- AH-189 (FN-06.3, FN-06.4): sửa và hủy khai báo sản lượng của vụ theo hộ. Sổ chỉ ghi thêm, dòng cũ và chuỗi băm
-- giữ nguyên; luồng theo thửa không đổi.
SET LOCAL lock_timeout = '5s';

-- 1. Sổ vụ: hành động "reopened" — hủy khai báo đưa vụ "Đã thu hoạch" về "Đang canh tác". Là bản bù của sự kiện
--    completed (supersedes_id + correction_reason = lý do hủy); chỉ vụ theo hộ.
DO $$ DECLARE c record; BEGIN
 FOR c IN SELECT conname FROM pg_constraint WHERE conrelid='pf_coop.season_event'::regclass AND contype='c'
   AND (pg_get_constraintdef(oid) LIKE '%''opened''%' OR pg_get_constraintdef(oid) LIKE '%supersedes_id IS NOT NULL%') LOOP
  EXECUTE format('ALTER TABLE pf_coop.season_event DROP CONSTRAINT %I', c.conname);
 END LOOP;
END $$;
ALTER TABLE pf_coop.season_event
  ADD CONSTRAINT season_event_action_check CHECK (action IN ('opened','corrected','completed','cancelled','reopened')) NOT VALID,
  ADD CONSTRAINT season_event_correction_shape CHECK ((action IN ('corrected','cancelled','reopened')) = (supersedes_id IS NOT NULL)) NOT VALID,
  ADD CONSTRAINT season_event_reopened_household CHECK (action <> 'reopened' OR entry_kind = 'household') NOT VALID;
ALTER TABLE pf_coop.season_event VALIDATE CONSTRAINT season_event_action_check;
ALTER TABLE pf_coop.season_event VALIDATE CONSTRAINT season_event_correction_shape;
ALTER TABLE pf_coop.season_event VALIDATE CONSTRAINT season_event_reopened_household;

-- 2. Sổ khai báo: dòng hủy (voided) là bản bù của khai báo hiện hành, chỉ ở vụ theo hộ. Cột NULL, không DEFAULT,
--    đăng ký epoch: không đổi băm dòng cũ.
ALTER TABLE pf_coop.harvest_declaration ADD COLUMN voided boolean NULL;
SELECT shared.register_ledger_columns('pf_coop.harvest_declaration', ARRAY['voided']);
ALTER TABLE pf_coop.harvest_declaration ADD CONSTRAINT harvest_declaration_voided_shape CHECK (
  voided IS NULL OR (voided AND plot_ref IS NULL AND household_ref IS NOT NULL AND supersedes_id IS NOT NULL)) NOT VALID;
ALTER TABLE pf_coop.harvest_declaration VALIDATE CONSTRAINT harvest_declaration_voided_shape;
