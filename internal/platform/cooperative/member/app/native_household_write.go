package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/member/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/member/store"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"
)

var ErrHouseholdProduction = problem.New(http.StatusConflict, "party.household_has_production", "Hộ đã có số liệu sản xuất; hãy chuyển sang Ngừng tham gia")
var nativeIDPattern = regexp.MustCompile(`^[0-9]{12}$`)
var nativePhonePattern = regexp.MustCompile(`^0[0-9]{9}$`)

type NativeHouseholdMetadata struct {
	CooperativeRef uuid.UUID `json:"cooperative_ref"`
	CommuneCode    string    `json:"commune_code"`
	Cultivated     string    `json:"cultivated_area_ha"`
	Livestock      *string   `json:"livestock_area_ha"`
	Active         bool      `json:"active"`
}
type NativeHouseholdMutation struct {
	HouseholdRef uuid.UUID `json:"household_ref"`
	CommandRef   uuid.UUID `json:"command_ref"`
	Revision     int64     `json:"revision"`
}

func nativeHouseholdWriter(ctx context.Context, action htxroles.Action) (authz.Principal, error) {
	p, err := nativeHouseholdCaller(ctx, action)
	if err != nil {
		return p, err
	}
	if p.Has(htxroles.Admin) && !p.MFA {
		return p, problem.MFARequired
	}
	return p, nil
}

func (s *Service) NativeHouseholdCommandScope(ctx context.Context, ref uuid.UUID) (port.NativeHouseholdCommandScope, error) {
	p, err := nativeHouseholdCaller(ctx, htxroles.Read)
	if err != nil {
		return port.NativeHouseholdCommandScope{}, err
	}
	var c *store.NativeHouseholdCommand
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error { var e error; c, e = store.NativeCommandByRef(ctx, tx, ref); return e })
	if err != nil {
		return port.NativeHouseholdCommandScope{}, err
	}
	if c == nil || c.ActorRef != p.ActorRef || c.CooperativeRef == nil || c.FailedCode != nil || c.Operation == "delete" {
		return port.NativeHouseholdCommandScope{}, ErrHouseholdNotFound
	}
	action := htxroles.Update
	if c.Operation == "create" {
		action = htxroles.Create
	}
	if _, err = nativeHouseholdWriter(ctx, action); err != nil {
		return port.NativeHouseholdCommandScope{}, err
	}
	if !p.Has(htxroles.Admin) && *c.CooperativeRef != p.OrgRef {
		return port.NativeHouseholdCommandScope{}, ErrHouseholdNotFound
	}
	var m NativeHouseholdMetadata
	if err = json.Unmarshal(c.Request, &m); err != nil {
		return port.NativeHouseholdCommandScope{}, err
	}
	org, err := s.coopOrg(ctx, p.TenantID, *c.CooperativeRef)
	if err != nil {
		return port.NativeHouseholdCommandScope{}, err
	}
	if org == nil {
		return port.NativeHouseholdCommandScope{}, ErrOrgNotFound
	}
	if c.CompletedAt == nil {
		if c.Operation == "create" && !org.Active {
			return port.NativeHouseholdCommandScope{}, ErrOrgState
		}
		if c.Operation == "update" {
			var household *domain.Household
			err := tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
				var e error
				household, e = store.HouseholdByRef(ctx, tx, c.HouseholdRef)
				return e
			})
			if err != nil {
				return port.NativeHouseholdCommandScope{}, err
			}
			if household == nil || household.CooperativeRef != *c.CooperativeRef || household.Revision != c.ExpectedRevision {
				return port.NativeHouseholdCommandScope{}, ErrHouseholdNotFound
			}
		}
	}
	return port.NativeHouseholdCommandScope{CommandRef: c.Ref, HouseholdRef: c.HouseholdRef, CooperativeRef: *c.CooperativeRef,
		Operation: c.Operation, ExpectedRevision: c.ExpectedRevision, CommuneCode: m.CommuneCode, Completed: c.CompletedAt != nil}, nil
}

