SET LOCAL lock_timeout='5s';
ALTER TABLE pf_coop.household ADD COLUMN native_deleted boolean NOT NULL DEFAULT false;
ALTER TABLE pf_coop.household ADD COLUMN native_revision bigint NOT NULL DEFAULT 1 CHECK(native_revision>0);
ALTER TABLE pf_coop.household ADD COLUMN native_code text GENERATED ALWAYS AS
 ('H-'||upper(replace(household_ref::text,'-',''))) STORED;

-- Only metadata. Original PII and its keyed replay digest live in Vault.
CREATE TABLE pf_coop.native_household_command (
 tenant_id uuid NOT NULL,command_ref uuid NOT NULL,household_ref uuid NOT NULL,
 cooperative_ref uuid,actor_ref uuid NOT NULL,operation text NOT NULL CHECK(operation IN ('create','update','delete')),
 idem_key text NOT NULL,expected_revision bigint NOT NULL,request jsonb NOT NULL,
 completed_at timestamptz,failed_code text,created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,command_ref),UNIQUE(tenant_id,actor_ref,idem_key)
);
CREATE UNIQUE INDEX native_household_pending ON pf_coop.native_household_command(tenant_id,household_ref)
 WHERE completed_at IS NULL AND failed_code IS NULL;
SELECT shared.apply_tenant_rls('pf_coop.native_household_command');
CREATE POLICY p_scope ON pf_coop.native_household_command AS RESTRICTIVE FOR ALL TO r_cooperative
 USING(actor_ref=(SELECT k_ref.ctx_uuid('app.actor_ref')) AND ((SELECT k_ref.ctx_uuid('app.node_id')) IS NULL OR cooperative_ref=(SELECT k_ref.ctx_uuid('app.node_id')) OR cooperative_ref IS NULL))
 WITH CHECK(actor_ref=(SELECT k_ref.ctx_uuid('app.actor_ref')) AND ((SELECT k_ref.ctx_uuid('app.node_id')) IS NULL OR cooperative_ref=(SELECT k_ref.ctx_uuid('app.node_id')) OR cooperative_ref IS NULL));
REVOKE ALL ON pf_coop.native_household_command FROM PUBLIC;
GRANT SELECT,INSERT,UPDATE ON pf_coop.native_household_command TO r_cooperative;
GRANT SELECT ON pf_coop.native_household_command TO own_cooperative;
GRANT USAGE ON SCHEMA shared TO own_cooperative;
GRANT EXECUTE ON FUNCTION shared.current_tenant() TO own_cooperative;
CREATE FUNCTION pf_coop.has_pending_native_household(ref uuid) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
 SELECT EXISTS(SELECT 1 FROM pf_coop.native_household_command
 WHERE tenant_id=shared.current_tenant() AND household_ref=ref AND completed_at IS NULL AND failed_code IS NULL)
