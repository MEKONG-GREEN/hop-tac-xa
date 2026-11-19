-- AH-283: owner publication + writer guard; no application/business code in DB-only modules.
SET LOCAL lock_timeout = '5s';
CREATE SCHEMA IF NOT EXISTS pf_sync_pub;
REVOKE ALL ON SCHEMA pf_sync_pub FROM PUBLIC;
GRANT USAGE ON SCHEMA pf_sync_pub TO r_sync_pub;
SELECT shared.publish_cooperative_references('pf_sync','pf_sync_pub','r_sync_pub');