func (s *Service) NativeSaveHousehold(ctx context.Context, ref uuid.UUID, revision int64, key string, m NativeHouseholdMetadata, id port.HouseholdIdentity) (NativeHouseholdMutation, error) {
	action, op := htxroles.Update, "update"
	if ref == uuid.Nil {
		action, op = htxroles.Create, "create"
	}
	p, err := nativeHouseholdWriter(ctx, action)
	if err != nil {
		return NativeHouseholdMutation{}, err
	}
	if key == "" {
		return NativeHouseholdMutation{}, problem.IdempotencyRequired
	}
	if !nativeIDPattern.MatchString(strings.TrimSpace(id.NationalID)) {
		return NativeHouseholdMutation{}, problem.Validation.WithField("national_id", "CCCD gồm đúng 12 chữ số")
	}
	if !nativePhonePattern.MatchString(strings.TrimSpace(id.Phone)) {
		return NativeHouseholdMutation{}, problem.Validation.WithField("phone", "SĐT gồm 10 chữ số, bắt đầu bằng 0")
	}
	if n := utf8.RuneCountInString(strings.TrimSpace(id.FullName)); n < 1 || n > 200 {
		return NativeHouseholdMutation{}, problem.Validation.WithField("full_name", "Bắt buộc nhập, tối đa 200 ký tự")
	}
	if utf8.RuneCountInString(id.Address) > 500 {
		return NativeHouseholdMutation{}, problem.Validation.WithField("address", "Tối đa 500 ký tự")
	}
	area, err := decimal.NewFromString(m.Cultivated)
	if err != nil || !area.IsPositive() {
		return NativeHouseholdMutation{}, problem.Validation.WithField("cultivated_area_ha", "Diện tích phải lớn hơn 0")
	}
	if !domain.ValidArea(area) {
		return NativeHouseholdMutation{}, problem.Validation.WithField("cultivated_area_ha", domain.ErrArea.Error())
	}
	m.Cultivated = area.String()
	if m.Livestock != nil {
		v, e := decimal.NewFromString(*m.Livestock)
		if e != nil || v.IsNegative() {
			return NativeHouseholdMutation{}, problem.Validation.WithField("livestock_area_ha", "Diện tích không được âm")
		}
		if !domain.ValidArea(v) {
			return NativeHouseholdMutation{}, problem.Validation.WithField("livestock_area_ha", domain.ErrArea.Error())
		}
		raw := v.String()
		m.Livestock = &raw
	}
	if m.Livestock == nil {
		zero := "0"
		m.Livestock = &zero
	}
	if !p.Has(htxroles.Admin) && m.CooperativeRef == uuid.Nil {
		m.CooperativeRef = p.OrgRef
	}
	if m.CooperativeRef == uuid.Nil {
		return NativeHouseholdMutation{}, problem.Validation.WithField("cooperative_ref", "Bắt buộc nhập")
	}
	if !p.Has(htxroles.Admin) && m.CooperativeRef != p.OrgRef {
		return NativeHouseholdMutation{}, ErrHouseholdNotFound
	}
	org, err := s.coopOrg(ctx, p.TenantID, m.CooperativeRef)
	if err != nil {
		return NativeHouseholdMutation{}, err
	}
	if org == nil {
		return NativeHouseholdMutation{}, ErrOrgNotFound
	}
	if ref == uuid.Nil {
		revision = 0
		m.Active = true
		if !org.Active {
			return NativeHouseholdMutation{}, ErrOrgState
		}
	}
	if m.CommuneCode == "" {
		m.CommuneCode = org.AdminUnitCode
	}
	if s.Units == nil {
		return NativeHouseholdMutation{}, ErrDependency
	}
	unit, err := s.Units.Get(ctx, m.CommuneCode, s.now())
	if err != nil || unit.Disabled || string(unit.Level) != "commune" {
		return NativeHouseholdMutation{}, problem.Validation.WithField("commune_code", "Chọn xã / phường đang sử dụng")
	}
	cmd, err := s.startNativeHouseholdCommand(ctx, p, ref, revision, key, op, m)
	if err != nil {
		return NativeHouseholdMutation{}, err
	}
	writer, ok := s.Vault.(port.NativeHouseholdIdentityWriter)
	if !ok {
		return NativeHouseholdMutation{}, ErrDependency.WithDetail("Chưa nối ghi định danh hộ theo quyền HTX")
	}
	// Even a completed retry must verify the original identity payload at Vault.
	// Its immutable receipt makes this a no-op instead of overwriting a newer edit.
	if err = writer.WriteNativeHouseholdIdentity(ctx, cmd.Ref, id); err != nil {
		var pr *problem.Problem
		// Only duplicate-index outcomes prove that this command has never
		// committed at Vault. A conflicting replay must not release a lease
		// whose original identity mutation may already have committed.
		if errors.As(err, &pr) && (pr.Code == "vault.household_national_id_taken" || pr.Code == "vault.household_phone_taken") && cmd.CompletedAt == nil {
			if failErr := tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error { return store.FailNativeCommand(ctx, tx, cmd.Ref, pr.Code) }); failErr != nil {
				return NativeHouseholdMutation{}, failErr
			}
		}
		return NativeHouseholdMutation{}, vaultError(err)
	}
	result, err := s.finishNativeHouseholdCommand(ctx, p, *cmd, m)
	if err != nil {
		return NativeHouseholdMutation{}, ErrDependency.WithDetail("Chưa hoàn tất cập nhật hộ; giữ nguyên thao tác để thử lại").WithCause(err)
	}
	return result, nil
}

