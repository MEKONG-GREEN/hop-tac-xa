-- FN-03: ý định sửa hồ sơ nằm ở org_event. Chỉ bổ sung CHECK, không thêm
-- cột/sửa dòng sổ cũ để giữ nguyên chuỗi hash (bài học W4.1).
SET LOCAL lock_timeout = '5s';
ALTER TABLE pf_party.org_event ADD CONSTRAINT org_event_action_v2
  CHECK (action IN ('created', 'moved', 'renamed', 'deactivated', 'reactivated', 'profile_updated')) NOT VALID;
ALTER TABLE pf_party.org_event VALIDATE CONSTRAINT org_event_action_v2;
ALTER TABLE pf_party.org_event DROP CONSTRAINT org_event_action_check;

-- Receipt hoàn tất sau khi các owner đã ghi lũy đẳng. Thiếu receipt của một
-- ý định trong sổ luôn được coi là CHƯA xong, kể cả khi projection bị rỗng.
CREATE TABLE pf_party.htx_profile_completion (
  tenant_id uuid NOT NULL,
  command_ref uuid NOT NULL,
  org_ref uuid NOT NULL,
  completed_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, command_ref),
  FOREIGN KEY (tenant_id, org_ref) REFERENCES pf_party.org(tenant_id, ref)
);
CREATE INDEX htx_profile_completion_org ON pf_party.htx_profile_completion(tenant_id, org_ref);
SELECT shared.apply_tenant_rls('pf_party.htx_profile_completion');
REVOKE ALL ON pf_party.htx_profile_completion FROM PUBLIC;
GRANT SELECT, INSERT ON pf_party.htx_profile_completion TO r_party;
