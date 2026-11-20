-- Ngoại lệ DATA-03 chỉ dành cho HTX chưa dùng, không mở quyền xóa cứng chung.
-- Kiểm lại ở DB để không phụ thuộc vào việc một caller có bỏ qua app preflight.
CREATE FUNCTION pf_party.guard_unused_cooperative_delete() RETURNS trigger
LANGUAGE plpgsql SECURITY INVOKER SET search_path=pg_catalog,pg_temp AS $$
DECLARE source record;present boolean;actor uuid;
BEGIN
 IF TG_OP<>'DELETE' OR TG_TABLE_SCHEMA<>'pf_party' OR TG_TABLE_NAME<>'org' THEN
  RAISE EXCEPTION 'invalid unused cooperative delete trigger' USING ERRCODE='42501';
 END IF;
 -- Quản trị cụm/maintenance đã có thể tắt trigger; không đổi luồng seed/repair
 -- đặc quyền. Vai ứng dụng không có BYPASSRLS hay SUPERUSER.
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname=current_user AND (rolsuper OR rolbypassrls)) THEN RETURN OLD; END IF;
 actor:=nullif(current_setting('app.actor_ref',true),'')::uuid;
 IF OLD.kind<>'cooperative' OR actor IS NULL OR shared.current_tenant() IS DISTINCT FROM OLD.tenant_id
  OR current_setting('app.purpose',true) IS DISTINCT FROM 'registry.cooperative_delete' THEN
  RAISE EXCEPTION 'only authorized unused cooperative deletion is supported' USING ERRCODE='42501';
 END IF;
 IF NOT EXISTS(SELECT 1 FROM pf_party.org_event e WHERE e.tenant_id=OLD.tenant_id AND e.org_ref=OLD.ref
  AND e.id=OLD.last_event_id AND e.seq=OLD.last_seq AND e.action='deleted' AND e.actor_ref=actor) THEN
  RAISE EXCEPTION 'cooperative deletion requires its source event' USING ERRCODE='23514';
 END IF;
 PERFORM pg_advisory_xact_lock(hashtextextended('cooperative-life:'||OLD.tenant_id::text||':'||OLD.ref::text,283));
 IF EXISTS(SELECT 1 FROM shared.unprotected_cooperative_references()) THEN
  RAISE EXCEPTION 'cooperative reference coverage is incomplete' USING ERRCODE='55000';
 END IF;
 FOR source IN SELECT n.nspname,c.relname FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
  WHERE n.nspname LIKE 'pf\_%\_pub' AND c.relname='v1_cooperative_references' AND c.relkind='v' LOOP
  EXECUTE format('SELECT EXISTS(SELECT 1 FROM %I.%I WHERE tenant_id=$1 AND cooperative_ref=$2)',source.nspname,source.relname)
   INTO present USING OLD.tenant_id,OLD.ref;
  IF present THEN RAISE EXCEPTION 'cooperative has business references' USING ERRCODE='23503',CONSTRAINT='cooperative_has_references'; END IF;
 END LOOP;
 RETURN OLD;
END $$;
REVOKE ALL ON FUNCTION pf_party.guard_unused_cooperative_delete() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION pf_party.guard_unused_cooperative_delete() TO r_party;
CREATE TRIGGER trg_unused_cooperative_delete BEFORE DELETE ON pf_party.org
FOR EACH ROW EXECUTE FUNCTION pf_party.guard_unused_cooperative_delete();
GRANT DELETE ON pf_party.org TO r_party;
-- Hook của bảng nguồn do Party cài; hàm kiểm sổ riêng vẫn thuộc Cooperative.
CREATE TRIGGER zzzz_private_cooperative_delete BEFORE DELETE ON pf_party.org
 FOR EACH ROW EXECUTE FUNCTION pf_coop.guard_private_cooperative_delete();
