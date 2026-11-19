-- AH-93: household checklist records share the append-only FarmLog ledger.
-- NULL fields preserve all legacy hashes through the ledger column epoch.
SET LOCAL lock_timeout = '5s';
ALTER TABLE pf_coop.farm_log ADD COLUMN entry_kind text NULL, ADD COLUMN task_completion jsonb NULL;
SELECT shared.register_ledger_columns('pf_coop.farm_log', ARRAY['entry_kind','task_completion']);
ALTER TABLE pf_coop.farm_log ALTER COLUMN plot_ref DROP NOT NULL;
DO $$ DECLARE c record; BEGIN
 FOR c IN SELECT conname FROM pg_constraint WHERE conrelid='pf_coop.farm_log'::regclass AND contype='c'
   AND (pg_get_constraintdef(oid) LIKE '%location_status%' OR pg_get_constraintdef(oid) LIKE '%activity_code%') LOOP
  EXECUTE format('ALTER TABLE pf_coop.farm_log DROP CONSTRAINT %I',c.conname);
 END LOOP;
END $$;
ALTER TABLE pf_coop.farm_log ADD CONSTRAINT farm_log_entry_shape CHECK ((
 (entry_kind IS NULL AND task_completion IS NULL AND plot_ref IS NOT NULL
   AND location_status IN ('match','mismatch','missing')
   AND activity_code IN ('lam_dat','gieo_sa','tuoi_nuoc','bon_phan','phun_thuoc','kiem_tra','thu_hoach'))
 OR
 (entry_kind='household' AND plot_ref IS NULL AND household_ref IS NOT NULL AND sop_step_seq>=1
   AND batch_ref IS NULL AND jsonb_typeof(task_completion)='object'
   AND task_completion->>'state' IN ('done','partial','skipped')
   AND (task_completion->>'step_seq')::integer=sop_step_seq
   AND (task_completion->>'stage_seq')::integer>=1
   AND (task_completion->>'sop_version_id')::uuid IS NOT NULL
   AND (task_completion->>'season_event_id')::uuid IS NOT NULL
   AND jsonb_typeof(task_completion->'step')='object'
   AND (task_completion->'step'->>'seq')::integer=sop_step_seq
   AND (task_completion->'step'->>'stage_seq')::integer=(task_completion->>'stage_seq')::integer
   AND task_completion->'step'->>'activity_code'=activity_code
   AND task_completion->>'done_on' ~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}$'
   AND (task_completion->>'done_on')::date <= (event_time AT TIME ZONE 'Asia/Ho_Chi_Minh')::date
   AND activity_code IN ('lam_dat','gieo_sa','tuoi_nuoc','bon_phan','phun_thuoc','kiem_tra','thu_hoach','cat_tia','khac')
   AND ((gps IS NULL AND location_status='missing') OR (gps IS NOT NULL AND location_status='unverified')))
) IS TRUE) NOT VALID;
ALTER TABLE pf_coop.farm_log VALIDATE CONSTRAINT farm_log_entry_shape;

-- Separate projection: a recorded household task is not a reviewed plot log.
CREATE TABLE pf_coop.household_task_current (
 tenant_id uuid NOT NULL, log_ref uuid NOT NULL, id uuid NOT NULL, version bigint NOT NULL CHECK(version>=1),
 season_ref uuid NOT NULL, household_ref uuid NOT NULL, cooperative_ref uuid NOT NULL,
 sop_step_seq integer NOT NULL CHECK(sop_step_seq>=1),
 state text NOT NULL CHECK(state IN ('done','partial','skipped')),
 done_on date NOT NULL, actor_ref uuid NOT NULL, event_time timestamptz NOT NULL, recorded_at timestamptz NOT NULL,
 PRIMARY KEY(tenant_id,log_ref)
);
SELECT shared.apply_tenant_rls('pf_coop.household_task_current');
CREATE POLICY p_scope ON pf_coop.household_task_current AS RESTRICTIVE FOR ALL TO r_cooperative
 USING(EXISTS(SELECT 1 FROM pf_coop.season_current s WHERE s.tenant_id=household_task_current.tenant_id
   AND s.season_ref=household_task_current.season_ref AND s.entry_kind='household'
   AND s.cooperative_ref=household_task_current.cooperative_ref AND s.household_ref=household_task_current.household_ref))
 WITH CHECK(EXISTS(SELECT 1 FROM pf_coop.season_current s WHERE s.tenant_id=household_task_current.tenant_id
   AND s.season_ref=household_task_current.season_ref AND s.entry_kind='household'
   AND s.cooperative_ref=household_task_current.cooperative_ref AND s.household_ref=household_task_current.household_ref));
ALTER TABLE pf_coop.household_task_current OWNER TO own_cooperative;
REVOKE ALL ON pf_coop.household_task_current FROM PUBLIC;
GRANT SELECT,INSERT,UPDATE,DELETE ON pf_coop.household_task_current TO r_cooperative;
CREATE INDEX household_task_current_season ON pf_coop.household_task_current(tenant_id,season_ref,sop_step_seq);
CREATE UNIQUE INDEX household_task_one_done ON pf_coop.household_task_current(tenant_id,season_ref,sop_step_seq) WHERE state='done';
