package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	memberstore "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/member/store"
	port "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/store"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
)

var (
	ErrSeasonHousehold   = problem.New(http.StatusNotFound, "cropcycle.household_not_found", "Không tìm thấy nông hộ trong HTX")
	ErrHouseholdInactive = problem.New(http.StatusUnprocessableEntity, "cropcycle.household_inactive", "Nông hộ đã ngừng tham gia HTX")
	ErrHouseholdLand     = problem.New(http.StatusUnprocessableEntity, "cropcycle.household_land_unknown", "Chưa có diện tích đất của nông hộ")
	ErrHouseholdArea     = problem.New(http.StatusConflict, "cropcycle.household_area_exceeded", "Diện tích vượt diện tích còn lại của nông hộ")
	ErrHouseholdSOP      = problem.New(http.StatusUnprocessableEntity, "cropcycle.no_active_sop_for_crop", "Chưa có SOP đang áp dụng cho loại cây; vui lòng liên hệ Admin")
)

// HouseholdOpenInput is online-only. The server owns identifiers and SOP data.
type HouseholdOpenInput struct {
	CooperativeRef, HouseholdRef              uuid.UUID
	CropCode, SeasonCode, VarietyCode, AreaHa string
	SeasonYear                                int
	AnchorDate                                time.Time
	SOPVersionID                              uuid.UUID
	GPS                                       domain.GPS
	Channel, IdemKey                          string
	// PlanChanges: tùy chỉnh quy trình trước khi lưu vụ (AH-113 AC1); bản chụp lưu đúng tùy chỉnh, SOP chuẩn không đổi.
	PlanChanges []domain.PlanChange
	// CopyFrom: AH-139 — sao chép giống và quy trình (kèm tùy chỉnh) từ một vụ trước của chính hộ này, kể cả vụ đang
	// canh tác. Loại cây, giống, SOP lấy theo vụ nguồn; người dùng chỉ nhập Vụ/Năm, mốc gốc, diện tích.
	CopyFrom uuid.UUID
	// SelfMade: AH-162 — quy trình tự tạo (giai đoạn, việc cùng cấu trúc SOP) khi loại cây/giống chưa có SOP đang áp
	// dụng; chỉ gắn vụ này (KH-12). SelfMadeAnchor là tên mốc gốc (mặc định "Ngày xuống giống").
	SelfMade       *domain.Content
	SelfMadeAnchor string
}

// SelfMadePlanName là tên quy trình tự tạo trong bản chụp của vụ (AH-162).
const SelfMadePlanName = "Quy trình tự tạo"

// ErrSOPAvailable: loại cây/giống đã có SOP đang áp dụng — chọn SOP thay vì tự tạo (AH-162).
var ErrSOPAvailable = problem.New(http.StatusConflict, "cropcycle.sop_available", "Loại cây đã có SOP đang áp dụng; chọn SOP thay vì tự tạo quy trình")

// selfMadeSnapshot cho biết bản chụp là quy trình tự tạo (không có phiên bản SOP master).
func selfMadeSnapshot(raw json.RawMessage) bool {
	var m struct {
		SelfMade bool `json:"self_made"`
	}
	return json.Unmarshal(raw, &m) == nil && m.SelfMade
}

