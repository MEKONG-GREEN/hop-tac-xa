package app

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/store"
	mdport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
)

var ErrStrawFactorTaken = problem.New(http.StatusConflict, "cropcycle.straw_factor_taken", "Giống này đã có hệ số")
var ErrStrawFactorMissing = problem.New(http.StatusNotFound, "cropcycle.straw_factor_not_found", "Giống chưa có hệ số được cấu hình")
var ErrStrawFactorConcurrent = problem.New(http.StatusConflict, "cropcycle.straw_factor_concurrent", "Hệ số đã thay đổi; tải lại trước khi sửa")
var factorPattern = regexp.MustCompile(`^(?:0|[1-9][0-9]{0,2})(?:\.[0-9]{1,4})?$`)

func strawFactorCaller(ctx context.Context, action htxroles.Action) (authz.Principal, error) {
	p, err := authz.Require(ctx, htxroles.With("FN-11", action)...)
	if err != nil {
		return p, err
	}
	if !htxroles.Allows(p.Has, "FN-11", action) {
		return p, problem.Forbidden
	}
	if !p.MFA {
		return p, problem.MFARequired
	}
	return p, nil
}

func (s *Service) SaveStrawFactor(ctx context.Context, code, raw, key string, expected int64, create bool) (store.StrawFactorEvent, error) {
	action := htxroles.Update
	if create {
		action = htxroles.Create
	}
	p, err := strawFactorCaller(ctx, action)
	if err != nil {
		return store.StrawFactorEvent{}, err
	}
	code, raw = strings.TrimSpace(code), strings.TrimSpace(raw)
	if code == "" {
		return store.StrawFactorEvent{}, problem.Validation.WithField("variety_code", "Bắt buộc nhập")
	}
	if raw == "" {
		return store.StrawFactorEvent{}, problem.Validation.WithField("percentage", "Bắt buộc nhập")
	}
	if key == "" {
		return store.StrawFactorEvent{}, problem.IdempotencyRequired
	}
	if !factorPattern.MatchString(raw) {
		return store.StrawFactorEvent{}, problem.Validation.WithField("percentage", "Hệ số từ 0 đến 100%")
	}
	percentage, _ := decimal.NewFromString(raw)
	if percentage.GreaterThan(decimal.NewFromInt(100)) {
		return store.StrawFactorEvent{}, problem.Validation.WithField("percentage", "Hệ số từ 0 đến 100%")
	}
	validateReplay := func(old *store.StrawFactorEvent) error {
		if old.ActorRef != p.ActorRef || old.VarietyCode != code || !old.Percentage.Equal(percentage) || old.Seq-1 != expected || create != (old.Seq == 1) {
			return problem.IdempotencyConflict
		}
		return nil
	}
	var receipt *store.StrawFactorEvent
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		receipt, e = store.StrawFactorByIdem(ctx, tx, key)
		return e
	})
	if err != nil {
		return store.StrawFactorEvent{}, err
	}
	// A completed operation remains replayable after its variety is hidden or
	// the catalog service becomes unavailable; it must never create a new event.
	if receipt != nil {
		return *receipt, validateReplay(receipt)
	}
	if s.Catalogs == nil {
		return store.StrawFactorEvent{}, ErrDependency
	}
	variety, err := s.Catalogs.Get(ctx, mdport.CatalogRiceVariety, code)
	if err != nil {
		if !errors.Is(err, mdport.ErrCatalogItemNotFound) {
			return store.StrawFactorEvent{}, ErrDependency.WithCause(err)
		}
		return store.StrawFactorEvent{}, ErrCatalogInvalid.WithField("variety_code", "Chọn giống lúa đang có trong danh mục")
	}
	crop, err := s.Catalogs.Get(ctx, mdport.CatalogCrop, variety.Attrs[mdport.AttrCropCode])
	if err != nil && !errors.Is(err, mdport.ErrCatalogItemNotFound) {
		return store.StrawFactorEvent{}, ErrDependency.WithCause(err)
	}
	if err != nil || crop.Attrs[mdport.AttrIsRice] != "true" || (create && (!variety.Active || !crop.Active)) {
		return store.StrawFactorEvent{}, ErrCatalogInvalid.WithField("variety_code", "Chọn giống thuộc loại cây lúa đang sử dụng")
	}
	var result store.StrawFactorEvent
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		if err := store.LockStrawFactorOperation(ctx, tx, key, code); err != nil {
			return err
		}
		old, err := store.StrawFactorByIdem(ctx, tx, key)
		if err != nil {
			return err
		}
		if old != nil {
			result = *old
			return validateReplay(old)
		}
		current, err := store.StrawFactorAt(ctx, tx, code, time.Time{})
		if err != nil {
			return err
		}
		if create && current != nil {
			return ErrStrawFactorTaken
		}
		if !create && current == nil {
			return ErrStrawFactorMissing
		}
		if (create && expected != 0) || (!create && expected != current.Seq) {
			return ErrStrawFactorConcurrent
		}
		result = store.StrawFactorEvent{ID: ids.New(), TenantID: p.TenantID, ActorRef: p.ActorRef, VarietyCode: code, Percentage: percentage, IdemKey: key}
		if current != nil {
			result.Previous = &current.Percentage
		}
		return store.InsertStrawFactor(ctx, tx, &result)
	})
	return result, err
}

