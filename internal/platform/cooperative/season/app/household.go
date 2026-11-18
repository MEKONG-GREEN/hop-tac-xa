package app

import (
	"context"
	"encoding/json"
	"net/http"
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
		t, v, err := householdSOP(ctx, tx, in.CooperativeRef, in.CropCode, in.VarietyCode, in.SOPVersionID)
		if err != nil {
			return SeasonResult{}, err
		}
		stages, steps := v.Stages, v.Steps
		if stages == nil {
			stages = []domain.Stage{}
		}
		if steps == nil {
			steps = []domain.Step{}
		}
		snapshot, err := json.Marshal(map[string]any{"template_ref": t.Ref, "version_id": v.ID, "version_no": v.VersionNo,
			"crop_code": in.CropCode, "name": t.Name, "anchor_name": t.AnchorName, "stages": stages, "steps": steps})
		if err != nil {
			return SeasonResult{}, err
		}
		loc := domain.LocMissing
		if in.GPS.HasPoint() {
			loc = "unverified"
		}
		ev := store.SeasonEvent{ID: ids.New(), TenantID: a.p.TenantID, SeasonRef: ids.New(), Action: "opened",
			EntryKind: "household", CropCode: in.CropCode, SOPSnapshot: snapshot,
			HouseholdRef: h.Ref, CooperativeRef: in.CooperativeRef, CommuneCode: h.CommuneCode,
			SeasonCode: in.SeasonCode, SeasonYear: in.SeasonYear, VarietyCode: in.VarietyCode, AreaHa: area,
			SowingDate: in.AnchorDate, SOPTemplateRef: t.Ref, SOPVersionID: v.ID, SOPVersionNo: v.VersionNo,
			ExpectedYield: expectedYield(variety), GPS: in.GPS, LocationStatus: loc, Channel: in.Channel,
			ActorRef: a.p.ActorRef, OnBehalf: a.onBehalf(), EventTime: now, IdemKey: key}
		if v.HarvestDayOffset != nil && v.HarvestWindowDays != nil {
			ev.ExpectedHarvestDate = in.AnchorDate.AddDate(0, 0, *v.HarvestDayOffset)
			ev.HarvestOpenDate = ev.ExpectedHarvestDate.AddDate(0, 0, -*v.HarvestWindowDays)
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
		!sameHouseholdGPS(e.GPS, in.GPS) || (in.SOPVersionID != uuid.Nil && in.SOPVersionID != e.SOPVersionID) {
		return SeasonResult{}, problem.IdempotencyConflict
	}
	return r, nil
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
	if status != "" && status != port.SeasonOpen && status != "completed" && status != "cancelled" {
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
	return page, err
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
	return result, err
}
