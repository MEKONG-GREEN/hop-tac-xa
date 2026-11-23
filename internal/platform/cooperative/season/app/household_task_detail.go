package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/store"
	mdport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
)

const MaxTaskPhotos = 5
const MaxTaskPhotoPlainBytes = 1 << 20

var taskDosePattern = regexp.MustCompile(`^(?:0|[1-9][0-9]{0,8})(?:\.[0-9]{1,4})?$`)
var ErrTaskZeroConfirmation = problem.New(http.StatusConflict, "cropcycle.task_zero_confirmation_required", "Liều thực tế bằng 0; xác nhận trước khi lưu")

type ActualMaterialInput struct {
	MaterialCode string `json:"material_code,omitempty"`
	Name         string `json:"name,omitempty"`
	Unit         string `json:"unit,omitempty"`
	DosePerHa    string `json:"dose_per_ha"`
}

type DetailedTaskInput struct {
	CooperativeRef, HouseholdRef, SeasonRef, ExpectedEventID uuid.UUID
	StepSeq                                                  int
	State, DoneOn, Note, BackfillReason                      string
	Materials                                                []ActualMaterialInput
	FileRefs                                                 []uuid.UUID
	ZeroConfirmed                                            bool
	GPS                                                      domain.GPS
	Channel, IdemKey                                         string
	// Only the validated sync adapter supplies these. HTTP uses server time and
	// server-generated IDs; the received timestamp remains the DB recorded_at.
	RecordID  uuid.UUID
	EventTime time.Time
	DeviceID  *string
}

func normalizeDetailedTask(in DetailedTaskInput, now time.Time) (DetailedTaskInput, string, error) {
	bad := func(field, reason string) (DetailedTaskInput, string, error) {
		return in, "", problem.Validation.WithField(field, reason)
	}
	if in.IdemKey == "" {
		return in, "", problem.IdempotencyRequired
	}
	if in.StepSeq < 1 || in.StepSeq > domain.MaxPlanStepSeq || in.ExpectedEventID == uuid.Nil {
		return bad("step_seq", "Chọn việc và phiên bản vụ hợp lệ")
	}
	if in.State != "done" && in.State != "partial" {
		return bad("state", "Chọn Đã xong hoàn toàn hoặc Chưa xong, làm tiếp")
	}
	day, err := domain.ParseDay(in.DoneOn)
	if err != nil || day.After(businessDay(now)) {
		return bad("done_on", "Ngày làm xong không được sau hôm nay")
	}
	if in.Channel == "" {
		in.Channel = ChannelApp
	}
	if in.Channel != ChannelApp && in.Channel != ChannelWeb {
		return bad("channel", "app hoặc web")
	}
	if !in.EventTime.IsZero() && (in.EventTime.Year() < 1 || in.EventTime.Year() > 9999 || in.EventTime.After(now.Add(5*time.Minute))) {
		return bad("event_time", "Thời gian ghi không hợp lệ")
	}
	if in.RecordID != uuid.Nil && in.RecordID.Version() != 7 {
		return bad("record_id", "Mã bản ghi phải là UUIDv7")
	}
	if len(in.Materials) > domain.MaxInputs {
		return bad("materials", "Tối đa 20 vật tư")
	}
	if in.Materials == nil {
		in.Materials = []ActualMaterialInput{}
	}
	if in.FileRefs == nil {
		in.FileRefs = []uuid.UUID{}
	}
	if len(in.FileRefs) > MaxTaskPhotos {
		return bad("file_refs", "Tối đa 5 ảnh, mỗi ảnh không quá 1 MB")
	}
	seenFiles := map[uuid.UUID]bool{}
	for _, ref := range in.FileRefs {
		if ref == uuid.Nil || ref.Version() != 7 || seenFiles[ref] {
			return bad("file_refs", "Mã ảnh không hợp lệ hoặc bị trùng")
		}
		seenFiles[ref] = true
	}
	in.Note, err = domain.NormalizeNote("note", in.Note)
	if err != nil {
		return in, "", fieldProblem(err)
	}
	in.BackfillReason, err = domain.NormalizeNote("backfill_reason", in.BackfillReason)
	if err != nil {
		return in, "", fieldProblem(err)
	}
	if utf8.RuneCountInString(in.BackfillReason) > 500 {
		return bad("backfill_reason", "Lý do tối đa 500 ký tự")
	}
	for i := range in.Materials {
		m := &in.Materials[i]
		m.MaterialCode = strings.TrimSpace(m.MaterialCode)
		m.Name = strings.TrimSpace(m.Name)
		m.Unit = strings.TrimSpace(m.Unit)
		if domain.ContainsPII(m.Unit) {
			return bad("materials", "Đơn vị không được chứa SĐT hoặc CCCD")
		}
		if m.MaterialCode != "" {
			m.Name = ""
		} // the catalog owns names for referenced materials
		m.DosePerHa = strings.TrimSpace(m.DosePerHa)
		if !taskDosePattern.MatchString(m.DosePerHa) {
			return bad("materials", "Liều thực tế không âm, tối đa 4 chữ số thập phân")
		}
		dose, _ := decimal.NewFromString(m.DosePerHa)
		m.DosePerHa = dose.String()
		if dose.IsZero() && !in.ZeroConfirmed {
			return in, "", ErrTaskZeroConfirmation
		}
		if m.MaterialCode == "" {
			if _, err := domain.NormalizeInputs([]domain.Input{{Name: m.Name, Unit: m.Unit}}); err != nil {
				return in, "", fieldProblem(err)
			}
		}
	}
	if err := in.GPS.Normalize(); err != nil {
		return in, "", fieldProblem(err)
	}
	if in.GPS.Source != domain.GPSDevice && in.GPS.Source != domain.GPSPhotoEXIF && in.GPS.Source != domain.GPSDenied && in.GPS.Source != domain.GPSNoDevice {
		return bad("gps.source", "Lấy GPS thiết bị, ảnh vừa chụp hoặc nhãn thiếu GPS")
	}
	if in.GPS.HasPoint() {
		lon, lat := in.GPS.Lon.Round(7), in.GPS.Lat.Round(7)
		in.GPS.Lon, in.GPS.Lat = &lon, &lat
	}
	if in.GPS.AccuracyM != nil {
		if in.GPS.AccuracyM.GreaterThan(decimal.RequireFromString("999999.99")) {
			return bad("gps.accuracy_m", "Tối đa 999999.99")
		}
		accuracy := in.GPS.AccuracyM.Round(2)
		in.GPS.AccuracyM = &accuracy
	}
	// Inputs are already normalized, and notes/free-text names passed the same
	// PII guards as existing journals. The digest contains no identity fields.
	hashInput := in
	hashInput.GPS = domain.GPS{} // compare the stored GPS separately; keep coordinates out of the digest
	raw, _ := json.Marshal(hashInput)
	sum := sha256.Sum256(raw)
	return in, hex.EncodeToString(sum[:]), nil
}

