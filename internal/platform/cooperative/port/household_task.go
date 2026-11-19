package port

import "github.com/google/uuid"

const EventHouseholdTaskCompleted = "pf.cooperative.household_task.completed"

// HouseholdTaskCompletedV1 does not claim a review or a plot association.
//
//ag:event pf.cooperative.household_task.completed v1
type HouseholdTaskCompletedV1 struct {
	LogID          uuid.UUID `json:"log_id"`
	SeasonRef      uuid.UUID `json:"season_ref"`
	HouseholdRef   uuid.UUID `json:"household_ref"`
	CooperativeRef uuid.UUID `json:"cooperative_ref"`
	SOPVersionID   uuid.UUID `json:"sop_version_id"`
	StepSeq        int       `json:"step_seq"`
	DoneOn         string    `json:"done_on"`
	GPSMissing     bool      `json:"gps_missing"`
	ActorRef       uuid.UUID `json:"actor_ref"`
	EventTime      string    `json:"event_time"`
}
