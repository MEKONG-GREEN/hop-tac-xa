-- AH-61: a household's budget includes seasons opened before an HTX transfer.
-- Only the aggregate is readable through the household's existing RLS scope.
SET LOCAL lock_timeout = '5s';
CREATE TABLE pf_coop.household_season_capacity (
  tenant_id uuid NOT NULL,
  household_ref uuid NOT NULL,
  active_area_ha numeric NOT NULL CHECK (active_area_ha >= 0),
  PRIMARY KEY (tenant_id, household_ref)
);
SELECT shared.apply_tenant_rls('pf_coop.household_season_capacity');
CREATE POLICY p_scope ON pf_coop.household_season_capacity AS RESTRICTIVE FOR SELECT TO r_cooperative
  USING (EXISTS (
    SELECT 1 FROM pf_coop.household h WHERE h.tenant_id=household_season_capacity.tenant_id
      AND h.household_ref=household_season_capacity.household_ref));
INSERT INTO pf_coop.household_season_capacity (tenant_id, household_ref, active_area_ha)
  SELECT tenant_id, household_ref, sum(area_ha) FROM pf_coop.season_current
  WHERE status='open' AND household_ref IS NOT NULL GROUP BY tenant_id, household_ref;
ALTER TABLE pf_coop.household_season_capacity OWNER TO own_cooperative;
REVOKE ALL ON pf_coop.household_season_capacity FROM PUBLIC;
REVOKE ALL ON pf_coop.household_season_capacity FROM r_cooperative;
GRANT SELECT ON pf_coop.household_season_capacity TO r_cooperative;
-- Runtime writes are denied by the SELECT-only ACL above. Keep the standard
-- p_tenant/p_scope policy set required by shared.v_missing_rls; only the private
-- trigger owner may update this projection.

CREATE FUNCTION pf_coop.project_household_capacity() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, pg_temp AS $$
DECLARE
  previous_tenant text := current_setting('app.tenant_id',true);
  source_tenant uuid;
  old_hh uuid;
  new_hh uuid;
  old_area numeric := 0;
  new_area numeric := 0;
  d record;
BEGIN
  IF TG_WHEN<>'AFTER' OR TG_LEVEL<>'ROW' OR TG_RELID<>'pf_coop.season_current'::regclass
     OR TG_OP NOT IN ('INSERT','UPDATE','DELETE') THEN
    RAISE EXCEPTION 'household capacity: invalid trigger source' USING ERRCODE='insufficient_privilege';
  END IF;
  IF TG_OP IN ('UPDATE','DELETE') THEN
    source_tenant:=OLD.tenant_id;
    IF OLD.status='open' THEN old_hh:=OLD.household_ref; old_area:=OLD.area_ha; END IF;
  END IF;
  IF TG_OP IN ('INSERT','UPDATE') THEN
    IF source_tenant IS NOT NULL AND source_tenant<>NEW.tenant_id THEN
      RAISE EXCEPTION 'household capacity: immutable tenant' USING ERRCODE='check_violation';
    END IF;
    source_tenant:=NEW.tenant_id;
    IF NEW.status='open' THEN new_hh:=NEW.household_ref; new_area:=NEW.area_ha; END IF;
  END IF;
  IF nullif(previous_tenant,'') IS NOT NULL AND previous_tenant::uuid<>source_tenant THEN
    RAISE EXCEPTION 'household capacity: tenant context mismatch' USING ERRCODE='insufficient_privilege';
  END IF;
  PERFORM set_config('app.tenant_id',source_tenant::text,true);
  FOR d IN SELECT hh,sum(delta) AS delta FROM
    (VALUES (old_hh,-old_area),(new_hh,new_area)) x(hh,delta)
    WHERE hh IS NOT NULL GROUP BY hh HAVING sum(delta)<>0 ORDER BY hh LOOP
    IF d.delta<0 THEN
      UPDATE pf_coop.household_season_capacity SET active_area_ha=active_area_ha+d.delta
        WHERE tenant_id=source_tenant AND household_ref=d.hh;
      IF NOT FOUND THEN RAISE EXCEPTION 'household capacity: projection missing' USING ERRCODE='check_violation'; END IF;
    ELSE
      INSERT INTO pf_coop.household_season_capacity(tenant_id,household_ref,active_area_ha)
        VALUES(source_tenant,d.hh,d.delta)
        ON CONFLICT(tenant_id,household_ref) DO UPDATE
          SET active_area_ha=household_season_capacity.active_area_ha+EXCLUDED.active_area_ha;
    END IF;
  END LOOP;
  PERFORM set_config('app.tenant_id',coalesce(previous_tenant,''),true);
  RETURN NULL;
END $$;
ALTER FUNCTION pf_coop.project_household_capacity() OWNER TO own_cooperative;
REVOKE ALL ON FUNCTION pf_coop.project_household_capacity() FROM PUBLIC;
CREATE TRIGGER trg_household_capacity AFTER INSERT OR UPDATE OR DELETE ON pf_coop.season_current
  FOR EACH ROW EXECUTE FUNCTION pf_coop.project_household_capacity();
