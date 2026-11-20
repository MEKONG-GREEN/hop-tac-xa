package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/masterdata/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/masterdata/store"
	partyport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/party/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tabular"
)

var unitImportAliases = map[string]string{"ten": "name", "cap": "level", "tinh_thanh_pho_truc_thuoc": "parent"}

var ErrUnitImportInvalid = problem.New(http.StatusUnprocessableEntity, "masterdata.admin_unit_import_invalid", "Tệp không đúng mẫu đơn vị hành chính, vui lòng tải mẫu chuẩn và thử lại")

// ImportManagedUnits accepts the FN-02 spreadsheet in file order. Business
// errors reject individual rows. Any storage error rolls back the whole batch,
// including its durable result; retrying the same operation cannot add twice.
func (s *Service) ImportManagedUnits(ctx context.Context, body io.Reader, key string) (CatalogImportResult, error) {
	p, err := authz.Require(ctx, authz.RolePlatformAdmin)
	if err != nil {
		return CatalogImportResult{}, err
	}
	if key == "" {
		return CatalogImportResult{}, problem.IdempotencyRequired
	}
	limits := tabular.Limits{MaxBytes: MaxCatalogImportBytes, MaxRows: MaxCatalogImportRows}
	raw, err := io.ReadAll(io.LimitReader(body, limits.MaxBytes+1))
	if err != nil {
		return CatalogImportResult{}, err
	}
	table, err := tabular.ReadBytes(raw, limits)
	if err != nil {
		return CatalogImportResult{}, ErrUnitImportInvalid.WithDetail(tabular.Message(err, limits))
	}
	cols := table.Columns(unitImportAliases)
	if len(table.Header) != 3 || len(tabular.Missing(cols, "name", "level", "parent")) > 0 {
		return CatalogImportResult{}, ErrUnitImportInvalid.WithDetail("Dùng ba cột: Tên, Cấp, Tỉnh / thành phố trực thuộc")
	}
	hash := sha256.Sum256(raw)
	idem := "admin-unit:import:" + p.ActorRef.String() + ":" + key
	result := CatalogImportResult{ID: ids.New(), Rows: []CatalogImportRow{}}
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		if err := store.LockImport(ctx, tx, p.TenantID); err != nil {
			return err
		}
		old, err := store.CatalogImportByKey(ctx, tx, idem)
		if err != nil {
			return err
		}
		if old != nil {
			if !bytes.Equal(old.FileSHA256, hash[:]) {
				return problem.IdempotencyConflict
			}
			result, err = fromStore(old)
			result.Replayed = true
			return err
		}
		units, err := store.ManagedUnits(ctx, tx, domain.DayOf(s.Clock.Now()), nil)
		if err != nil {
			return err
		}
		for _, record := range table.Rows {
			row := CatalogImportRow{Row: record.Line, Catalog: "admin_unit", Status: ImportRowRejected}
			in := ManagedUnitInput{Name: domain.NormalizeUnitName(record.Get(cols, "name")), Active: true}
			switch domain.UnitNameKey(record.Get(cols, "level")) {
			case "tỉnh", "tinh":
				in.Level = partyport.LevelProvince
			case "xã", "xa":
				in.Level = partyport.LevelCommune
			default:
				row.Reason = "Cấp phải là Tỉnh hoặc Xã"
			}
			parentName := domain.UnitNameKey(record.Get(cols, "parent"))
			if row.Reason == "" && in.Level == partyport.LevelCommune {
				for _, u := range units {
					if u.Unit.Level == partyport.LevelProvince && u.Selectable && domain.UnitNameKey(u.Unit.Name) == parentName {
						in.ParentCode = u.Unit.Code
						break
					}
				}
				if in.ParentCode == "" {
					row.Reason = "Chọn tỉnh / thành phố đang sử dụng, đã có trước dòng này"
				}
			} else if row.Reason == "" && parentName != "" {
				row.Reason = "Cấp Tỉnh không có đơn vị cấp trên"
			}
			if row.Reason == "" {
				if err := domain.ValidateManagedUnit(in.Name, in.Level, in.ParentCode); err != nil {
					row.Reason = importReason(err)
				}
			}
			if row.Reason == "" && !unitNameAvailable(units, in, "") {
				row.Reason = ErrUnitNameTaken.Title
			}
			if row.Reason != "" {
				result.add(row)
				continue
			}
			created, err := s.insertManagedUnit(ctx, tx, p, units, in, idem+":row:"+strconv.Itoa(record.Line))
			if err != nil {
				return err
			}
			units = append(units, created)
			row.Code = created.Unit.Code
			row.Status = ImportRowCreated
			result.add(row)
		}
		rows, err := json.Marshal(result.Rows)
		if err != nil {
			return err
		}
		return store.InsertCatalogImport(ctx, tx, p.TenantID, store.CatalogImport{ID: result.ID, ActorRef: p.ActorRef, Format: table.Format, FileSHA256: hash[:], Total: result.Total, Created: result.Created, Skipped: result.Skipped, Rejected: result.Rejected, Rows: rows, IdemKey: idem, EventTime: s.Clock.Now()})
	})
	return result, err
}
