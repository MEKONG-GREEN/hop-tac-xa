-- AH-283: owner publication + writer guard; no application/business code in DB-only modules.
SET LOCAL lock_timeout = '5s';
CREATE SCHEMA IF NOT EXISTS pf_party_pub;
REVOKE ALL ON SCHEMA pf_party_pub FROM PUBLIC;
GRANT USAGE ON SCHEMA pf_party_pub TO r_party_pub;
SELECT shared.publish_cooperative_references('pf_party','pf_party_pub','r_party_pub');
