SET LOCAL lock_timeout = '5s';
ALTER TABLE pf_party.org_event ADD CONSTRAINT org_event_action_v3
 CHECK(action IN ('created','moved','renamed','deactivated','reactivated','profile_updated','deleted')) NOT VALID;
ALTER TABLE pf_party.org_event VALIDATE CONSTRAINT org_event_action_v3;
ALTER TABLE pf_party.org_event DROP CONSTRAINT org_event_action_v2;
CREATE INDEX org_event_deleted ON pf_party.org_event(tenant_id,org_ref)
 WHERE kind='cooperative' AND action='deleted';

-- Metadata của giao thức vòng đời, không phải dữ liệu nghiệp vụ công bố.
-- Ghi cùng transaction DELETE đã được guard của Party kiểm chứng; writer
-- thường và trigger owner chỉ cần đọc chốt, không nhận thêm vai publication.
CREATE TABLE shared.cooperative_lifecycle_tombstone (
 tenant_id uuid NOT NULL,cooperative_ref uuid NOT NULL,deletion_command_ref uuid NOT NULL,
 source_seq bigint NOT NULL CHECK(source_seq>0),recorded_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,cooperative_ref)
);
SELECT shared.apply_tenant_rls('shared.cooperative_lifecycle_tombstone');
GRANT USAGE,CREATE ON SCHEMA shared TO own_party;
ALTER TABLE shared.cooperative_lifecycle_tombstone OWNER TO own_party;
REVOKE ALL ON shared.cooperative_lifecycle_tombstone FROM PUBLIC;
GRANT SELECT ON shared.cooperative_lifecycle_tombstone TO ag_shared_user,own_cooperative;
GRANT USAGE ON SCHEMA shared TO own_party;
GRANT EXECUTE ON FUNCTION shared.current_tenant() TO own_party;
GRANT EXECUTE ON FUNCTION shared.guard_deleted_cooperative() TO own_cooperative;
CREATE TRIGGER trg_immutable_cooperative_lifecycle BEFORE UPDATE OR DELETE
 ON shared.cooperative_lifecycle_tombstone FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_no_truncate_cooperative_lifecycle BEFORE TRUNCATE
 ON shared.cooperative_lifecycle_tombstone FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable();

CREATE FUNCTION shared.capture_deleted_cooperative() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
BEGIN
 IF TG_OP<>'DELETE' OR TG_LEVEL<>'ROW' OR TG_TABLE_SCHEMA<>'pf_party' OR TG_TABLE_NAME<>'org' THEN
  RAISE EXCEPTION 'invalid cooperative lifecycle source' USING ERRCODE='42501';
 END IF;
 IF OLD.kind<>'cooperative' OR current_setting('app.purpose',true) IS DISTINCT FROM 'registry.cooperative_delete' THEN
  RETURN OLD; -- không biến seed/repair đặc quyền thành thao tác xóa nghiệp vụ
 END IF;
 IF OLD.tenant_id IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
  OR nullif(current_setting('app.actor_ref',true),'') IS NULL
  OR OLD.active OR OLD.last_seq<1 OR OLD.last_event_id IS NULL THEN
  RAISE EXCEPTION 'invalid cooperative lifecycle deletion' USING ERRCODE='23514';
 END IF;
 INSERT INTO shared.cooperative_lifecycle_tombstone(tenant_id,cooperative_ref,deletion_command_ref,source_seq)
 VALUES(OLD.tenant_id,OLD.ref,OLD.last_event_id,OLD.last_seq) ON CONFLICT DO NOTHING;
 RETURN OLD;
END $$;
ALTER FUNCTION shared.capture_deleted_cooperative() OWNER TO own_party;
REVOKE CREATE ON SCHEMA shared FROM own_party;
REVOKE ALL ON FUNCTION shared.capture_deleted_cooperative() FROM PUBLIC;
CREATE TRIGGER trg_deleted_cooperative_lifecycle AFTER DELETE ON pf_party.org
 FOR EACH ROW EXECUTE FUNCTION shared.capture_deleted_cooperative();

ALTER TABLE pf_party.htx_profile_completion DROP CONSTRAINT htx_profile_completion_tenant_id_org_ref_fkey;
ALTER TABLE pf_party.htx_profile_completion ADD CONSTRAINT htx_profile_completion_tenant_id_org_ref_fkey
 FOREIGN KEY(tenant_id,org_ref) REFERENCES pf_party.org(tenant_id,ref) ON DELETE CASCADE NOT VALID;
ALTER TABLE pf_party.htx_profile_completion VALIDATE CONSTRAINT htx_profile_completion_tenant_id_org_ref_fkey;

-- Receipt sau khi Vault tiêu hủy liên hệ riêng HTX. Không FK tới projection
-- đã xóa; command_ref truy vết sự kiện deleted bất biến của chính owner.
CREATE TABLE pf_party.htx_deletion_completion(
 tenant_id uuid NOT NULL,command_ref uuid NOT NULL,completed_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,command_ref)
);
SELECT shared.apply_tenant_rls('pf_party.htx_deletion_completion');
REVOKE ALL ON pf_party.htx_deletion_completion FROM PUBLIC;
GRANT SELECT,INSERT ON pf_party.htx_deletion_completion TO r_party;