func (s *Service) startNativeHouseholdCommand(ctx context.Context, p authz.Principal, ref uuid.UUID, revision int64, key, op string, m NativeHouseholdMetadata) (*store.NativeHouseholdCommand, error) {
	raw, _ := json.Marshal(m)
	var command *store.NativeHouseholdCommand
	err := tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('native-hh-command:'||shared.current_tenant()::text||':'||$1||':'||$2,285))`, p.ActorRef.String(), key); err != nil {
			return err
		}
		old, err := store.NativeCommandByKey(ctx, tx, p.ActorRef, key)
		if err != nil {
			return err
		}
		if old != nil {
			var stored NativeHouseholdMetadata
			if err := json.Unmarshal(old.Request, &stored); err != nil {
				return err
			}
			canonical, _ := json.Marshal(stored)
			if old.FailedCode != nil || old.Operation != op || old.ExpectedRevision != revision || (ref != uuid.Nil && old.HouseholdRef != ref) || !bytes.Equal(canonical, raw) {
				return problem.IdempotencyConflict
			}
			command = old
			return nil
		}
		if op != "create" {
			if err := store.LockHousehold(ctx, tx, ref); err != nil {
				return err
			}
			h, err := store.HouseholdByRef(ctx, tx, ref)
			if err != nil {
				return err
			}
			if h == nil || h.CooperativeRef != m.CooperativeRef || (!p.Has(htxroles.Admin) && h.CooperativeRef != p.OrgRef) {
				return ErrHouseholdNotFound
			}
			if revision < 1 || h.Revision != revision {
				return problem.IdempotencyConflict
			}
			if op == "update" {
				area, _ := decimal.NewFromString(m.Cultivated)
				used, err := store.ActiveHouseholdArea(ctx, tx, ref)
				if err != nil {
					return err
				}
				if area.LessThan(used) {
					return problem.Validation.WithField("cultivated_area_ha", "Không được nhỏ hơn tổng diện tích vụ đang canh tác ("+used.String()+" ha)")
				}
			}
		} else {
			ref = ids.New()
		}
		command = &store.NativeHouseholdCommand{Ref: ids.New(), HouseholdRef: ref, CooperativeRef: &m.CooperativeRef, ActorRef: p.ActorRef, Operation: op, IdemKey: key, ExpectedRevision: revision, Request: raw}
		return store.InsertNativeCommand(ctx, tx, p.TenantID, *command)
	})
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.ConstraintName == "native_household_pending" {
		return nil, ErrDependency.WithDetail("Hộ đang được cập nhật; thử lại thao tác đang chờ")
	}
	return command, err
}

func (s *Service) finishNativeHouseholdCommand(ctx context.Context, p authz.Principal, c store.NativeHouseholdCommand, m NativeHouseholdMetadata) (NativeHouseholdMutation, error) {
	ctx = authz.WithPurpose(ctx, "household.native_commit")
	result := NativeHouseholdMutation{HouseholdRef: c.HouseholdRef, CommandRef: c.Ref, Revision: c.ExpectedRevision + 1}
	err := tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('native-hh-commit:'||shared.current_tenant()::text||':'||$1::text,285))`, c.HouseholdRef.String()); err != nil {
			return err
		}
		current, err := store.NativeCommandByRef(ctx, tx, c.Ref)
		if err != nil {
			return err
		}
		if current == nil || current.FailedCode != nil {
			return problem.IdempotencyConflict
		}
		if current.CompletedAt != nil {
			return nil
		}
		area, _ := decimal.NewFromString(m.Cultivated)
		var livestock *decimal.Decimal
		if m.Livestock != nil {
			v, _ := decimal.NewFromString(*m.Livestock)
			livestock = &v
		}
		before := []byte(`{}`)
		if c.Operation == "create" {
			h := domain.Household{Ref: c.HouseholdRef, CooperativeRef: m.CooperativeRef, CommuneCode: m.CommuneCode, CultivatedAreaHa: &area, LivestockAreaHa: livestock, Active: true, CreatedAt: s.now()}
			inserted, err := store.InsertHousehold(ctx, tx, p.TenantID, h, p.ActorRef)
			if err != nil {
				return err
			}
			if !inserted {
				return problem.IdempotencyConflict
			}
			if err := enqueueRegistered(ctx, tx, p, h, port.HouseholdSourceHTXApp, s.now()); err != nil {
				return err
			}
		} else {
			if err := store.LockHousehold(ctx, tx, c.HouseholdRef); err != nil {
				return err
			}
			h, err := store.HouseholdByRef(ctx, tx, c.HouseholdRef)
			if err != nil {
				return err
			}
			if h == nil || h.CooperativeRef != m.CooperativeRef {
				return ErrHouseholdNotFound
			}
			if h.Revision != c.ExpectedRevision {
				return problem.IdempotencyConflict
			}
			before = snapshotJSON(map[string]any{"cultivated_area_ha": h.CultivatedAreaHa, "livestock_area_ha": h.LivestockAreaHa, "active": h.Active, "commune_code": h.CommuneCode})
			used, err := store.ActiveHouseholdArea(ctx, tx, h.Ref)
			if err != nil {
				return err
			}
			if c.Operation == "update" && area.LessThan(used) {
				return problem.Validation.WithField("cultivated_area_ha", "Không được nhỏ hơn tổng diện tích vụ đang canh tác ("+used.String()+" ha)")
			}
		}
		after, _ := json.Marshal(m)
		event := store.ProfileEvent{ID: c.Ref, TenantID: p.TenantID, SubjectKind: store.ProfileHousehold, SubjectRef: c.HouseholdRef, CommuneCode: m.CommuneCode, Action: "updated", Before: before, After: after, EventTime: s.now(), ActorRef: p.ActorRef, IdemKey: "native-household:" + c.Ref.String()}
		if err := store.InsertProfileEvent(ctx, tx, &event); err != nil {
			return err
		}
		if err := store.UpdateNativeHousehold(ctx, tx, c.HouseholdRef, m.CommuneCode, m.Cultivated, m.Livestock, m.Active && c.Operation != "delete", c.Operation == "delete", result.Revision); err != nil {
			return err
		}
		return store.FinishNativeCommand(ctx, tx, c.Ref)
	})
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.ConstraintName == "household_has_production" {
		return NativeHouseholdMutation{}, ErrHouseholdProduction
	}
	return result, err
}

