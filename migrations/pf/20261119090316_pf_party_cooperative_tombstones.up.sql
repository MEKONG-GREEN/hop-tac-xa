SET LOCAL lock_timeout = '5s';
ALTER TABLE pf_party.org_event ADD CONSTRAINT org_event_action_v3
 CHECK(action IN ('created','moved','renamed','deactivated','reactivated','profile_updated','deleted')) NOT VALID;
ALTER TABLE pf_party.org_event VALIDATE CONSTRAINT org_event_action_v3;
ALTER TABLE pf_party.org_event DROP CONSTRAINT org_event_action_v2;
CREATE INDEX org_event_deleted ON pf_party.org_event(tenant_id,org_ref)
 WHERE kind='cooperative' AND action='deleted';

CREATE VIEW pf_party_pub.v1_cooperative_tombstones WITH(security_barrier=true) AS
 SELECT DISTINCT tenant_id,org_ref AS cooperative_ref FROM pf_party.org_event
 WHERE kind='cooperative' AND action='deleted';
REVOKE ALL ON pf_party_pub.v1_cooperative_tombstones FROM PUBLIC;
GRANT SELECT ON pf_party_pub.v1_cooperative_tombstones TO r_party_pub;
-- capture_plot_scope_loss chạy dưới own_cooperative và ghi metadata mất
-- phạm vi của thửa. Guard tham chiếu mới cũng phải kiểm được dấu xóa ở đây,
-- không cấp cả r_party_pub hay quyền bảng riêng cho vai owner.
GRANT USAGE ON SCHEMA pf_party_pub TO own_cooperative;
GRANT SELECT ON pf_party_pub.v1_cooperative_tombstones TO own_cooperative;
GRANT EXECUTE ON FUNCTION shared.guard_deleted_cooperative() TO own_cooperative;

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
