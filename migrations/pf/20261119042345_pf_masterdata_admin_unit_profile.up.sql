-- AH-271: operational catalog metadata is separate from immutable legal
-- boundaries. Disabling a picker option never closes a geographic version or
-- removes the scope of organizations that already reference it.
SET LOCAL lock_timeout = '5s';
CREATE TABLE pf_masterdata.admin_unit_profile_event (
 id uuid NOT NULL, tenant_id uuid NOT NULL, stream_key text NOT NULL, seq bigint NOT NULL,
 code text NOT NULL, unit_valid_from date NOT NULL,
 action text NOT NULL CHECK(action IN ('created','updated')),
 name text NOT NULL CHECK(char_length(name) BETWEEN 1 AND 200), active boolean NOT NULL,
 event_time timestamptz NOT NULL, recorded_at timestamptz NOT NULL DEFAULT now(),
 supersedes_id uuid NULL, correction_reason text NULL,
 actor_ref uuid NOT NULL, device_id text NULL, idem_key text NOT NULL, rule_version text NOT NULL,
 prev_hash bytea NOT NULL, row_hash bytea NOT NULL,
 PRIMARY KEY(id,recorded_at),
 CHECK(stream_key='admin_unit_profile:'||code||':'||to_char(unit_valid_from,'YYYY-MM-DD')),
 CHECK((supersedes_id IS NULL)=(correction_reason IS NULL)),
 CHECK(correction_reason IS NULL OR char_length(correction_reason) BETWEEN 10 AND 500),
 FOREIGN KEY(tenant_id,code,unit_valid_from) REFERENCES pf_masterdata.admin_unit(tenant_id,code,valid_from)
) PARTITION BY RANGE(recorded_at);
CREATE INDEX admin_unit_profile_stream ON pf_masterdata.admin_unit_profile_event(tenant_id,stream_key,seq);
CREATE INDEX admin_unit_profile_idem ON pf_masterdata.admin_unit_profile_event(tenant_id,idem_key);
CREATE INDEX admin_unit_profile_at ON pf_masterdata.admin_unit_profile_event(tenant_id,code,unit_valid_from,event_time DESC,seq DESC);
SELECT shared.apply_ledger('pf_masterdata.admin_unit_profile_event','pf_masterdata.admin_unit_profile_event');

CREATE TABLE pf_masterdata.admin_unit_profile_current (
 tenant_id uuid NOT NULL, code text NOT NULL, unit_valid_from date NOT NULL,
 id uuid NOT NULL, revision bigint NOT NULL CHECK(revision>0),
 name text NOT NULL CHECK(char_length(name) BETWEEN 1 AND 200), active boolean NOT NULL,
 actor_ref uuid NOT NULL, event_time timestamptz NOT NULL,
 PRIMARY KEY(tenant_id,code,unit_valid_from),
 FOREIGN KEY(tenant_id,code,unit_valid_from) REFERENCES pf_masterdata.admin_unit(tenant_id,code,valid_from)
);
SELECT shared.apply_tenant_rls('pf_masterdata.admin_unit_profile_current');
REVOKE ALL ON pf_masterdata.admin_unit_profile_event,pf_masterdata.admin_unit_profile_current FROM PUBLIC;
GRANT SELECT,INSERT ON pf_masterdata.admin_unit_profile_event TO r_masterdata;
GRANT SELECT,INSERT,UPDATE,DELETE ON pf_masterdata.admin_unit_profile_current TO r_masterdata;
