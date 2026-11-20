package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"

	syncport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
	"github.com/google/uuid"
)

// HouseholdTaskPayload is a narrow native journal capability. Generic Member
// season/farm-log synchronization remains closed. Authorization is repeated by
// RecordHouseholdTask using the current principal, never a role in the payload.
type HouseholdTaskPayload struct {
	Channel         string                `json:"channel"`
	RecordedBy      uuid.UUID             `json:"recorded_by"`
	TenantID        uuid.UUID             `json:"tenant_id"`
	CooperativeRef  uuid.UUID             `json:"cooperative_ref"`
	HouseholdRef    uuid.UUID             `json:"household_ref"`
	SeasonRef       uuid.UUID             `json:"season_ref"`
	ExpectedEventID uuid.UUID             `json:"expected_season_event_id"`
	StepSeq         int                   `json:"step_seq"`
	State           string                `json:"state"`
	DoneOn          string                `json:"done_on"`
	Note            string                `json:"note"`
	BackfillReason  string                `json:"backfill_reason"`
	Materials       []ActualMaterialInput `json:"materials"`
	FileRefs        []uuid.UUID           `json:"file_refs"`
	ZeroConfirmed   bool                  `json:"zero_confirmed"`
	GPS             *GPSPayload           `json:"gps"`
}

func (snk SyncSink) householdTask(ctx context.Context, rec syncport.Record) syncport.Result {
	if rec.Op != syncport.OpCreate || rec.SupersedesID != nil || rec.CorrectionReason != nil || rec.ID.Version() != 7 || rec.DeviceID == uuid.Nil || rec.EventTime.IsZero() {
		return rejected(rec.ID, problem.Validation.WithField("op", "household_task_record cần create, UUIDv7, thời gian và thiết bị đã đăng ký"))
	}
	var p HouseholdTaskPayload
	dec := json.NewDecoder(bytes.NewReader(rec.Payload))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil || dec.Decode(&struct{}{}) != io.EOF {
		return rejected(rec.ID, problem.Validation.WithField("payload", "Dữ liệu ghi việc không hợp lệ"))
	}
	caller, ok := authz.FromContext(ctx)
	if !ok || p.RecordedBy != caller.ActorRef || p.TenantID != caller.TenantID {
		return rejected(rec.ID, problem.Forbidden.WithDetail("Bản ghi thuộc tài khoản khác"))
	}
	gps, err := p.GPS.Parse()
	if err != nil {
		return rejected(rec.ID, err)
	}
	device := rec.DeviceID.String()
	r, err := snk.Svc.RecordHouseholdTask(ctx, DetailedTaskInput{CooperativeRef: p.CooperativeRef, HouseholdRef: p.HouseholdRef, SeasonRef: p.SeasonRef, ExpectedEventID: p.ExpectedEventID, StepSeq: p.StepSeq, State: p.State, DoneOn: p.DoneOn, Note: p.Note, BackfillReason: p.BackfillReason, Materials: p.Materials, FileRefs: p.FileRefs, ZeroConfirmed: p.ZeroConfirmed, GPS: gps, Channel: p.Channel, IdemKey: rec.IdemKey, RecordID: rec.ID, EventTime: rec.EventTime, DeviceID: &device})
	if err != nil {
		return rejected(rec.ID, err)
	}
	status := syncport.StatusAccepted
	if r.Replayed {
		status = syncport.StatusDuplicate
	}
	return syncport.Result{ID: rec.ID, Status: status}
}
