-- FN-11 coefficients are independent of collection and season-detail forecasts.
CREATE TABLE pf_coop.straw_factor_event (
 id uuid NOT NULL, tenant_id uuid NOT NULL, stream_key text NOT NULL, seq bigint NOT NULL,
 variety_code text NOT NULL CHECK(variety_code ~ '^[A-Za-z0-9_.-]{1,40}$'),
 percentage numeric NOT NULL CHECK(percentage BETWEEN 0 AND 100 AND scale(percentage)<=4),
 previous_percentage numeric NULL,
 effective_at timestamptz NOT NULL,
 event_time timestamptz NOT NULL, recorded_at timestamptz NOT NULL DEFAULT now(),
 actor_ref uuid NOT NULL, device_id text NULL, idem_key text NOT NULL, rule_version text NOT NULL,
 supersedes_id uuid NULL, correction_reason text NULL,
 prev_hash bytea NOT NULL, row_hash bytea NOT NULL,
 PRIMARY KEY(id,recorded_at),
 CHECK(stream_key='straw_factor:'||variety_code),
 CHECK(previous_percentage IS NULL OR previous_percentage BETWEEN 0 AND 100)
) PARTITION BY RANGE(recorded_at);
CREATE INDEX straw_factor_event_lookup ON pf_coop.straw_factor_event(tenant_id,variety_code,effective_at DESC,seq DESC);
CREATE INDEX straw_factor_event_idem ON pf_coop.straw_factor_event(tenant_id,idem_key);
SELECT shared.apply_ledger('pf_coop.straw_factor_event','pf_coop.straw_factor_event');
ALTER TABLE pf_coop.straw_factor_event OWNER TO own_cooperative;
REVOKE ALL ON pf_coop.straw_factor_event FROM PUBLIC;
GRANT SELECT,INSERT ON pf_coop.straw_factor_event TO r_cooperative;
