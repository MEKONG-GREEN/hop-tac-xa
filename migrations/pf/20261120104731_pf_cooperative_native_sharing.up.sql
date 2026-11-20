-- FN-09: same-tenant, directional, cooperative-controlled sharing. This is
-- independent from the cross-tenant partner grants owned by pf/party.
SET LOCAL lock_timeout = '5s';
CREATE TABLE pf_coop.native_sharing (
 tenant_id uuid NOT NULL, ref uuid NOT NULL,
 requester_cooperative_ref uuid NOT NULL, owner_cooperative_ref uuid NOT NULL,
 state text NOT NULL CHECK(state IN ('requested','approved','rejected','revoked')),
 revision bigint NOT NULL CHECK(revision>=1), requested_at timestamptz NOT NULL,
 processed_at timestamptz, actor_ref uuid NOT NULL,
 PRIMARY KEY(tenant_id,ref), CHECK(requester_cooperative_ref<>owner_cooperative_ref),
 CHECK((state='requested')=(processed_at IS NULL))
);
CREATE UNIQUE INDEX native_sharing_active_pair ON pf_coop.native_sharing
 (tenant_id,requester_cooperative_ref,owner_cooperative_ref) WHERE state IN ('requested','approved');
CREATE INDEX native_sharing_incoming ON pf_coop.native_sharing(tenant_id,owner_cooperative_ref,state,ref);

CREATE TABLE pf_coop.native_sharing_event (
 id uuid NOT NULL,tenant_id uuid NOT NULL,stream_key text NOT NULL,seq bigint NOT NULL,
 request_ref uuid NOT NULL,requester_cooperative_ref uuid NOT NULL,owner_cooperative_ref uuid NOT NULL,
 action text NOT NULL CHECK(action IN ('request','approve','reject','revoke')),
 state text NOT NULL CHECK(state IN ('requested','approved','rejected','revoked')),
 event_time timestamptz NOT NULL,recorded_at timestamptz NOT NULL DEFAULT now(),
 supersedes_id uuid,correction_reason text,actor_ref uuid NOT NULL,device_id text,
 idem_key text NOT NULL,rule_version text NOT NULL,prev_hash bytea NOT NULL,row_hash bytea NOT NULL,
 PRIMARY KEY(id,recorded_at), CHECK(stream_key='native_sharing:'||request_ref::text)
) PARTITION BY RANGE(recorded_at);
SELECT shared.apply_ledger('pf_coop.native_sharing_event','pf_coop.native_sharing_event');

-- Durable, actor-bound replay receipts. Only references and workflow metadata;
-- neither cooperative labels nor any account/household identity is copied.
CREATE TABLE pf_coop.native_sharing_operation (
 tenant_id uuid NOT NULL,actor_ref uuid NOT NULL,idem_key text NOT NULL,
 requester_cooperative_ref uuid NOT NULL,owner_cooperative_ref uuid NOT NULL,
 action text NOT NULL,request_ref uuid NOT NULL,expected_revision bigint NOT NULL,
 response jsonb NOT NULL, PRIMARY KEY(tenant_id,actor_ref,idem_key)
);
SELECT shared.apply_tenant_rls('pf_coop.native_sharing');
SELECT shared.apply_tenant_rls('pf_coop.native_sharing_operation');
DO $$ DECLARE t text; BEGIN
 FOREACH t IN ARRAY ARRAY['native_sharing','native_sharing_event','native_sharing_operation'] LOOP
  EXECUTE format($p$CREATE POLICY p_scope ON pf_coop.%1$I AS RESTRICTIVE FOR ALL TO r_cooperative
   USING(k_ref.ctx_uuid('app.node_id') IS NULL OR
    (k_ref.ctx_uuid('app.node_id') IN (requester_cooperative_ref,owner_cooperative_ref)
     AND EXISTS(SELECT 1 FROM k_ref.node_closure n WHERE n.tenant_id=shared.current_tenant()
       AND n.ancestor=k_ref.ctx_uuid('app.node_id') AND n.descendant=n.ancestor AND n.depth=0)))
   WITH CHECK(k_ref.ctx_uuid('app.node_id') IS NULL OR
    (k_ref.ctx_uuid('app.node_id') IN (requester_cooperative_ref,owner_cooperative_ref)
     AND EXISTS(SELECT 1 FROM k_ref.node_closure n WHERE n.tenant_id=shared.current_tenant()
       AND n.ancestor=k_ref.ctx_uuid('app.node_id') AND n.descendant=n.ancestor AND n.depth=0)))$p$,t);
  EXECUTE format('ALTER TABLE pf_coop.%I OWNER TO own_cooperative',t);
  EXECUTE format('REVOKE ALL ON pf_coop.%I FROM PUBLIC',t);
 END LOOP;
