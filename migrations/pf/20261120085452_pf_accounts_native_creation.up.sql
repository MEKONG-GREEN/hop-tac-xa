-- FN-07: durable claims span IAM I/O without an open database transaction.
-- Request digests are keyed HMACs; no phone-derived lookup index or PII is stored.
CREATE TABLE pf_accounts.native_creation (
  tenant_id uuid NOT NULL,
  id uuid NOT NULL,
  operation_key bytea NOT NULL,
  request_digest bytea NOT NULL CHECK (octet_length(request_digest)=32),
  actor_ref uuid NOT NULL,
  user_ref uuid,
  state text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','complete','compensated')),
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id,id),
  UNIQUE (tenant_id,operation_key),
  CHECK ((state='complete')=(user_ref IS NOT NULL))
);
-- Serialize IAM provisioning across custodial tenants, without moving the phone
-- uniqueness index out of IAM (R-04). An ambiguous IAM outcome stays pending
-- until reconciled; it must never expire and allow a concurrent duplicate.
CREATE UNIQUE INDEX native_creation_inflight ON pf_accounts.native_creation((true)) WHERE state='pending';
SELECT shared.apply_tenant_rls('pf_accounts.native_creation');
REVOKE ALL ON pf_accounts.native_creation FROM PUBLIC;
GRANT SELECT,INSERT,UPDATE ON pf_accounts.native_creation TO r_accounts;
