package app

import (
	"context"
	"encoding/base32"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/masterdata/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/masterdata/store"
	partyport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/party/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
)

var (
	ErrUnitNameTaken = problem.New(http.StatusConflict, "masterdata.admin_unit_name_taken", "Tên đã có trong cùng đơn vị cấp trên")
	ErrUnitChanged   = problem.New(http.StatusConflict, "masterdata.admin_unit_changed", "Đơn vị đã thay đổi, vui lòng tải lại")
)

type ManagedUnit struct {
	Code             string          `json:"code"`
	Name             string          `json:"name"`
	Level            partyport.Level `json:"level"`
	ParentCode       string          `json:"parent_code"`
	ValidFrom        string          `json:"valid_from"`
	Active           bool            `json:"active"`
	Selectable       bool            `json:"selectable"`
	Revision         int64           `json:"revision"`
	CommuneCount     int             `json:"commune_count"`
	CooperativeCount *int64          `json:"cooperative_count"`
}

type ManagedUnitInput struct {
	Name       string
	Level      partyport.Level
	ParentCode string
	Active     bool
}

func managedUnitView(m store.ManagedUnit) ManagedUnit {
	return ManagedUnit{Code: m.Unit.Code, Name: m.Unit.Name, Level: m.Unit.Level, ParentCode: m.Unit.ParentCode,
		ValidFrom: m.Unit.ValidFrom.Format(time.DateOnly), Active: m.Active, Selectable: m.Selectable, Revision: m.Revision}
}

// ManagedUnitList is for administration, including disabled options. The
// application picker APIs continue to return only selectable options.
func (s *Service) ManagedUnitList(ctx context.Context, parent, query, state, after string, limit int) (Page[ManagedUnit], error) {
	p, err := authz.Require(ctx, authz.RolePlatformAdmin)
	if err != nil {
		return Page[ManagedUnit]{}, err
	}
	if state != "" && state != "active" && state != "inactive" {
		return Page[ManagedUnit]{}, problem.Validation.WithField("state", "Trạng thái không hợp lệ")
	}
	limit = clampLimit(limit)
	var units []store.ManagedUnit
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		units, e = store.ManagedUnits(ctx, tx, domain.DayOf(s.Clock.Now()), nil)
		return e
	})
	if err != nil {
		return Page[ManagedUnit]{}, err
	}
	counts := map[string]int{}
	for _, u := range units {
		if u.Unit.Level == partyport.LevelCommune {
			counts[u.Unit.ParentCode]++
		}
	}
	items := []ManagedUnit{}
	q := domain.UnitNameKey(query)
	for _, u := range units {
		if (parent == "" && u.Unit.Level != partyport.LevelProvince) || (parent != "" && (u.Unit.Level != partyport.LevelCommune || u.Unit.ParentCode != parent)) || u.Unit.Code <= after {
			continue
		}
		if (state == "active" && !u.Active) || (state == "inactive" && u.Active) {
			continue
		}
		if q != "" && !strings.Contains(domain.UnitNameKey(u.Unit.Name), q) && !strings.Contains(domain.UnitNameKey(u.Unit.Code), q) {
			continue
		}
		v := managedUnitView(u)
		v.CommuneCount = counts[v.Code]
		items = append(items, v)
		if len(items) > limit {
			break
		}
	}
	pg := Page[ManagedUnit]{Items: items}
	if len(items) > limit {
		pg.Items = items[:limit]
		last := pg.Items[limit-1].Code
		pg.Next = &last
	}
	if parent != "" && s.UnitUsage != nil {
		codes := make([]string, 0, len(pg.Items))
		for _, item := range pg.Items {
			codes = append(codes, item.Code)
		}
		counts, err := s.UnitUsage.CooperativeCountsByUnits(ctx, codes)
		if err != nil {
			return Page[ManagedUnit]{}, err
		}
		for i := range pg.Items {
			n := counts[pg.Items[i].Code]
			pg.Items[i].CooperativeCount = &n
		}
	}
	return pg, nil
}

