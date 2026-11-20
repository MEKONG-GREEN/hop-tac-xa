-- AH-283: owner publication + writer guard; no application/business code in DB-only modules.
SET LOCAL lock_timeout = '5s';
CREATE SCHEMA IF NOT EXISTS pf_accounts_pub;
REVOKE ALL ON SCHEMA pf_accounts_pub FROM PUBLIC;
GRANT USAGE ON SCHEMA pf_accounts_pub TO r_accounts_pub;
SELECT shared.publish_cooperative_references('pf_accounts','pf_accounts_pub','r_accounts_pub');