// normalizeSelfMade kiểm quy trình tự tạo bằng đúng luật soạn SOP (thứ tự liên tục, Từ ≤ Đến...); lỗi theo trường
// mang tiền tố "self_made_plan.".
func normalizeSelfMade(in *HouseholdOpenInput) error {
	if in.SelfMade == nil {
		return nil
	}
	if in.CopyFrom != uuid.Nil || in.SOPVersionID != uuid.Nil {
		return problem.Validation.WithField("self_made_plan", "Chọn SOP, sao chép vụ trước hoặc tự tạo quy trình — chỉ một cách")
	}
	if len(in.SelfMade.Stages) == 0 || len(in.SelfMade.Steps) == 0 {
		return problem.Validation.WithField("self_made_plan", "Nhập ít nhất một giai đoạn và một việc")
	}
	c, errs := domain.CheckContent(*in.SelfMade)
	if len(errs) > 0 {
		out := make(domain.FieldErrors, len(errs))
		for i, fe := range errs {
			out[i] = domain.FieldError{Field: "self_made_plan." + fe.Field, Reason: fe.Reason}
		}
		return fieldProblem(out)
	}
	in.SelfMade = &c
	in.SelfMadeAnchor = strings.TrimSpace(in.SelfMadeAnchor)
	if in.SelfMadeAnchor == "" {
		in.SelfMadeAnchor = "Ngày xuống giống"
	}
	if len([]rune(in.SelfMadeAnchor)) > 100 {
		return problem.Validation.WithField("self_made_anchor", "Tên mốc gốc tối đa 100 ký tự")
	}
	return nil
}

// copySource đọc vụ nguồn để sao chép (AH-139): phải là vụ theo hộ của đúng hộ, đúng HTX trong phạm vi người gọi.
// Loại cây, giống, SOP người dùng gửi (nếu có) phải khớp vụ nguồn.
func (s *Service) copySource(ctx context.Context, tid uuid.UUID, in *HouseholdOpenInput) (*store.Season, error) {
	var src *store.Season
	err := s.inTx(ctx, tid, func(tx pgx.Tx) error {
		var err error
		src, err = householdTaskSeason(ctx, tx, tid, in.CooperativeRef, in.HouseholdRef, in.CopyFrom, false)
		return err
	})
	if err != nil {
		return nil, err
	}
	switch {
	case in.CropCode != "" && in.CropCode != src.CropCode:
		return nil, problem.Validation.WithField("crop_code", "Sao chép giữ loại cây của vụ trước")
	case in.VarietyCode != "" && in.VarietyCode != src.VarietyCode:
		return nil, problem.Validation.WithField("variety_code", "Sao chép giữ giống của vụ trước")
	case in.SOPVersionID != uuid.Nil && in.SOPVersionID != src.SOPVersionID:
		return nil, problem.Validation.WithField("sop_version_id", "Sao chép giữ quy trình của vụ trước")
	}
	in.CropCode, in.VarietyCode, in.SOPVersionID = src.CropCode, src.VarietyCode, src.SOPVersionID
	return src, nil
}

func (s *Service) householdAccess(ctx context.Context, coop uuid.UUID, write bool) (access, error) {
	fn, action := fnSeasonView, htxroles.Read
	if write {
		fn, action = fnSeason, htxroles.Create
	}
	a, err := s.matrixAccess(ctx, fn, action)
	if err != nil {
		return a, err
	}
	if coop == uuid.Nil || (a.kind != accAll && (a.p.OrgRef == uuid.Nil || a.p.OrgRef != coop)) {
		return a, ErrSeasonHousehold
	}
	return a, nil
}