func unitByCode(units []store.ManagedUnit, code string) *store.ManagedUnit {
	for i := range units {
		if units[i].Unit.Code == code {
			return &units[i]
		}
	}
	return nil
}

func unitNameAvailable(units []store.ManagedUnit, in ManagedUnitInput, except string) bool {
	for _, u := range units {
		if u.Unit.Code != except && u.Unit.Level == in.Level && u.Unit.ParentCode == in.ParentCode && domain.UnitNameKey(u.Unit.Name) == domain.UnitNameKey(in.Name) {
			return false
		}
	}
	return true
}

func managedUnitParent(units []store.ManagedUnit, in ManagedUnitInput) error {
	if in.Level == partyport.LevelProvince {
		return nil
	}
	p := unitByCode(units, in.ParentCode)
	if p == nil || p.Unit.Level != partyport.LevelProvince || !p.Selectable {
		return problem.Validation.WithField("parent_code", "Chọn tỉnh / thành phố đang sử dụng")
	}
	return nil
}

// CreateManagedUnit generates the immutable business code on the server. All
// catalog validation and ledger/projection/outbox writes use the same own-module
// transaction. No external ports are called while the transaction is open.
func (s *Service) CreateManagedUnit(ctx context.Context, in ManagedUnitInput, key string) (ManagedUnit, bool, error) {
	return s.writeManagedUnit(ctx, "", in, 0, key)
}

func (s *Service) UpdateManagedUnit(ctx context.Context, code, name string, active bool, expected int64, key string) (ManagedUnit, bool, error) {
	return s.writeManagedUnit(ctx, code, ManagedUnitInput{Name: name, Active: active}, expected, key)
}

func (s *Service) writeManagedUnit(ctx context.Context, code string, in ManagedUnitInput, expected int64, key string) (ManagedUnit, bool, error) {
	p, err := authz.Require(ctx, authz.RolePlatformAdmin)
	if err != nil {
		return ManagedUnit{}, false, err
	}
	if key == "" {
		return ManagedUnit{}, false, problem.IdempotencyRequired
	}
	in.Name = domain.NormalizeUnitName(in.Name)
	in.ParentCode = strings.TrimSpace(in.ParentCode)
	create := code == ""
	operation := "update"
	if create {
		operation = "create"
	}
	idem := "admin-unit:" + operation + ":" + p.ActorRef.String() + ":" + key
	var result ManagedUnit
	replayed := false
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		if err := store.LockImport(ctx, tx, p.TenantID); err != nil {
			return err
		}
		old, err := store.UnitProfileByKey(ctx, tx, idem)
		if err != nil {
			return err
		}
		if old != nil {
			base, err := store.UnitVersion(ctx, tx, old.Code, old.UnitValidFrom)
			if err != nil {
				return err
			}
			if base == nil {
				return ErrUnitNotFound
			}
			if old.Name != in.Name || old.Active != in.Active || (!create && (old.Code != code || old.Revision-1 != expected)) || (create && (base.Level != in.Level || base.ParentCode != in.ParentCode)) {
				return problem.IdempotencyConflict
			}
			base.Name = old.Name
			result = managedUnitView(store.ManagedUnit{Unit: *base, Active: old.Active, Selectable: old.Active, Revision: old.Revision})
			replayed = true
			return nil
		}
		units, err := store.ManagedUnits(ctx, tx, domain.DayOf(s.Clock.Now()), nil)
		if err != nil {
			return err
		}
		var base domain.Unit
		var previous *store.UnitProfileEvent
		if create {
			created, err := s.insertManagedUnit(ctx, tx, p, units, in, idem)
			result = managedUnitView(created)
			return err
		} else {
			current := unitByCode(units, code)
			if current == nil {
				return ErrUnitNotFound
			}
			if expected < 0 || current.Revision != expected {
				return ErrUnitChanged
			}
			base = current.Unit
			in.Level = base.Level
			in.ParentCode = base.ParentCode
			if err := domain.ValidateManagedUnit(in.Name, in.Level, in.ParentCode); err != nil {
				return asProblem(err)
			}
			if in.Active && !current.Active {
				if err := managedUnitParent(units, in); err != nil {
					return err
				}
			}
			previous, err = store.LatestUnitProfile(ctx, tx, code, base.ValidFrom)
			if err != nil {
				return err
			}
		}
		if !unitNameAvailable(units, in, code) {
			return ErrUnitNameTaken
		}
		e := store.UnitProfileEvent{ID: ids.New(), TenantID: p.TenantID, ActorRef: p.ActorRef, Code: code, Name: in.Name, Active: in.Active, UnitValidFrom: base.ValidFrom, Action: "updated", EventTime: s.Clock.Now(), IdemKey: idem}
		if previous != nil {
			e.SupersedesID = &previous.ID
			reason := "Cập nhật tên hoặc trạng thái sử dụng đơn vị hành chính"
			e.CorrectionReason = &reason
		}
		if err := store.InsertUnitProfile(ctx, tx, &e); err != nil {
			return err
		}
		if err := store.ProjectUnitProfile(ctx, tx, e); err != nil {
			return err
		}
		base.Name = in.Name
		selectable := in.Active
		if in.Level == partyport.LevelCommune {
			parent := unitByCode(units, in.ParentCode)
			selectable = selectable && parent != nil && parent.Selectable
		}
		result = managedUnitView(store.ManagedUnit{Unit: base, Active: in.Active, Selectable: selectable, Revision: e.Revision})
		return nil
	})
	return result, replayed, err
}

