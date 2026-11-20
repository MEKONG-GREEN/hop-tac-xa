-- AH-283: owner publication + writer guard; no application/business code in DB-only modules.
SET LOCAL lock_timeout = '5s';
CREATE SCHEMA IF NOT EXISTS pf_mechanization_pub;
REVOKE ALL ON SCHEMA pf_mechanization_pub FROM PUBLIC;
GRANT USAGE ON SCHEMA pf_mechanization_pub TO r_mechanization_pub;
SELECT shared.publish_cooperative_references('pf_mechanization','pf_mechanization_pub','r_mechanization_pub');