// OpenHouseholdSeason performs port lookups before its own transaction. Capacity,
// membership and activity are read under the household row lock before writing.
func (s *Service) OpenHouseholdSeason(ctx context.Context, in HouseholdOpenInput) (SeasonResult, error) {
	a, err := s.householdAccess(ctx, in.CooperativeRef, true)
	if err != nil {
		return SeasonResult{}, err
	}
	if in.IdemKey == "" {
		return SeasonResult{}, problem.IdempotencyRequired
	}
	area, err := domain.ParseHouseholdSeasonArea(in.AreaHa)
	if err != nil {
		return SeasonResult{}, problem.Validation.WithField("area_ha", err.Error())
	}
	if in.HouseholdRef == uuid.Nil {
		return SeasonResult{}, ErrSeasonHousehold
	}
	if in.AnchorDate.IsZero() || in.AnchorDate.Year() < 1 || in.AnchorDate.Year() > 9999 {
		return SeasonResult{}, problem.Validation.WithField("anchor_date", "ngày hợp lệ YYYY-MM-DD")
	}
	if in.Channel == "" {
		in.Channel = ChannelApp
	}
	if in.Channel != ChannelApp && in.Channel != ChannelWeb {
		return SeasonResult{}, problem.Validation.WithField("channel", "phải là app hoặc web")
	}
	if err := in.GPS.Normalize(); err != nil {
		return SeasonResult{}, fieldProblem(err)
	}
	if in.GPS.HasPoint() {
		lon, lat := in.GPS.Lon.Round(7), in.GPS.Lat.Round(7)
		in.GPS.Lon, in.GPS.Lat = &lon, &lat
	}
	if in.GPS.AccuracyM != nil {
		if in.GPS.AccuracyM.GreaterThan(decimal.RequireFromString("999999.99")) {
			return SeasonResult{}, problem.Validation.WithField("gps.accuracy_m", "tối đa 999999.99 m")
		}
		accuracy := in.GPS.AccuracyM.Round(2)
		in.GPS.AccuracyM = &accuracy
	}
	if s.dependencyTxOpen(ctx) || s.Orgs == nil || s.Catalogs == nil {
		return SeasonResult{}, ErrDependency
	}
	if err := normalizeSelfMade(&in); err != nil {
		return SeasonResult{}, err
	}
	if in.CopyFrom != uuid.Nil {
		if _, err := s.copySource(ctx, a.p.TenantID, &in); err != nil {
			return SeasonResult{}, err
		}
	}
	if err := s.checkCoop(ctx, in.CooperativeRef); err != nil {
		return SeasonResult{}, err
	}
	if err := s.checkCatalogScope(ctx, in.CropCode, in.VarietyCode); err != nil {
		return SeasonResult{}, err
	}
	variety, err := s.checkSeasonFields(ctx, in.SeasonCode, in.SeasonYear, in.VarietyCode)
	if err != nil {
		return SeasonResult{}, err
	}
	key := "household-open:" + a.p.ActorRef.String() + ":" + in.IdemKey
	now := s.now()
	// AH-87: năng suất quy ước và cờ lúa đọc qua port trước giao dịch, chụp vào sổ sản lượng dự kiến lúc mở vụ.
	estParams, err := s.yieldEstimateParams(ctx, in.CropCode, now)
	if err != nil {
		return SeasonResult{}, err
	}
	r, err := s.writeSeason(ctx, a.p.TenantID, key, func(tx pgx.Tx) (SeasonResult, error) {
		if err := memberstore.LockHousehold(ctx, tx, in.HouseholdRef); err != nil {
			return SeasonResult{}, err
		}
		h, err := memberstore.HouseholdByRef(ctx, tx, in.HouseholdRef)
		if err != nil {
			return SeasonResult{}, err
		}
		if h == nil || h.TenantID != a.p.TenantID || h.CooperativeRef != in.CooperativeRef {
			return SeasonResult{}, ErrSeasonHousehold
		}
		if !h.Active {
			return SeasonResult{}, ErrHouseholdInactive
		}
		if h.Pending {
			return SeasonResult{}, ErrDependency.WithDetail("Hộ đang được cập nhật; thử lại sau")
		}
		if h.CultivatedAreaHa == nil {
			return SeasonResult{}, ErrHouseholdLand
		}
		used, err := memberstore.ActiveHouseholdArea(ctx, tx, h.Ref)
		if err != nil {
			return SeasonResult{}, err
		}
		remaining := decimal.Max(decimal.Zero, h.CultivatedAreaHa.Sub(used))
		if area.GreaterThan(remaining) {
			return SeasonResult{}, ErrHouseholdArea.With("remaining_area_ha", remaining.String()).With("active_area_ha", used.String()).With("land_area_ha", h.CultivatedAreaHa.String())
		}
		var templateRef, versionID uuid.UUID
		var versionNo int
		var harvestOffset, harvestWindow *int
		var snapshot json.RawMessage
		fromVersion := func(v *store.Version) {
			versionID, versionNo, harvestOffset, harvestWindow = v.ID, v.VersionNo, v.HarvestDayOffset, v.HarvestWindowDays
		}
		// selfMadeHarvest: quy trình tự tạo (AH-162, KH-12) tính ngày thu hoạch dự kiến từ việc Thu hoạch cuối;
		// không có việc Thu hoạch thì không có ngày dự kiến.
		selfMadeHarvest := func(steps []domain.Step) {
			if h, ok := domain.HarvestStep(steps); ok {
				offset, window := h.DayOffset, h.WindowDays
				harvestOffset, harvestWindow = &offset, &window
			}
		}
		switch {
		case in.CopyFrom != uuid.Nil:
			// AH-139: bản chụp của vụ nguồn (gồm lớp tùy chỉnh AH-113) dùng nguyên; ngày dự kiến tính theo mốc mới.
			src, err := householdTaskSeason(ctx, tx, a.p.TenantID, in.CooperativeRef, in.HouseholdRef, in.CopyFrom, false)
			if err != nil {
				return SeasonResult{}, err
			}
			if src.CropCode != in.CropCode || src.VarietyCode != in.VarietyCode || src.SOPVersionID != in.SOPVersionID {
				return SeasonResult{}, ErrConcurrentEdit.WithDetail("Vụ nguồn vừa đổi; mở lại form sao chép")
			}
			var base domain.SeasonSnapshot
			if err := json.Unmarshal(src.SOPSnapshot, &base); err != nil {
				return SeasonResult{}, err
			}
			v, err := store.VersionByID(ctx, tx, src.SOPVersionID, false)
			if err != nil {
				return SeasonResult{}, err
			}
			switch {
			case v != nil:
				fromVersion(v)
			case selfMadeSnapshot(src.SOPSnapshot):
				versionID, versionNo = src.SOPVersionID, src.SOPVersionNo
				selfMadeHarvest(base.Steps)
			default:
				return SeasonResult{}, ErrHouseholdSOP
			}
			copied, err := applyPlanChanges(base, in.PlanChanges)
			if err != nil {
				return SeasonResult{}, err
			}
			if snapshot, err = mergeSnapshot(src.SOPSnapshot, copied); err != nil {
				return SeasonResult{}, err
			}
			templateRef = src.SOPTemplateRef
		case in.SelfMade != nil:
			// AH-162 (FN-03-EX-03, KH-12): chỉ khi loại cây/giống chưa có SOP đang áp dụng; quy trình chỉ gắn vụ này.
			if _, _, err := householdSOP(ctx, tx, in.CooperativeRef, in.CropCode, in.VarietyCode, uuid.Nil); err == nil {
				return SeasonResult{}, ErrSOPAvailable
			} else if !errors.Is(err, ErrHouseholdSOP) {
				return SeasonResult{}, err
			}
			custom, err := applyPlanChanges(domain.SeasonSnapshot{Stages: in.SelfMade.Stages, Steps: in.SelfMade.Steps}, in.PlanChanges)
			if err != nil {
				return SeasonResult{}, err
			}
			templateRef, versionID, versionNo = ids.New(), ids.New(), 1
			selfMadeHarvest(in.SelfMade.Steps)
			fields := map[string]any{"template_ref": templateRef, "version_id": versionID, "version_no": versionNo,
				"crop_code": in.CropCode, "name": SelfMadePlanName, "anchor_name": in.SelfMadeAnchor, "self_made": true,
				"stages": in.SelfMade.Stages, "steps": in.SelfMade.Steps}
			if custom.Custom != nil {
				fields["custom"] = custom.Custom
			}
			if snapshot, err = json.Marshal(fields); err != nil {
				return SeasonResult{}, err
			}
		default:
			t, v, err := householdSOP(ctx, tx, in.CooperativeRef, in.CropCode, in.VarietyCode, in.SOPVersionID)
			if err != nil {
				return SeasonResult{}, err
			}
			fromVersion(v)
			templateRef = t.Ref
			stages, steps := v.Stages, v.Steps
			if stages == nil {
				stages = []domain.Stage{}
			}
			if steps == nil {
				steps = []domain.Step{}
			}
			custom, err := applyPlanChanges(domain.SeasonSnapshot{Stages: stages, Steps: steps}, in.PlanChanges)
			if err != nil {
				return SeasonResult{}, err
			}
			fields := map[string]any{"template_ref": t.Ref, "version_id": v.ID, "version_no": v.VersionNo,
				"crop_code": in.CropCode, "name": t.Name, "anchor_name": t.AnchorName, "stages": stages, "steps": steps}
			if custom.Custom != nil {
				fields["custom"] = custom.Custom
			}
			if snapshot, err = json.Marshal(fields); err != nil {
				return SeasonResult{}, err
			}
		}
		loc := domain.LocMissing
		if in.GPS.HasPoint() {
			loc = "unverified"
		}
		ev := store.SeasonEvent{ID: ids.New(), TenantID: a.p.TenantID, SeasonRef: ids.New(), Action: "opened",
			EntryKind: "household", CropCode: in.CropCode, SOPSnapshot: snapshot,
			HouseholdRef: h.Ref, CooperativeRef: in.CooperativeRef, CommuneCode: h.CommuneCode,
			SeasonCode: in.SeasonCode, SeasonYear: in.SeasonYear, VarietyCode: in.VarietyCode, AreaHa: area,
			SowingDate: in.AnchorDate, SOPTemplateRef: templateRef, SOPVersionID: versionID, SOPVersionNo: versionNo, CopiedFrom: in.CopyFrom,
			ExpectedYield: expectedYield(variety), GPS: in.GPS, LocationStatus: loc, Channel: in.Channel,
			ActorRef: a.p.ActorRef, OnBehalf: a.onBehalf(), EventTime: now, IdemKey: key}
		if harvestOffset != nil && harvestWindow != nil {
			ev.ExpectedHarvestDate = in.AnchorDate.AddDate(0, 0, *harvestOffset)
			ev.HarvestOpenDate = ev.ExpectedHarvestDate.AddDate(0, 0, -*harvestWindow)
			if ev.ExpectedHarvestDate.Year() < 1 || ev.ExpectedHarvestDate.Year() > 9999 || ev.HarvestOpenDate.Year() < 1 || ev.HarvestOpenDate.Year() > 9999 {
				return SeasonResult{}, problem.Validation.WithField("anchor_date", "mốc gốc làm ngày dự kiến ngoài khoảng năm hợp lệ")
			}
		}
		if err := store.InsertSeasonEvent(ctx, tx, &ev); err != nil {
			return SeasonResult{}, err
		}
		cur := store.ApplySeasonEvent(nil, ev)
		if err := store.SaveSeason(ctx, tx, a.p.TenantID, cur); err != nil {
			return SeasonResult{}, err
		}
		est, err := computeEstimateTx(ctx, tx, cur, estParams, nil, nil)
		if err != nil {
			return SeasonResult{}, err
		}
		if _, err := appendEstimateTx(ctx, tx, a.p.TenantID, cur, store.YieldEstimateComputed, est, a.p.ActorRef, now, "yield-estimate:"+key); err != nil {
			return SeasonResult{}, err
		}
		if err := afterSeasonChange(ctx, tx, a.p.TenantID, cur, now); err != nil {
			return SeasonResult{}, err
		}
		if err := enqueue(ctx, tx, a.p.TenantID, port.EventHouseholdSeasonOpened, now, port.HouseholdSeasonOpenedV1{
			SeasonRef: ev.SeasonRef, EventID: ev.ID, HouseholdRef: h.Ref, CooperativeRef: ev.CooperativeRef,
			CropCode: ev.CropCode, SeasonCode: ev.SeasonCode, SeasonYear: ev.SeasonYear, VarietyCode: ev.VarietyCode,
			SOPVersionID: ev.SOPVersionID, AreaHa: area.String(), AnchorDate: domain.FormatDay(in.AnchorDate),
			GPSMissing: !in.GPS.HasPoint(), ActorRef: a.p.ActorRef, EventTime: now.Format(time.RFC3339),
		}); err != nil {
			return SeasonResult{}, err
		}
		return s.reloadSeason(ctx, tx, ev)
	})
	if err != nil {
		return SeasonResult{}, err
	}
	// A durable ledger retry must remain scoped even after the HTTP receipt expires.
	e := r.Event
	if e.EntryKind != "household" || e.ActorRef != a.p.ActorRef || e.CooperativeRef != in.CooperativeRef || e.HouseholdRef != in.HouseholdRef ||
		e.CropCode != in.CropCode || e.VarietyCode != in.VarietyCode || e.SeasonCode != in.SeasonCode || e.SeasonYear != in.SeasonYear ||
		!e.AreaHa.Equal(area) || domain.FormatDay(e.SowingDate) != domain.FormatDay(in.AnchorDate) || e.Channel != in.Channel ||
		!sameHouseholdGPS(e.GPS, in.GPS) || (in.SOPVersionID != uuid.Nil && in.SOPVersionID != e.SOPVersionID) || e.CopiedFrom != in.CopyFrom {
		return SeasonResult{}, problem.IdempotencyConflict
	}
	items := []SeasonResult{r}
	if err := s.attachEstimates(ctx, a.p.TenantID, items); err != nil {
		return SeasonResult{}, err
	}
	return items[0], nil
}

