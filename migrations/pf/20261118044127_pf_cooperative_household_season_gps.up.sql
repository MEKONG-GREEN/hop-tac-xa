-- AH-64: household GPS is a declaration, not a plot-boundary verification.
-- No ledger rows or hash epochs change; legacy location/date rules remain.
SET LOCAL lock_timeout = '5s';
ALTER TABLE pf_coop.season_event DROP CONSTRAINT season_event_location_status_check;
ALTER TABLE pf_coop.season_event ADD CONSTRAINT season_event_location_status_check
  CHECK ((location_status IN ('match','mismatch','missing')
    OR (entry_kind='household' AND location_status='unverified' AND gps IS NOT NULL)) IS TRUE) NOT VALID;
ALTER TABLE pf_coop.season_event VALIDATE CONSTRAINT season_event_location_status_check;

-- The old sowing-date rule only belongs to plot seasons. A household SOP may
-- have offsets before its independently selected anchor (e.g. perennial crop).
DO $$
DECLARE old_name text;
BEGIN
  SELECT conname INTO STRICT old_name FROM pg_constraint
    WHERE conrelid='pf_coop.season_event'::regclass AND contype='c' AND conparentid=0
      AND pg_get_constraintdef(oid) LIKE '%harvest_open_date >= sowing_date%';
  EXECUTE format('ALTER TABLE pf_coop.season_event DROP CONSTRAINT %I', old_name);
END $$;
ALTER TABLE pf_coop.season_event ADD CONSTRAINT season_event_legacy_forecast_order
  CHECK (entry_kind='household' OR (harvest_open_date>=sowing_date AND expected_harvest_date>=sowing_date)) NOT VALID;
ALTER TABLE pf_coop.season_event VALIDATE CONSTRAINT season_event_legacy_forecast_order;
