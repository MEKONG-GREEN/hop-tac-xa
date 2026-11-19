-- AH-283: owner publication + writer guard; no application/business code in DB-only modules.
SET LOCAL lock_timeout = '5s';
CREATE SCHEMA IF NOT EXISTS pf_notify_pub;
REVOKE ALL ON SCHEMA pf_notify_pub FROM PUBLIC;
GRANT USAGE ON SCHEMA pf_notify_pub TO r_notify_pub;
SELECT shared.publish_cooperative_references('pf_notify','pf_notify_pub','r_notify_pub');