func sameHouseholdGPS(a, b domain.GPS) bool {
	equal := func(x, y *decimal.Decimal) bool {
		if x == nil || y == nil {
			return x == nil && y == nil
		}
		return x.Equal(*y)
	}
	return a.Source == b.Source && equal(a.Lon, b.Lon) && equal(a.Lat, b.Lat) && equal(a.AccuracyM, b.AccuracyM)
}

// Existing plot routes allocate from the same household capacity. A legacy plot
// without a household keeps its old behavior; it cannot allocate a real household.
func checkLegacyHouseholdArea(ctx context.Context, tx pgx.Tx, tid, coop, hh uuid.UUID, area decimal.Decimal) error {
	if hh == uuid.Nil {
		return nil
	}
	if err := memberstore.LockHousehold(ctx, tx, hh); err != nil {
		return err
	}
	h, err := memberstore.HouseholdByRef(ctx, tx, hh)
	if err != nil {
		return err
	}
	if h == nil || h.TenantID != tid || h.CooperativeRef != coop {
		return ErrSeasonHousehold
	}
	if !h.Active {
		return ErrHouseholdInactive
	}
	if h.CultivatedAreaHa == nil {
		return ErrHouseholdLand
	}
	used, err := memberstore.ActiveHouseholdArea(ctx, tx, hh)
	if err != nil {
		return err
	}
	remaining := decimal.Max(decimal.Zero, h.CultivatedAreaHa.Sub(used))
	if area.GreaterThan(remaining) {
		return ErrHouseholdArea.With("remaining_area_ha", remaining.String())
	}
	return nil
}