func (s *Service) detailedMaterials(ctx context.Context, cur store.Season, step domain.Step, in []ActualMaterialInput) ([]domain.ActualMaterial, []domain.Input, error) {
	actual := make([]domain.ActualMaterial, 0, len(in))
	inputs := make([]domain.Input, 0, len(in))
	if len(in) == 0 {
		return actual, inputs, nil
	}
	if s.Catalogs == nil {
		return nil, nil, ErrDependency.WithDetail("Chưa nối danh mục vật tư")
	}
	catalog, err := s.Catalogs.List(ctx, mdport.CatalogMaterial)
	if err != nil {
		return nil, nil, ErrDependency.WithCause(err)
	}
	byCode := map[string]mdport.CatalogItem{}
	for _, it := range catalog {
		byCode[it.Code] = it
	}
	seen := map[string]bool{}
	for _, raw := range in {
		name, unit, group, kind := raw.Name, raw.Unit, "", ""
		if raw.MaterialCode != "" {
			it, ok := byCode[raw.MaterialCode]
			pinned := false
			for _, m := range step.Materials {
				if m.MaterialCode == raw.MaterialCode {
					pinned = true
				}
			}
			if !ok || (!it.Active && !pinned) {
				return nil, nil, ErrCatalogInvalid.WithField("materials", "Vật tư không có trong danh mục đang dùng")
			}
			name, unit, group, kind = it.Name, it.Attrs["unit"], it.Attrs["group"], it.Attrs["fertilizer_type"]
			if raw.Unit != "" && raw.Unit != unit {
				return nil, nil, problem.Validation.WithField("materials", "Đơn vị không khớp danh mục; tải lại vật tư")
			}
		} else {
			for _, it := range catalog {
				if strings.EqualFold(strings.Join(strings.Fields(it.Name), " "), strings.Join(strings.Fields(name), " ")) {
					return nil, nil, problem.Validation.WithField("materials", "Vật tư đã có trong danh mục; hãy chọn theo mã")
				}
			}
		}
		normalized, err := domain.NormalizeInputs([]domain.Input{{Name: name, Unit: unit}})
		if err != nil {
			return nil, nil, fieldProblem(err)
		}
		if domain.ContainsPII(unit) {
			return nil, nil, problem.Validation.WithField("materials", "Đơn vị không được chứa SĐT hoặc CCCD")
		}
		name, unit = normalized[0].Name, normalized[0].Unit
		key := raw.MaterialCode
		if key == "" {
			key = strings.ToLower(name) + ":" + unit
		}
		if seen[key] {
			return nil, nil, problem.Validation.WithField("materials", "Vật tư bị trùng trong cùng bản ghi")
		}
		seen[key] = true
		dose, _ := decimal.NewFromString(raw.DosePerHa)
		quantity := dose.Mul(cur.AreaHa)
		if quantity.GreaterThanOrEqual(decimal.New(1, 9)) {
			return nil, nil, problem.Validation.WithField("materials", "Tổng lượng vật tư phải nhỏ hơn 1000000000")
		}
		// Exact derived totals can have six decimal places (dose4 × area2). Do not
		// truncate them to the legacy manual-input precision or multiply area again.
		actual = append(actual, domain.ActualMaterial{MaterialCode: raw.MaterialCode, Name: name, Unit: unit, Group: group, FertilizerType: kind, DosePerHa: dose.String(), Quantity: quantity.String()})
		inputs = append(inputs, domain.Input{Name: name, Unit: unit, Qty: quantity})
	}
	return actual, inputs, nil
}

