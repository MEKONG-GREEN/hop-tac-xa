package app

// AH-190 (FN-06-EX-04, EX-09, BR-03; KH-15): khai báo sản lượng của vụ theo hộ ghi lúc mất mạng, đẩy lên qua hàng đợi
// đồng bộ của AH-114. Máy chủ kiểm lại đầy đủ như khai báo online bằng quyền của người gọi hiện tại (không tin vai hay
// phạm vi trong payload); bản khai ngoại tuyến không kiểm phiên bản vụ, chỉ kiểm vụ còn "Đang canh tác" — vụ đã khai
// báo, dừng, hoàn thành trong lúc offline bị từ chối kèm lý do. Khóa của bản ghi trên máy là Idempotency-Key.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/domain"
	syncport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
)

// EntityHouseholdHarvest là loại bản ghi đồng bộ chiều lên của khai báo sản lượng vụ theo hộ.
const EntityHouseholdHarvest = "household_harvest"

// HouseholdHarvestPayload là nội dung bản khai ngoại tuyến.
type HouseholdHarvestPayload struct {
	RecordedBy     uuid.UUID   `json:"recorded_by"`
	TenantID       uuid.UUID   `json:"tenant_id"`
	CooperativeRef uuid.UUID   `json:"cooperative_ref"`
	HouseholdRef   uuid.UUID   `json:"household_ref"`
	SeasonRef      uuid.UUID   `json:"season_ref"`
	MassT          string      `json:"mass_t"`
	HarvestDate    string      `json:"harvest_date"`
	ZeroConfirmed  bool        `json:"zero_confirmed"`
	GPS            *GPSPayload `json:"gps"`
}

func (snk SyncSink) householdHarvest(ctx context.Context, rec syncport.Record) syncport.Result {
	if rec.Op != syncport.OpCreate || rec.SupersedesID != nil || rec.CorrectionReason != nil || rec.ID.Version() != 7 || rec.DeviceID == uuid.Nil || rec.EventTime.IsZero() {
		return rejected(rec.ID, problem.Validation.WithField("op", "household_harvest cần create, UUIDv7, thời gian và thiết bị đã đăng ký"))
	}
	var p HouseholdHarvestPayload
	dec := json.NewDecoder(bytes.NewReader(rec.Payload))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil || dec.Decode(&struct{}{}) != io.EOF {
		return rejected(rec.ID, problem.Validation.WithField("payload", "Dữ liệu khai báo sản lượng không hợp lệ"))
	}
	caller, ok := authz.FromContext(ctx)
	if !ok || p.RecordedBy != caller.ActorRef || p.TenantID != caller.TenantID {
		return rejected(rec.ID, problem.Forbidden.WithDetail("Bản khai thuộc tài khoản khác"))
	}
	gps, err := p.GPS.Parse()
	if err != nil {
		return rejected(rec.ID, err)
	}
	if gps.Source == "" {
		gps.Source = "no_device"
	}
	day, err := domain.ParseDay(p.HarvestDate)
	if err != nil {
		return rejected(rec.ID, problem.Validation.WithField("harvest_date", "Ngày thu hoạch cuối dạng YYYY-MM-DD"))
	}
	device := rec.DeviceID.String()
	r, err := snk.Svc.DeclareOfflineHouseholdHarvest(ctx, HouseholdHarvestInput{CooperativeRef: p.CooperativeRef, HouseholdRef: p.HouseholdRef,
		SeasonRef: p.SeasonRef, MassT: p.MassT, HarvestDate: day, ZeroConfirmed: p.ZeroConfirmed, GPS: gps, Channel: ChannelSync, Source: ChannelSync,
		IdemKey: rec.IdemKey, AnyEvent: true, EventTime: rec.EventTime, DeviceID: &device, RecordID: rec.ID})
	if err != nil {
		if pr := problem.From(err); pr.Code == ErrSeasonState.Code {
			return rejected(rec.ID, ErrSeasonState.WithDetail("Vụ không còn \"Đang canh tác\" (đã khai báo, dừng hoặc hoàn thành trong lúc ngoại tuyến); bản khai không được ghi"))
		}
		return rejected(rec.ID, err)
	}
	status := syncport.StatusAccepted
	if r.Replayed {
		status = syncport.StatusDuplicate
	}
	return syncport.Result{ID: rec.ID, Status: status}
}
