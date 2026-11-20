-- AH-57: guidance calendars, independent of opened seasons/SOPs.
-- Recipient UUIDs are snapshots; no account names/phone numbers are stored.
SET LOCAL lock_timeout = '5s';
CREATE TABLE pf_coop.season_calendar_event (
 id uuid NOT NULL, tenant_id uuid NOT NULL, stream_key text NOT NULL, seq bigint NOT NULL,
 calendar_ref uuid NOT NULL, cooperative_ref uuid NOT NULL,
 action text NOT NULL CHECK (action IN ('published','edited','cancelled')),
 crop_code text NOT NULL CHECK(crop_code ~ '^[A-Za-z0-9_.-]{1,40}$'),
 season_code text NOT NULL CHECK(season_code ~ '^[A-Za-z0-9_.-]{1,40}$'),
 season_year integer NOT NULL CHECK(season_year BETWEEN 2000 AND 2100),
 sow_from date NOT NULL, sow_to date NOT NULL,
 recipients uuid[] NOT NULL,
 event_time timestamptz NOT NULL, recorded_at timestamptz NOT NULL DEFAULT now(),
 supersedes_id uuid NULL, correction_reason text NULL,
 actor_ref uuid NOT NULL, device_id text NULL, idem_key text NOT NULL, rule_version text NOT NULL,
 prev_hash bytea NOT NULL, row_hash bytea NOT NULL,
 PRIMARY KEY(id,recorded_at),
 CHECK(stream_key='season_calendar:'||calendar_ref::text),
 CHECK(sow_from BETWEEN DATE '0001-01-01' AND DATE '9999-12-31' AND sow_to BETWEEN sow_from AND DATE '9999-12-31'),
 CHECK(cardinality(recipients) BETWEEN 1 AND 500 AND array_ndims(recipients)=1
   AND array_position(recipients,NULL) IS NULL AND NOT ('00000000-0000-0000-0000-000000000000'::uuid=ANY(recipients))),
 CHECK((action='published')=(supersedes_id IS NULL)),
 CHECK((supersedes_id IS NULL)=(correction_reason IS NULL)),
 CHECK(correction_reason IS NULL OR char_length(correction_reason) BETWEEN 10 AND 500)
) PARTITION BY RANGE(recorded_at);
CREATE INDEX season_calendar_event_stream ON pf_coop.season_calendar_event(tenant_id,calendar_ref,seq);
CREATE INDEX season_calendar_event_idem ON pf_coop.season_calendar_event(tenant_id,idem_key);
SELECT shared.apply_ledger('pf_coop.season_calendar_event','pf_coop.season_calendar_event');

CREATE TABLE pf_coop.season_calendar_current (
 tenant_id uuid NOT NULL, calendar_ref uuid NOT NULL, cooperative_ref uuid NOT NULL,
 id uuid NOT NULL, version bigint NOT NULL CHECK(version>=1),
 status text NOT NULL CHECK(status IN ('published','cancelled')),
 crop_code text NOT NULL, season_code text NOT NULL, season_year integer NOT NULL,
 sow_from date NOT NULL, sow_to date NOT NULL, recipients uuid[] NOT NULL,
 actor_ref uuid NOT NULL, event_time timestamptz NOT NULL, recorded_at timestamptz NOT NULL,
 PRIMARY KEY(tenant_id,calendar_ref)
);
SELECT shared.apply_tenant_rls('pf_coop.season_calendar_current');
CREATE INDEX season_calendar_current_coop ON pf_coop.season_calendar_current(tenant_id,cooperative_ref,status,sow_from,calendar_ref);
CREATE INDEX season_calendar_current_recipients ON pf_coop.season_calendar_current USING gin(recipients);

-- Same node-less Admin/jobs convention as cooperative's other private tables.
-- A scoped caller needs a real projected node; empty k_ref never grants access.
-- Recipient/role authorization is additional application filtering, not inferred
-- from membership in the cooperative node alone.
DO $$ DECLARE t text; BEGIN
 FOREACH t IN ARRAY ARRAY['season_calendar_event','season_calendar_current'] LOOP
  EXECUTE format($p$CREATE POLICY p_scope ON pf_coop.%1$I AS RESTRICTIVE FOR ALL TO r_cooperative
   USING(k_ref.ctx_uuid('app.node_id') IS NULL OR
    (EXISTS(SELECT 1 FROM k_ref.node_closure n WHERE n.tenant_id=shared.current_tenant()
      AND n.ancestor=k_ref.ctx_uuid('app.node_id') AND n.descendant=k_ref.ctx_uuid('app.node_id') AND n.depth=0)
     AND EXISTS(SELECT 1 FROM k_ref.node_closure n WHERE n.tenant_id=%1$I.tenant_id
      AND n.ancestor=k_ref.ctx_uuid('app.node_id') AND n.descendant=%1$I.cooperative_ref)))
   WITH CHECK(k_ref.ctx_uuid('app.node_id') IS NULL OR
    (EXISTS(SELECT 1 FROM k_ref.node_closure n WHERE n.tenant_id=shared.current_tenant()
      AND n.ancestor=k_ref.ctx_uuid('app.node_id') AND n.descendant=k_ref.ctx_uuid('app.node_id') AND n.depth=0)
     AND EXISTS(SELECT 1 FROM k_ref.node_closure n WHERE n.tenant_id=%1$I.tenant_id
      AND n.ancestor=k_ref.ctx_uuid('app.node_id') AND n.descendant=%1$I.cooperative_ref)))$p$,t);
  EXECUTE format('ALTER TABLE pf_coop.%I OWNER TO own_cooperative',t);
  EXECUTE format('REVOKE ALL ON pf_coop.%I FROM PUBLIC',t);
 END LOOP;
END $$;
GRANT SELECT,INSERT ON pf_coop.season_calendar_event TO r_cooperative;
GRANT SELECT,INSERT,UPDATE,DELETE ON pf_coop.season_calendar_current TO r_cooperative;