func householdSOP(ctx context.Context, tx pgx.Tx, coop uuid.UUID, crop, variety string, selected uuid.UUID) (*store.Template, *store.Version, error) {
	all, err := store.HouseholdSOPTemplates(ctx, tx, coop, crop, variety)
	if err != nil {
		return nil, nil, err
	}
	binding, err := store.CurrentBinding(ctx, tx, variety, false)
	if err != nil {
		return nil, nil, err
	}
	var specific, generic []store.Template
	for _, t := range all {
		if t.VarietyCode == variety || (binding != nil && binding.TemplateRef == t.Ref) {
			specific = append(specific, t)
		} else {
			generic = append(generic, t)
		}
	}
	candidates := specific
	if len(candidates) == 0 {
		candidates = generic
	}
	if len(candidates) == 0 {
		return nil, nil, ErrHouseholdSOP
	}
	var chosen *store.Template
	var version *store.Version
	if selected != uuid.Nil {
		version, err = store.VersionByID(ctx, tx, selected, false)
		if err != nil {
			return nil, nil, err
		}
		if version != nil {
			for i := range candidates {
				if candidates[i].Ref == version.TemplateRef {
					chosen = &candidates[i]
					break
				}
			}
		}
	} else if len(candidates) == 1 {
		chosen = &candidates[0]
	}
	if chosen == nil {
		return nil, nil, problem.Validation.WithField("sop_version_id", "chọn một phiên bản SOP đang áp dụng phù hợp với loại cây và giống")
	}
	t, err := store.TemplateByRef(ctx, tx, chosen.Ref, true)
	if err != nil {
		return nil, nil, err
	}
	if t == nil || t.Status != domain.TemplatePublished || t.PublishedVersion == nil || t.Crop != crop ||
		(t.VarietyCode != "" && t.VarietyCode != variety) || (t.Scope != domain.ScopeSystem && (t.Scope != domain.ScopeHTX || t.CooperativeRef != coop)) {
		return nil, nil, ErrHouseholdSOP
	}
	if selected == uuid.Nil {
		version, err = store.LatestPublished(ctx, tx, t.Ref)
		if err != nil {
			return nil, nil, err
		}
	}
	if version == nil || version.Status != domain.VersionPublished || version.VersionNo != *t.PublishedVersion {
		return nil, nil, problem.Validation.WithField("sop_version_id", "phiên bản SOP không còn đang áp dụng; tải lại danh sách")
	}
	return t, version, nil
}