func (s *Service) NativeDeleteHousehold(ctx context.Context, ref uuid.UUID, revision int64, key string) (NativeHouseholdMutation, error) {
	ctx = authz.WithPurpose(ctx, "household.native_delete")
	p, err := nativeHouseholdWriter(ctx, htxroles.Delete)
	if err != nil {
		return NativeHouseholdMutation{}, err
	}
	if key == "" {
		return NativeHouseholdMutation{}, problem.IdempotencyRequired
	}
	var previous *store.NativeHouseholdCommand
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		previous, e = store.NativeCommandByKey(ctx, tx, p.ActorRef, key)
		return e
	})
	if err != nil {
		return NativeHouseholdMutation{}, err
	}
	if previous != nil && previous.CompletedAt != nil && previous.FailedCode == nil && previous.Operation == "delete" && previous.HouseholdRef == ref && previous.ExpectedRevision == revision {
		return NativeHouseholdMutation{HouseholdRef: ref, CommandRef: previous.Ref, Revision: revision + 1}, nil
	}
	if previous != nil && previous.CompletedAt == nil && previous.FailedCode == nil && previous.Operation == "delete" && previous.HouseholdRef == ref && previous.ExpectedRevision == revision {
		if previous.CooperativeRef == nil || (!p.Has(htxroles.Admin) && *previous.CooperativeRef != p.OrgRef) {
			return NativeHouseholdMutation{}, ErrHouseholdNotFound
		}
		var metadata NativeHouseholdMetadata
		if err := json.Unmarshal(previous.Request, &metadata); err != nil {
			return NativeHouseholdMutation{}, err
		}
		return s.finishNativeDeletion(ctx, p, *previous, metadata)
	}
	scope, err := s.NativeHouseholdIdentityScope(ctx, ref)
	if err != nil {
		return NativeHouseholdMutation{}, err
	}
	if s.ProductionHistory == nil {
		return NativeHouseholdMutation{}, ErrDependency.WithDetail("Chưa nối kiểm tra lịch sử sản xuất")
	}
	used, err := s.ProductionHistory.HasHouseholdProduction(ctx, ref)
	if err != nil {
		return NativeHouseholdMutation{}, ErrDependency.WithCause(err)
	}
	if used {
		return NativeHouseholdMutation{}, ErrHouseholdProduction
	}
	var h *domain.Household
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error { var e error; h, e = store.HouseholdByRef(ctx, tx, ref); return e })
	if err != nil {
		return NativeHouseholdMutation{}, err
	}
	if h == nil {
		return NativeHouseholdMutation{}, ErrHouseholdNotFound
	}
	m := NativeHouseholdMetadata{CooperativeRef: scope.CooperativeRef, CommuneCode: h.CommuneCode, Cultivated: "0", Active: false}
	if h.CultivatedAreaHa != nil {
		m.Cultivated = h.CultivatedAreaHa.String()
	}
	if h.LivestockAreaHa != nil {
		v := h.LivestockAreaHa.String()
		m.Livestock = &v
	}
	c, err := s.startNativeHouseholdCommand(ctx, p, ref, revision, key, "delete", m)
	if err != nil {
		return NativeHouseholdMutation{}, err
	}
	return s.finishNativeDeletion(ctx, p, *c, m)
}

func (s *Service) finishNativeDeletion(ctx context.Context, p authz.Principal, c store.NativeHouseholdCommand, m NativeHouseholdMetadata) (NativeHouseholdMutation, error) {
	if s.ProductionHistory == nil {
		return NativeHouseholdMutation{}, ErrDependency.WithDetail("Chưa nối kiểm tra lịch sử sản xuất")
	}
	var result NativeHouseholdMutation
	used, err := s.ProductionHistory.HasHouseholdProduction(ctx, c.HouseholdRef)
	if err != nil {
		return result, ErrDependency.WithCause(err)
	}
	if used {
		err = ErrHouseholdProduction
	} else {
		result, err = s.finishNativeHouseholdCommand(ctx, p, c, m)
	}
	var pr *problem.Problem
	if errors.As(err, &pr) && pr.Status >= 400 && pr.Status < 500 {
		if failErr := tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error { return store.FailNativeCommand(ctx, tx, c.Ref, pr.Code) }); failErr != nil {
			return NativeHouseholdMutation{}, failErr
		}
	}
	return result, err
}
