-- AH-283: owner publication + writer guard; no application/business code in DB-only modules.
SET LOCAL lock_timeout = '5s';
CREATE SCHEMA IF NOT EXISTS pf_extension_pub;
REVOKE ALL ON SCHEMA pf_extension_pub FROM PUBLIC;
GRANT USAGE ON SCHEMA pf_extension_pub TO r_extension_pub;
SELECT shared.publish_cooperative_references('pf_extension','pf_extension_pub','r_extension_pub');