func (s *Service) detailedTaskState(ctx context.Context, tx pgx.Tx, cur store.Season, in DetailedTaskInput) (domain.Step, bool, error) {
	if cur.CurrentEventID != in.ExpectedEventID {
		return domain.Step{}, false, ErrTaskChanged.With("current_event_id", cur.CurrentEventID)
	}
	if cur.Status != "open" && cur.Status != "completed" {
		return domain.Step{}, false, ErrTaskState
	}
	checklist, err := s.householdChecklistTx(ctx, tx, cur, true)
	if err != nil {
		return domain.Step{}, false, err
	}
	var selected *domain.TaskProgress
	for i := range checklist.Progress.Stages {
		for j := range checklist.Progress.Stages[i].Tasks {
			t := &checklist.Progress.Stages[i].Tasks[j]
			if t.Seq == in.StepSeq {
				selected = t
			}
		}
	}
	if selected == nil {
		return domain.Step{}, false, problem.Validation.WithField("step_seq", "Việc không thuộc snapshot của vụ")
	}
	if selected.DoneOn != nil || selected.CompletionStatus == "skipped" {
		return domain.Step{}, false, ErrTaskDone
	}
	if !selected.CanComplete {
		return domain.Step{}, false, ErrTaskLocked
	}
	var content domain.SeasonSnapshot
	if err := json.Unmarshal(cur.SOPSnapshot, &content); err != nil {
		return domain.Step{}, false, err
	}
	step, ok := content.Step(in.StepSeq)
	if !ok {
		return domain.Step{}, false, ErrDependency
	}
	if len(step.Materials) > 0 && len(in.Materials) == 0 && !in.ZeroConfirmed {
		return domain.Step{}, false, ErrTaskZeroConfirmation
	}
	created := in.EventTime
	if created.IsZero() {
		created = s.now()
	}
	done, _ := domain.ParseDay(in.DoneOn)
	if done.After(businessDay(created)) {
		return domain.Step{}, false, problem.Validation.WithField("done_on", "Ngày làm xong không được sau ngày ghi")
	}
	backfilled := !selected.Undated && done.Before(businessDay(created).AddDate(0, 0, -3))
	if backfilled && in.BackfillReason == "" {
		return domain.Step{}, false, problem.Validation.WithField("backfill_reason", "Bắt buộc nhập lý do khi ghi bù quá 3 ngày")
	}
	return step, backfilled, nil
}

