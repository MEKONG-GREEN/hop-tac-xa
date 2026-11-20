CREATE TABLE vault.native_household_command (
 tenant_id uuid NOT NULL,command_ref uuid NOT NULL,household_ref uuid NOT NULL,
 digest bytea NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,command_ref)
);
SELECT shared.apply_tenant_rls('vault.native_household_command');
REVOKE ALL ON vault.native_household_command FROM PUBLIC;
GRANT SELECT,INSERT ON vault.native_household_command TO r_identity;
CREATE TRIGGER trg_native_household_command_immutable BEFORE UPDATE OR DELETE ON vault.native_household_command
 FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_native_household_command_no_truncate BEFORE TRUNCATE ON vault.native_household_command
 FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable();
CREATE TABLE vault.native_household_head (
 tenant_id uuid NOT NULL,household_ref uuid NOT NULL,revision bigint NOT NULL CHECK(revision>0),
 PRIMARY KEY(tenant_id,household_ref)
);
SELECT shared.apply_tenant_rls('vault.native_household_head');
REVOKE ALL ON vault.native_household_head FROM PUBLIC;
GRANT SELECT,INSERT,UPDATE ON vault.native_household_head TO r_identity;
