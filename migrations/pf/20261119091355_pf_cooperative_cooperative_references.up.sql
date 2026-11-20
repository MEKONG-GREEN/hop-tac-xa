-- AH-283: owner publication + writer guard; no application/business code in DB-only modules.
SET LOCAL lock_timeout = '5s';
CREATE SCHEMA IF NOT EXISTS pf_cooperative_pub;
REVOKE ALL ON SCHEMA pf_cooperative_pub FROM PUBLIC;
GRANT USAGE ON SCHEMA pf_cooperative_pub TO r_cooperative_pub;
-- ADR-0034: không xuất sổ song phương ra _pub, kể cả metadata theo bản ghi.
-- Port chỉ trả boolean phục vụ DATA-03; kiểm lại tại DB khi xóa dưới lifecycle lock.
-- Hoàn tất ownership của đúng hai sổ cho vai owner đã có (ADR-0038).
-- Không đổi hàng, chính sách FORCE RLS hay quyền SELECT/INSERT của ứng dụng;
-- không cấp thêm quyền đọc sổ cho module/role khác.
ALTER TABLE pf_coop.bilateral_pair_event OWNER TO own_cooperative;
ALTER TABLE pf_coop.bilateral_record OWNER TO own_cooperative;
-- Vai owner cần helper chỉ đọc GUC để thực thi chính sách p_pair hiện hữu;
-- không cấp quyền bảng/ledger hoặc vai tổng hợp mới.
GRANT USAGE ON SCHEMA k_ref TO own_cooperative;
GRANT EXECUTE ON FUNCTION k_ref.ctx_uuid(text) TO own_cooperative;
GRANT EXECUTE ON FUNCTION k_ref.ctx_text(text) TO own_cooperative;
GRANT EXECUTE ON FUNCTION k_ref.ctx_reject(text) TO own_cooperative;
CREATE FUNCTION pf_coop.cooperative_has_private_references(ref uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE previous_node text:=current_setting('app.node_id',true);used boolean;
BEGIN
 IF shared.current_tenant() IS NULL OR ref IS NULL
  OR current_setting('app.purpose',true) IS DISTINCT FROM 'registry.cooperative_delete'
  OR nullif(current_setting('app.actor_ref',true),'') IS NULL THEN
  RAISE EXCEPTION 'invalid cooperative deletion check context' USING ERRCODE='42501';
 END IF;
 -- Chỉ kiểm tồn tại trong đúng Nút/tenant; không mở RLS hay trả payload cặp.
 PERFORM set_config('app.node_id',ref::text,true);
 SELECT EXISTS(SELECT 1 FROM pf_coop.bilateral_pair_event WHERE tenant_id=shared.current_tenant() AND htx_node_id=ref)
  OR EXISTS(SELECT 1 FROM pf_coop.bilateral_record WHERE tenant_id=shared.current_tenant() AND htx_node_id=ref) INTO used;
 PERFORM set_config('app.node_id',coalesce(previous_node,''),true);
 RETURN used;
END $$;
ALTER FUNCTION pf_coop.cooperative_has_private_references(uuid) OWNER TO own_cooperative;
REVOKE ALL ON FUNCTION pf_coop.cooperative_has_private_references(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION pf_coop.cooperative_has_private_references(uuid) TO r_cooperative;

CREATE FUNCTION pf_coop.guard_private_cooperative_delete() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
BEGIN
 IF TG_OP<>'DELETE' OR TG_LEVEL<>'ROW' OR TG_TABLE_SCHEMA<>'pf_party' OR TG_TABLE_NAME<>'org' THEN
  RAISE EXCEPTION 'invalid private cooperative deletion source' USING ERRCODE='42501';
 END IF;
 IF OLD.kind<>'cooperative' THEN RETURN OLD; END IF;
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname=session_user AND (rolsuper OR rolbypassrls)) THEN RETURN OLD; END IF;
 IF OLD.tenant_id IS DISTINCT FROM shared.current_tenant() THEN
  RAISE EXCEPTION 'cooperative deletion tenant mismatch' USING ERRCODE='42501';
 END IF;
 IF pf_coop.cooperative_has_private_references(OLD.ref) THEN
  RAISE EXCEPTION 'cooperative has private business references'
   USING ERRCODE='23503',CONSTRAINT='cooperative_has_private_references';
 END IF;
 RETURN OLD;
END $$;
ALTER FUNCTION pf_coop.guard_private_cooperative_delete() OWNER TO own_cooperative;
REVOKE ALL ON FUNCTION pf_coop.guard_private_cooperative_delete() FROM PUBLIC;
SELECT shared.publish_cooperative_references('pf_coop','pf_cooperative_pub','r_cooperative_pub');
