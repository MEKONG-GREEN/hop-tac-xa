-- AH-283: owner publication + writer guard; no application/business code in DB-only modules.
SET LOCAL lock_timeout = '5s';
CREATE SCHEMA IF NOT EXISTS pf_hrm_pub;
REVOKE ALL ON SCHEMA pf_hrm_pub FROM PUBLIC;
GRANT USAGE ON SCHEMA pf_hrm_pub TO r_hrm_pub;
SELECT shared.publish_cooperative_references('pf_hrm','pf_hrm_pub','r_hrm_pub');
