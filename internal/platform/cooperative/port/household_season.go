package port

import "github.com/google/uuid"

const EventHouseholdSeasonOpened = "pf.cooperative.household_season.opened"

// HouseholdSeasonOpenedV1 never invents a plot for a household allocation.
//
//ag:event pf.cooperative.household_season.opened v1
type HouseholdSeasonOpenedV1 struct {
	SeasonRef      uuid.UUID `json:"season_ref"`
	EventID        uuid.UUID `json:"event_id"`
	HouseholdRef   uuid.UUID `json:"household_ref"`
	CooperativeRef uuid.UUID `json:"cooperative_ref"`
	CropCode       string    `json:"crop_code"`
	SeasonCode     string    `json:"season_code"`
	SeasonYear     int       `json:"season_year"`
	VarietyCode    string    `json:"variety_code"`
	SOPVersionID   uuid.UUID `json:"sop_version_id"`
	AreaHa         string    `json:"area_ha"`
	AnchorDate     string    `json:"anchor_date"`
	GPSMissing     bool      `json:"gps_missing"`
	ActorRef       uuid.UUID `json:"actor_ref"`
	EventTime      string    `json:"event_time"`
}

const EventHouseholdSeasonClosed = "pf.cooperative.household_season.closed"

// HouseholdSeasonClosedV1: vụ theo hộ "Đã hoàn thành vụ" (AH-152). Không mang số liệu tổng kết (đọc qua API).
//
//ag:event pf.cooperative.household_season.closed v1
type HouseholdSeasonClosedV1 struct {
	SeasonRef      uuid.UUID `json:"season_ref"`
	EventID        uuid.UUID `json:"event_id"`
	HouseholdRef   uuid.UUID `json:"household_ref"`
	CooperativeRef uuid.UUID `json:"cooperative_ref"`
	HarvestID      uuid.UUID `json:"harvest_id"`
	ClosedOn       string    `json:"closed_on"`
	ActorRef       uuid.UUID `json:"actor_ref"`
	EventTime      string    `json:"event_time"`
}
