package store

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func SaveHouseholdTask(ctx context.Context, tx pgx.Tx, l FarmLog) error {
	if !l.BecameCurrent {
		return nil
	}
	r := l.TaskCompletion
	_, err := tx.Exec(ctx, `INSERT INTO pf_coop.household_task_current
 (tenant_id,log_ref,id,version,season_ref,household_ref,cooperative_ref,sop_step_seq,state,done_on,actor_ref,event_time,recorded_at)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::date,$11,$12,$13)
 ON CONFLICT(tenant_id,log_ref) DO UPDATE SET id=EXCLUDED.id,version=EXCLUDED.version,state=EXCLUDED.state,done_on=EXCLUDED.done_on,
 actor_ref=EXCLUDED.actor_ref,event_time=EXCLUDED.event_time,recorded_at=EXCLUDED.recorded_at
 WHERE EXCLUDED.version>household_task_current.version`,
		l.TenantID, l.LogRef, l.ID, l.Version, l.SeasonRef, l.HouseholdRef, l.CooperativeRef, l.SOPStepSeq, r.State, r.DoneOn, l.ActorRef, l.EventTime, l.RecordedAt)
	return err
}

func HouseholdTasks(ctx context.Context, tx pgx.Tx, season uuid.UUID) ([]FarmLog, error) {
	rows, err := tx.Query(ctx, "SELECT "+farmLogCols+` FROM pf_coop.farm_log WHERE entry_kind='household' AND season_ref=$1 AND id IN
 (SELECT id FROM pf_coop.household_task_current WHERE season_ref=$1) ORDER BY recorded_at,id`, season)
	if err != nil {
		return nil, err
	}
	return collectFarmLogs(rows)
}
