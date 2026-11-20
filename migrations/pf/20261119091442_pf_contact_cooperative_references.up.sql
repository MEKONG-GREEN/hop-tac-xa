-- AH-283: owner publication + writer guard; no application/business code in DB-only modules.
SET LOCAL lock_timeout = '5s';
CREATE SCHEMA IF NOT EXISTS pf_contact_pub;
REVOKE ALL ON SCHEMA pf_contact_pub FROM PUBLIC;
GRANT USAGE ON SCHEMA pf_contact_pub TO r_contact_pub;
SELECT shared.publish_cooperative_references('pf_contact','pf_contact_pub','r_contact_pub');
