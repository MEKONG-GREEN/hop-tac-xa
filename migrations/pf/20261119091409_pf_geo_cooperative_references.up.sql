-- AH-283: owner publication + writer guard; no application/business code in DB-only modules.
SET LOCAL lock_timeout = '5s';
CREATE SCHEMA IF NOT EXISTS pf_geo_pub;
REVOKE ALL ON SCHEMA pf_geo_pub FROM PUBLIC;
GRANT USAGE ON SCHEMA pf_geo_pub TO r_geo_pub;
SELECT shared.publish_cooperative_references('pf_geo','pf_geo_pub','r_geo_pub');
