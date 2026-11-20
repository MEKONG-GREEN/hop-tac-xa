-- Chốt trước I/O Keycloak, không giữ DB transaction trong lúc gọi IAM.
-- Không PII: operation key là digest actor/idempotency-key của yêu cầu.
CREATE TABLE pf_accounts.cooperative_identity_reservation (
 tenant_id uuid NOT NULL,
 operation_ref uuid NOT NULL,
 cooperative_ref uuid NOT NULL,
 operation_key text NOT NULL,
 request_hash text NOT NULL,
 released_at timestamptz NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,operation_ref)
);
CREATE INDEX cooperative_identity_reservation_key ON pf_accounts.cooperative_identity_reservation(tenant_id,operation_key);
CREATE INDEX cooperative_identity_reservation_org ON pf_accounts.cooperative_identity_reservation(tenant_id,cooperative_ref);
SELECT shared.apply_tenant_rls('pf_accounts.cooperative_identity_reservation');
REVOKE ALL ON pf_accounts.cooperative_identity_reservation FROM PUBLIC;
GRANT SELECT,INSERT,UPDATE ON pf_accounts.cooperative_identity_reservation TO r_accounts;
