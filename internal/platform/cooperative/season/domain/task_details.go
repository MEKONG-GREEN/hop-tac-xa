package domain

// ActualMaterial snapshots classification and unit at recording. Dose is per
// hectare; Quantity is already the total for the season area, never per hectare.
// Missing classification is preserved for statistics to report skipped rows.
type ActualMaterial struct {
	MaterialCode   string `json:"material_code,omitempty"`
	Name           string `json:"name"`
	Unit           string `json:"unit"`
	Group          string `json:"group,omitempty"`
	FertilizerType string `json:"fertilizer_type,omitempty"`
	DosePerHa      string `json:"dose_per_ha"`
	Quantity       string `json:"quantity"`
}

// Details distinguishes measured entries from quick ticks, whose Inputs copy
// SOP recommendations. Only actual entries may feed material statistics.
type TaskDetails struct {
	ActualMaterials []ActualMaterial `json:"actual_materials"`
	Backfilled      bool             `json:"backfilled"`
	BackfillReason  string           `json:"backfill_reason,omitempty"`
	ZeroConfirmed   bool             `json:"zero_confirmed"`
	RequestHash     string           `json:"request_hash"`
}