END $$;
GRANT SELECT,INSERT,UPDATE ON pf_coop.native_sharing TO r_cooperative;
GRANT SELECT,INSERT ON pf_coop.native_sharing_event,pf_coop.native_sharing_operation TO r_cooperative;
SELECT shared.publish_cooperative_references('pf_coop','pf_cooperative_pub','r_cooperative_pub');

-- The application opts in only on native Representative read paths, retaining
-- the signed caller node. PostgreSQL READ ONLY is an additional write barrier:
-- broadening USING alone on an ALL policy must never enable DELETE/UPDATE.
CREATE FUNCTION pf_coop.native_shared_read_allowed(source uuid) RETURNS boolean
LANGUAGE sql STABLE SECURITY INVOKER SET search_path=pg_catalog,pg_temp AS $$
 SELECT current_setting('transaction_read_only')='on'
 AND current_setting('app.native_shared_read',true)='on'
 AND EXISTS(SELECT 1 FROM pf_coop.native_sharing g
  JOIN k_ref.org a ON a.tenant_id=g.tenant_id AND a.ref=g.requester_cooperative_ref AND a.active
  JOIN k_ref.org b ON b.tenant_id=g.tenant_id AND b.ref=g.owner_cooperative_ref AND b.active
  WHERE g.tenant_id=shared.current_tenant() AND g.requester_cooperative_ref=k_ref.ctx_uuid('app.node_id')
   AND g.owner_cooperative_ref=source AND g.state='approved')
$$;
REVOKE ALL ON FUNCTION pf_coop.native_shared_read_allowed(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION pf_coop.native_shared_read_allowed(uuid) TO r_cooperative,own_cooperative;
DO $$ DECLARE t text;col text;previous text;branch text; BEGIN
 FOR t,col IN SELECT * FROM (VALUES ('cooperative','org_ref'),('household_membership','org_ref'),
  ('plot','cooperative_ref'),('plot_boundary','cooperative_ref'),('season_current','cooperative_ref'),
  ('farm_log_current','cooperative_ref'),('harvest_declaration','cooperative_ref'),
  ('yield_summary','cooperative_ref'),('cooperative_area','cooperative_ref')) x(t,c) LOOP
  SELECT pg_get_expr(polqual,polrelid) INTO STRICT previous FROM pg_policy
   WHERE polrelid=format('pf_coop.%I',t)::regclass AND polname='p_scope' AND NOT polpermissive;
  branch:=format('(%I.tenant_id=shared.current_tenant() AND pf_coop.native_shared_read_allowed(%I.%I))',t,t,col);
  -- WITH CHECK remains byte-for-byte the existing own-node write rule.
  EXECUTE format('ALTER POLICY p_scope ON pf_coop.%I USING ((%s) OR %s)',t,previous,branch);
 END LOOP;
 SELECT pg_get_expr(polqual,polrelid) INTO STRICT previous FROM pg_policy
  WHERE polrelid='pf_coop.household'::regclass AND polname='p_scope' AND NOT polpermissive;
 EXECUTE format($p$ALTER POLICY p_scope ON pf_coop.household USING ((%s) OR
  (household.tenant_id=shared.current_tenant() AND EXISTS(SELECT 1 FROM pf_coop.household_membership m
   WHERE m.tenant_id=household.tenant_id AND m.household_ref=household.household_ref AND m.until_date IS NULL
    AND pf_coop.native_shared_read_allowed(m.org_ref))))$p$,previous);
END $$;