$$;
ALTER FUNCTION pf_coop.has_pending_native_household(uuid) OWNER TO own_cooperative;
REVOKE ALL ON FUNCTION pf_coop.has_pending_native_household(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION pf_coop.has_pending_native_household(uuid) TO r_cooperative;

-- Integrity checks must include history predating a legacy KN transfer.
-- Only a boolean leaves the owner; tenant is never widened.
GRANT SELECT ON pf_coop.season_event,pf_coop.season_current TO own_cooperative;
GRANT SELECT,UPDATE ON pf_coop.household TO own_cooperative;
-- RLS expressions are permission-checked even for an empty node context.
-- The owner receives only the exact metadata relations those policies read.
GRANT SELECT ON k_ref.node_closure,k_ref.share_scope,k_ref.org TO own_cooperative;
GRANT SELECT ON pf_coop.household_membership TO own_cooperative;
CREATE FUNCTION pf_coop.household_has_production_history(ref uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE previous_node text:=current_setting('app.node_id',true);previous_xa text:=current_setting('app.xa_ids',true);used boolean;
BEGIN
 IF nullif(current_setting('app.actor_ref',true),'') IS NULL OR coalesce(current_setting('app.purpose',true),'')
 NOT IN ('household.native_delete','household.native_commit') THEN
  RAISE EXCEPTION 'household history requires authorized integrity purpose' USING ERRCODE='42501';
 END IF;
 PERFORM set_config('app.node_id','',true),set_config('app.xa_ids','',true);
 SELECT EXISTS(SELECT 1 FROM pf_coop.season_event WHERE tenant_id=shared.current_tenant() AND household_ref=ref)
 OR EXISTS(SELECT 1 FROM pf_coop.season_current WHERE tenant_id=shared.current_tenant() AND household_ref=ref) INTO used;
 PERFORM set_config('app.node_id',coalesce(previous_node,''),true),set_config('app.xa_ids',coalesce(previous_xa,''),true);
 RETURN used;
END $$;
ALTER FUNCTION pf_coop.household_has_production_history(uuid) OWNER TO own_cooperative;
REVOKE ALL ON FUNCTION pf_coop.household_has_production_history(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION pf_coop.household_has_production_history(uuid) TO r_cooperative;

CREATE FUNCTION pf_coop.guard_native_household_command() RETURNS trigger
LANGUAGE plpgsql SECURITY INVOKER SET search_path=pg_catalog,pg_temp AS $$
BEGIN
 IF (NEW.tenant_id,NEW.command_ref,NEW.household_ref,NEW.actor_ref,NEW.operation,NEW.idem_key,NEW.expected_revision,NEW.request,NEW.created_at)
 IS DISTINCT FROM (OLD.tenant_id,OLD.command_ref,OLD.household_ref,OLD.actor_ref,OLD.operation,OLD.idem_key,OLD.expected_revision,OLD.request,OLD.created_at)
 OR OLD.completed_at IS NOT NULL OR (NEW.cooperative_ref IS DISTINCT FROM OLD.cooperative_ref
 AND NOT(NEW.cooperative_ref IS NULL AND NEW.failed_code IS NOT NULL AND NEW.completed_at IS NOT NULL)) THEN
  RAISE EXCEPTION 'immutable household command' USING ERRCODE='23514';
 END IF; RETURN NEW;
END $$;
REVOKE ALL ON FUNCTION pf_coop.guard_native_household_command() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION pf_coop.guard_native_household_command() TO r_cooperative;
CREATE TRIGGER trg_native_household_command BEFORE UPDATE ON pf_coop.native_household_command
 FOR EACH ROW EXECUTE FUNCTION pf_coop.guard_native_household_command();

CREATE FUNCTION pf_coop.guard_native_household_reservation() RETURNS trigger
LANGUAGE plpgsql SECURITY INVOKER SET search_path=pg_catalog,pg_temp AS $$
DECLARE row_data jsonb;
BEGIN
 row_data:=CASE WHEN TG_OP='DELETE' THEN to_jsonb(OLD) ELSE to_jsonb(NEW) END;
 IF pf_coop.has_pending_native_household((row_data->>'household_ref')::uuid)
 AND current_setting('app.purpose',true) IS DISTINCT FROM 'household.native_commit' THEN
  RAISE EXCEPTION 'household reserved by pending command' USING ERRCODE='23514',CONSTRAINT='household_native_pending';
 END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; END IF; RETURN NEW;
END $$;
REVOKE ALL ON FUNCTION pf_coop.guard_native_household_reservation() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION pf_coop.guard_native_household_reservation() TO r_cooperative;
CREATE TRIGGER trg_native_household_reservation BEFORE INSERT ON pf_coop.household
 FOR EACH ROW EXECUTE FUNCTION pf_coop.guard_native_household_reservation();
CREATE TRIGGER trg_native_household_reservation BEFORE INSERT OR UPDATE OR DELETE ON pf_coop.household_membership
 FOR EACH ROW EXECUTE FUNCTION pf_coop.guard_native_household_reservation();

CREATE FUNCTION pf_coop.guard_native_household_lifecycle() RETURNS trigger
LANGUAGE plpgsql SECURITY INVOKER SET search_path=pg_catalog,pg_temp AS $$
BEGIN
 IF pf_coop.has_pending_native_household(OLD.household_ref)
 AND current_setting('app.purpose',true) IS DISTINCT FROM 'household.native_commit' THEN
  RAISE EXCEPTION 'household has a pending native command' USING ERRCODE='23514',CONSTRAINT='household_native_pending';
 END IF;
 IF (NEW.cultivated_area_ha,NEW.livestock_area_ha,NEW.commune_code,NEW.active,NEW.native_deleted)
  IS DISTINCT FROM (OLD.cultivated_area_ha,OLD.livestock_area_ha,OLD.commune_code,OLD.active,OLD.native_deleted)
  AND NEW.native_revision=OLD.native_revision THEN NEW.native_revision:=OLD.native_revision+1; END IF;
 IF OLD.native_deleted AND NOT NEW.native_deleted THEN
  RAISE EXCEPTION 'deleted household cannot be restored' USING ERRCODE='23514';
 END IF;
 IF NOT OLD.native_deleted AND NEW.native_deleted AND (current_setting('app.purpose',true) IS DISTINCT FROM 'household.native_commit'
 OR NOT EXISTS(SELECT 1 FROM pf_coop.native_household_command c JOIN pf_coop.kn_profile_event e
 ON e.tenant_id=c.tenant_id AND e.id=c.command_ref AND e.subject_ref=c.household_ref AND e.actor_ref=c.actor_ref
 WHERE c.tenant_id=OLD.tenant_id AND c.household_ref=OLD.household_ref AND c.actor_ref=k_ref.ctx_uuid('app.actor_ref')
 AND c.operation='delete' AND c.completed_at IS NULL AND c.failed_code IS NULL AND c.expected_revision=OLD.native_revision)) THEN
  RAISE EXCEPTION 'household deletion requires source command and audit event' USING ERRCODE='42501';
 END IF;
 IF NOT OLD.native_deleted AND NEW.native_deleted AND pf_coop.household_has_production_history(OLD.household_ref) THEN
  RAISE EXCEPTION 'household has production history' USING ERRCODE='23514',CONSTRAINT='household_has_production';
 END IF;
 RETURN NEW;
END $$;
REVOKE ALL ON FUNCTION pf_coop.guard_native_household_lifecycle() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION pf_coop.guard_native_household_lifecycle() TO r_cooperative;
CREATE TRIGGER trg_native_household_lifecycle BEFORE UPDATE ON pf_coop.household
 FOR EACH ROW EXECUTE FUNCTION pf_coop.guard_native_household_lifecycle();

CREATE FUNCTION pf_coop.guard_household_production_reference() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE h record;previous_node text:=current_setting('app.node_id',true);previous_xa text:=current_setting('app.xa_ids',true);
 previous_tenant text:=current_setting('app.tenant_id',true);
BEGIN
 IF NEW.household_ref IS NULL THEN RETURN NEW; END IF;
 IF nullif(previous_tenant,'') IS NOT NULL AND previous_tenant::uuid<>NEW.tenant_id THEN RETURN NEW; END IF;
 PERFORM set_config('app.tenant_id',NEW.tenant_id::text,true),set_config('app.node_id','',true),set_config('app.xa_ids','',true);
 SELECT active,native_deleted INTO h FROM pf_coop.household
 WHERE tenant_id=NEW.tenant_id AND household_ref=NEW.household_ref FOR UPDATE;
 IF NOT FOUND THEN
  PERFORM set_config('app.tenant_id',coalesce(previous_tenant,''),true),set_config('app.node_id',coalesce(previous_node,''),true),set_config('app.xa_ids',coalesce(previous_xa,''),true);
  RETURN NEW;
 END IF; -- existing FK/RLS retains its own diagnostics
 IF TG_TABLE_NAME='season_event' AND to_jsonb(NEW)->>'action'='opened'
 AND pf_coop.has_pending_native_household(NEW.household_ref) THEN
  RAISE EXCEPTION 'household reserved by pending command' USING ERRCODE='23514',CONSTRAINT='household_native_pending';
 END IF;
 IF h.native_deleted OR (TG_TABLE_NAME='season_event' AND to_jsonb(NEW)->>'action'='opened' AND NOT h.active) THEN
  RAISE EXCEPTION 'household unavailable for production' USING ERRCODE='23514',CONSTRAINT='household_unavailable';
 END IF;
 PERFORM set_config('app.tenant_id',coalesce(previous_tenant,''),true),set_config('app.node_id',coalesce(previous_node,''),true),set_config('app.xa_ids',coalesce(previous_xa,''),true);
 RETURN NEW;
END $$;
ALTER FUNCTION pf_coop.guard_household_production_reference() OWNER TO own_cooperative;
REVOKE ALL ON FUNCTION pf_coop.guard_household_production_reference() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION pf_coop.guard_household_production_reference() TO r_cooperative;
CREATE TRIGGER trg_household_production_reference BEFORE INSERT ON pf_coop.season_event
 FOR EACH ROW EXECUTE FUNCTION pf_coop.guard_household_production_reference();
-- Projection rebuilds for an inactive household are allowed; deleted households
-- can never have a historical season, so a late producer must be rejected.
CREATE TRIGGER trg_household_production_reference BEFORE INSERT OR UPDATE ON pf_coop.season_current
 FOR EACH ROW EXECUTE FUNCTION pf_coop.guard_household_production_reference();
SELECT shared.publish_cooperative_references('pf_coop','pf_cooperative_pub','r_cooperative_pub');