// Called with the tenant import lock held. Single-item and spreadsheet creation
// share these rules and the exact same source/projection/outbox operation.
func (s *Service) insertManagedUnit(ctx context.Context, tx pgx.Tx, p authz.Principal, units []store.ManagedUnit, in ManagedUnitInput, idem string) (store.ManagedUnit, error) {
	if err := domain.ValidateManagedUnit(in.Name, in.Level, in.ParentCode); err != nil {
		if errors.Is(err, domain.ErrManagedUnitProvince) {
			return store.ManagedUnit{}, problem.Validation.WithField("parent_code", "Chọn tỉnh / thành phố")
		}
		return store.ManagedUnit{}, asProblem(err)
	}
	if err := managedUnitParent(units, in); err != nil {
		return store.ManagedUnit{}, err
	}
	if !unitNameAvailable(units, in, "") {
		return store.ManagedUnit{}, ErrUnitNameTaken
	}
	prefix := "P-"
	if in.Level == partyport.LevelCommune {
		prefix = "X-"
	}
	// Vault và đồng bộ thiết bị nhận area_code tối đa 32 ký tự. Base32 giữ
	// đủ 128 bit UUIDv7 nhưng mã có tiền tố chỉ dài 28, dùng được xuyên module.
	ref := ids.New()
	code := prefix + base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(ref[:])
	base := domain.Unit{Code: code, Name: in.Name, Level: in.Level, ParentCode: in.ParentCode, ValidFrom: domain.DayOf(s.Clock.Now())}
	if err := store.InsertUnit(ctx, tx, p.TenantID, base, "AH-271 operational catalog", p.ActorRef); err != nil {
		return store.ManagedUnit{}, err
	}
	e := store.UnitProfileEvent{ID: ids.New(), TenantID: p.TenantID, ActorRef: p.ActorRef, Code: code, Name: in.Name, Active: in.Active, UnitValidFrom: base.ValidFrom, Action: "created", EventTime: s.Clock.Now(), IdemKey: idem}
	if err := store.InsertUnitProfile(ctx, tx, &e); err != nil {
		return store.ManagedUnit{}, err
	}
	if err := store.ProjectUnitProfile(ctx, tx, e); err != nil {
		return store.ManagedUnit{}, err
	}
	if err := enqueueUnitChanged(ctx, tx, p.TenantID, base, "created"); err != nil {
		return store.ManagedUnit{}, err
	}
	return store.ManagedUnit{Unit: base, Active: in.Active, Selectable: in.Active, Revision: e.Revision}, nil
}
