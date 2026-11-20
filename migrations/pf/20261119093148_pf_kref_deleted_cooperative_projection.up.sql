-- Bounded projection như create-sync; không cấp DML k_ref cho Party.
CREATE FUNCTION k_ref.project_deleted_cooperative() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE previous_tenant text:=current_setting('app.tenant_id',true);
BEGIN
 IF TG_OP<>'DELETE' OR TG_LEVEL<>'ROW' OR TG_TABLE_SCHEMA<>'pf_party' OR TG_TABLE_NAME<>'org' THEN
  RAISE EXCEPTION 'invalid cooperative deletion source' USING ERRCODE='42501';
 END IF;
 IF OLD.kind<>'cooperative' THEN RETURN OLD; END IF;
 IF OLD.tenant_id IS NULL OR OLD.ref IS NULL OR OLD.last_seq<1 THEN
  RAISE EXCEPTION 'invalid cooperative deletion state' USING ERRCODE='23514';
 END IF;
 IF nullif(previous_tenant,'') IS NOT NULL AND previous_tenant::uuid<>OLD.tenant_id THEN
  RAISE EXCEPTION 'cooperative deletion tenant mismatch' USING ERRCODE='42501';
 END IF;
 PERFORM set_config('app.tenant_id',OLD.tenant_id::text,true);
 PERFORM pg_advisory_xact_lock(hashtextextended('k_ref:'||OLD.tenant_id::text,0));
 INSERT INTO k_ref.org(tenant_id,ref,kind,code,admin_unit_code,parent_ref,active,source_seq)
 VALUES(OLD.tenant_id,OLD.ref,OLD.kind,OLD.code,OLD.admin_unit_code,OLD.parent_ref,false,OLD.last_seq)
 ON CONFLICT(tenant_id,ref) DO UPDATE SET active=false,source_seq=EXCLUDED.source_seq
 WHERE k_ref.org.source_seq<=EXCLUDED.source_seq;
 IF NOT FOUND THEN
  RAISE EXCEPTION 'organization projection is ahead of source deletion'
   USING ERRCODE='23503',CONSTRAINT='kref_deleted_source_ahead';
 END IF;
 PERFORM set_config('app.tenant_id',coalesce(previous_tenant,''),true);
 RETURN OLD;
END $$;
ALTER FUNCTION k_ref.project_deleted_cooperative() OWNER TO own_kref;
REVOKE ALL ON FUNCTION k_ref.project_deleted_cooperative() FROM PUBLIC;