type StrawFactorRow struct {
	VarietyCode string     `json:"variety_code"`
	VarietyName string     `json:"variety_name"`
	Active      bool       `json:"active"`
	Configured  bool       `json:"configured"`
	Percentage  string     `json:"percentage"`
	Revision    int64      `json:"revision"`
	EffectiveAt *time.Time `json:"effective_at"`
}

func (s *Service) ListStrawFactors(ctx context.Context) ([]StrawFactorRow, error) {
	p, err := strawFactorCaller(ctx, htxroles.Read)
	if err != nil {
		return nil, err
	}
	if s.Catalogs == nil {
		return nil, ErrDependency
	}
	varieties, err := s.Catalogs.List(ctx, mdport.CatalogRiceVariety)
	if err != nil {
		return nil, err
	}
	crops, err := s.Catalogs.List(ctx, mdport.CatalogCrop)
	if err != nil {
		return nil, err
	}
	rice := map[string]bool{}
	for _, c := range crops {
		rice[c.Code] = c.Attrs[mdport.AttrIsRice] == "true"
	}
	var current map[string]store.StrawFactorEvent
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error { var e error; current, e = store.CurrentStrawFactors(ctx, tx); return e })
	if err != nil {
		return nil, err
	}
	rows := map[string]StrawFactorRow{}
	for _, v := range varieties {
		if rice[v.Attrs[mdport.AttrCropCode]] {
			rows[v.Code] = StrawFactorRow{VarietyCode: v.Code, VarietyName: v.Name, Active: v.Active, Percentage: "35"}
		}
	}
	for code, e := range current {
		r, ok := rows[code]
		if !ok {
			r = StrawFactorRow{VarietyCode: code, VarietyName: code}
		}
		r.Configured = true
		r.Percentage = e.Percentage.String()
		r.Revision = e.Seq
		r.EffectiveAt = &e.EffectiveAt
		rows[code] = r
	}
	out := make([]StrawFactorRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].VarietyCode < out[j].VarietyCode })
	return out, nil
}

func (s *Service) StrawFactorHistory(ctx context.Context, code string, after int64, limit int) ([]store.StrawFactorEvent, error) {
	p, err := strawFactorCaller(ctx, htxroles.Read)
	if err != nil {
		return nil, err
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	if after < 0 {
		return nil, problem.BadRequest
	}
	var out []store.StrawFactorEvent
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		out, e = store.StrawFactorHistory(ctx, tx, strings.TrimSpace(code), after, limit)
		return e
	})
	return out, err
}

// Internal port: authorization belongs to the statistics use case that calls
// it. Tenant context is mandatory and RLS still applies to every lookup.
func (s *Service) StrawFactorAt(ctx context.Context, code string, at time.Time) (decimal.Decimal, error) {
	var e *store.StrawFactorEvent
	err := tenant.FromCtxTx(ctx, s.DB, func(tx pgx.Tx) error {
		var err error
		e, err = store.StrawFactorAt(ctx, tx, strings.TrimSpace(code), at)
		return err
	})
	if err != nil {
		return decimal.Zero, err
	}
	if e == nil {
		return decimal.NewFromInt(35), nil
	}
	return e.Percentage, nil
}

var _ port.StrawFactors = (*Service)(nil)
