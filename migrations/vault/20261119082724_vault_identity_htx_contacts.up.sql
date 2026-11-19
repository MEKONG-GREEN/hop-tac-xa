-- FN-03: SĐT của HTX không phải chỉ mục định danh của một người. Mỗi HTX
-- có liên hệ độc lập; không chống trùng SĐT/không sửa chung một subject.
-- Bản mã là dữ liệu riêng tư có thể tiêu hủy, không phải sổ nghiệp vụ.
-- Audit tiếp tục ở access_log append-only; receipt không chứa PII rõ.
CREATE TABLE vault.htx_contact_command (
 tenant_id uuid NOT NULL,
 command_ref uuid NOT NULL,
 cooperative_ref uuid NOT NULL,
 payload_ct bytea NOT NULL,
 wrapped_key bytea NOT NULL,
 kek_id text NOT NULL,
 recorded_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,command_ref)
);
CREATE INDEX htx_contact_command_cooperative ON vault.htx_contact_command(tenant_id,cooperative_ref);
SELECT shared.apply_tenant_rls('vault.htx_contact_command');
CREATE TABLE vault.htx_contact (
 tenant_id uuid NOT NULL,
 cooperative_ref uuid NOT NULL,
 command_ref uuid NOT NULL,
 PRIMARY KEY(tenant_id,cooperative_ref),
 FOREIGN KEY(tenant_id,command_ref) REFERENCES vault.htx_contact_command(tenant_id,command_ref)
);
SELECT shared.apply_tenant_rls('vault.htx_contact');
REVOKE ALL ON vault.htx_contact, vault.htx_contact_command FROM PUBLIC;
GRANT SELECT, INSERT, UPDATE ON vault.htx_contact TO r_identity;
GRANT SELECT, INSERT ON vault.htx_contact_command TO r_identity;