func (s *Service) RecordHouseholdTask(ctx context.Context, in DetailedTaskInput) (CompleteTaskResult, error) {
	if _, err := s.matrixAccess(ctx, fnLog, htxroles.Create); err != nil {
		return CompleteTaskResult{}, err
	}
	a, err := s.householdAccess(ctx, in.CooperativeRef, true)
	if err != nil {
		return CompleteTaskResult{}, err
	}
	if s.dependencyTxOpen(ctx) {
		return CompleteTaskResult{}, ErrDependency.WithDetail("Danh mục và ảnh phải được kiểm trước giao dịch nhật ký")
	}
	in, hash, err := normalizeDetailedTask(in, s.now())
	if err != nil {
		return CompleteTaskResult{}, err
	}
	key := "household-detail:" + a.p.ActorRef.String() + ":" + in.IdemKey
	var out CompleteTaskResult
	var preflight *store.Season
	var step domain.Step
	var backfilled bool
	replay := func(old *store.FarmLog) error {
		if old.ActorRef != a.p.ActorRef || old.SeasonRef != in.SeasonRef || old.CooperativeRef != in.CooperativeRef || old.HouseholdRef != in.HouseholdRef || old.EntryKind != "household" || old.TaskCompletion == nil || old.TaskCompletion.Details == nil || old.TaskCompletion.Details.RequestHash != hash || !sameHouseholdGPS(old.GPS, in.GPS) {
			return problem.IdempotencyConflict
		}
		out.Record, out.Replayed = *old, true
		return nil
	}
	err = s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		var e error
		preflight, e = householdTaskSeason(ctx, tx, a.p.TenantID, in.CooperativeRef, in.HouseholdRef, in.SeasonRef, false)
		if e != nil {
			return e
		}
		old, e := store.FarmLogByIdem(ctx, tx, key)
		if e != nil {
			return e
		}
		if old != nil {
			if e = replay(old); e != nil {
				return e
			}
			out.Checklist, e = s.householdChecklistTx(ctx, tx, *preflight, true)
			return e
		}
		step, backfilled, e = s.detailedTaskState(ctx, tx, *preflight, in)
		return e
	})
	if err != nil || out.Replayed {
		return out, err
	}
	actual, inputs, err := s.detailedMaterials(ctx, *preflight, step, in.Materials)
	if err != nil {
		return out, err
	}
	if len(in.FileRefs) > 0 {
		if s.TaskImages == nil {
			return out, ErrDependency.WithDetail("Chưa nối kiểm tra ảnh nhật ký")
		}
		if err = s.TaskImages.VerifyOwnedImages(ctx, in.HouseholdRef, in.FileRefs, MaxTaskPhotoPlainBytes); err != nil {
			return out, err
		}
	}
	err = s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('pf_coop.household_detail:'||shared.current_tenant()::text||':'||$1,99))`, key); err != nil {
			return err
		}
		cur, err := householdTaskSeason(ctx, tx, a.p.TenantID, in.CooperativeRef, in.HouseholdRef, in.SeasonRef, true)
		if err != nil {
			return err
		}
		old, err := store.FarmLogByIdem(ctx, tx, key)
		if err != nil {
			return err
		}
		if old != nil {
			if err := replay(old); err != nil {
				return err
			}
		} else {
			step, backfilled, err = s.detailedTaskState(ctx, tx, *cur, in)
			if err != nil {
				return err
			}
			id, at := in.RecordID, in.EventTime
			if id == uuid.Nil {
				id = ids.New()
			}
			if at.IsZero() {
				at = s.now()
			}
			meta := domain.TaskRecord{StepSeq: in.StepSeq, StageSeq: step.StageSeq, State: in.State, DoneOn: in.DoneOn, SOPVersionID: cur.SOPVersionID, SeasonEventID: cur.CurrentEventID, Step: step,
				Details: &domain.TaskDetails{ActualMaterials: actual, Backfilled: backfilled, BackfillReason: in.BackfillReason, ZeroConfirmed: in.ZeroConfirmed, RequestHash: hash}}
			location := "missing"
			if in.GPS.HasPoint() {
				location = "unverified"
			}
			l := store.FarmLog{ID: id, TenantID: a.p.TenantID, LogRef: id, SeasonRef: cur.SeasonRef, HouseholdRef: cur.HouseholdRef, CooperativeRef: cur.CooperativeRef, CommuneCode: cur.CommuneCode, ActivityCode: step.ActivityCode, SOPStepSeq: &in.StepSeq,
				GPS: in.GPS, LocationStatus: location, Inputs: inputs, Note: in.Note, FileRefs: in.FileRefs, BecameCurrent: true, OnBehalf: a.onBehalf(), Channel: in.Channel, EventTime: at, ActorRef: a.p.ActorRef, DeviceID: in.DeviceID, IdemKey: key, EntryKind: "household", TaskCompletion: &meta}
			if err := store.InsertFarmLog(ctx, tx, &l); err != nil {
				return err
			}
			if err := store.SaveHouseholdTask(ctx, tx, l); err != nil {
				return err
			}
			if err := store.RefreshYield(ctx, tx, l.SeasonRef); err != nil {
				return err
			}
			if in.State == "done" {
				if err := enqueue(ctx, tx, a.p.TenantID, port.EventHouseholdTaskCompleted, at, port.HouseholdTaskCompletedV1{LogID: l.ID, SeasonRef: l.SeasonRef, HouseholdRef: l.HouseholdRef, CooperativeRef: l.CooperativeRef, SOPVersionID: meta.SOPVersionID, StepSeq: meta.StepSeq, DoneOn: meta.DoneOn, GPSMissing: !l.GPS.HasPoint(), ActorRef: l.ActorRef, EventTime: at.Format(time.RFC3339)}); err != nil {
					return err
				}
			}
			out.Record = l
		}
		out.Checklist, err = s.householdChecklistTx(ctx, tx, *cur, true)
		return err
	})
	return out, err
}