// HouseholdSeasonPage includes the immutable event for its GPS declaration.
type HouseholdSeasonPage struct {
	Items []SeasonResult
	Next  *uuid.UUID
}

func (s *Service) ListHouseholdSeasons(ctx context.Context, coop, hh, after uuid.UUID, status string, limit int) (HouseholdSeasonPage, error) {
	a, err := s.householdAccess(ctx, coop, false)
	if err != nil {
		return HouseholdSeasonPage{}, err
	}
	if status != "" && status != port.SeasonOpen && status != "completed" && status != "cancelled" && status != port.SeasonClosed {
		return HouseholdSeasonPage{}, problem.Validation.WithField("status", "trạng thái không hợp lệ")
	}
	limit = clampLimit(limit)
	page := HouseholdSeasonPage{Items: []SeasonResult{}}
	err = s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		items, err := store.HouseholdSeasons(ctx, tx, coop, hh, after, status, limit+1)
		if err != nil {
			return err
		}
		if len(items) > limit {
			items = items[:limit]
			ref := items[len(items)-1].SeasonRef
			page.Next = &ref
		}
		for _, cur := range items {
			ev, err := store.SeasonEventByID(ctx, tx, cur.CurrentEventID)
			if err != nil {
				return err
			}
			if ev == nil {
				return ErrSeasonNotFound
			}
			page.Items = append(page.Items, SeasonResult{Season: cur, Event: *ev})
		}
		return nil
	})
	if err != nil {
		return HouseholdSeasonPage{}, err
	}
	return page, s.attachEstimates(ctx, a.p.TenantID, page.Items)
}

func (s *Service) GetHouseholdSeason(ctx context.Context, coop, hh, ref uuid.UUID) (SeasonResult, error) {
	a, err := s.householdAccess(ctx, coop, false)
	if err != nil {
		return SeasonResult{}, err
	}
	var result SeasonResult
	err = s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		cur, err := store.SeasonByRef(ctx, tx, ref, false)
		if err != nil {
			return err
		}
		if cur == nil || cur.EntryKind != "household" || cur.CooperativeRef != coop || cur.HouseholdRef != hh {
			return ErrSeasonNotFound
		}
		ev, err := store.SeasonEventByID(ctx, tx, cur.CurrentEventID)
		if err != nil {
			return err
		}
		if ev == nil {
			return ErrSeasonNotFound
		}
		result = SeasonResult{Season: *cur, Event: *ev}
		return nil
	})
	if err != nil {
		return SeasonResult{}, err
	}
	items := []SeasonResult{result}
	if err := s.attachEstimates(ctx, a.p.TenantID, items); err != nil {
		return SeasonResult{}, err
	}
	return items[0], nil
}
