-- AH-283: owner publication + writer guard; no application/business code in DB-only modules.
SET LOCAL lock_timeout = '5s';
CREATE SCHEMA IF NOT EXISTS pf_cropcycle_pub;
REVOKE ALL ON SCHEMA pf_cropcycle_pub FROM PUBLIC;
GRANT USAGE ON SCHEMA pf_cropcycle_pub TO r_cropcycle_pub;
SELECT shared.publish_cooperative_references('pf_cropcycle','pf_cropcycle_pub','r_cropcycle_pub');
