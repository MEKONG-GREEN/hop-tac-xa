-- Bản mã liên hệ HTX là projection PII có thể tiêu hủy. Tombstone và audit
-- không giữ tên/SĐT, ngăn phát lại command cũ làm sống lại dữ liệu đã xóa.
CREATE TABLE vault.htx_contact_erasure(
 tenant_id uuid NOT NULL,cooperative_ref uuid NOT NULL,command_ref uuid NOT NULL,
 erased_at timestamptz NOT NULL DEFAULT now(),erased_by uuid NOT NULL,
 PRIMARY KEY(tenant_id,cooperative_ref),UNIQUE(tenant_id,command_ref)
);
SELECT shared.apply_tenant_rls('vault.htx_contact_erasure');
REVOKE ALL ON vault.htx_contact_erasure FROM PUBLIC;
GRANT SELECT,INSERT ON vault.htx_contact_erasure TO r_identity;
GRANT DELETE ON vault.htx_contact,vault.htx_contact_command TO r_identity;
