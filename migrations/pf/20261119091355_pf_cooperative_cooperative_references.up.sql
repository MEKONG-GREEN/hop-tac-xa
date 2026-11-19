-- AH-283: owner publication + writer guard; no application/business code in DB-only modules.
SET LOCAL lock_timeout = '5s';
CREATE SCHEMA IF NOT EXISTS pf_cooperative_pub;
REVOKE ALL ON SCHEMA pf_cooperative_pub FROM PUBLIC;
GRANT USAGE ON SCHEMA pf_cooperative_pub TO r_cooperative_pub;
SELECT shared.publish_cooperative_references('pf_coop','pf_cooperative_pub','r_cooperative_pub');
