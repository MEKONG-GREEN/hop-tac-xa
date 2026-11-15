package app

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/masterdata/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/masterdata/store"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
)

// Luật của loại cây (crop không có parent_code) và giống (rice_variety) — AH-2. Đặt trong service nên áp dụng cho
// mọi nơi gọi (màn mới, màn Danh mục dùng chung cũ), không phụ thuộc client.

// Kết quả thao tác xóa: mục chưa được dùng thì đã xóa; mục đang được dùng thì chỉ ẩn (BRD FN-01 BR-02, EX-03).
const (
	OutcomeDeleted = "deleted"
	OutcomeHidden  = "hidden"
)

// Lý do hệ thống ghi khi Admin ẩn/xóa không nhập lý do (giữ nguyên ràng buộc DB: ẩn thì phải có lý do).
const (
	systemHideReason   = "Ẩn tại màn Danh mục cây trồng & giống"
	systemDeleteReason = "Đã xóa tại màn Danh mục cây trồng & giống"
)

// DeleteResult là kết quả DeleteItem.
type DeleteResult struct {
	Outcome string
	Item    domain.Item
}

func cropVariety(c string) bool { return c == port.CatalogCrop || c == port.CatalogRiceVariety }

// legacyAttrs là thuộc tính màn mới không gửi lên: khi sửa phải giữ nguyên (vd năng suất cũ của giống).
var legacyAttrs = map[string][]string{
	port.CatalogRiceVariety: {"expected_yield_t_per_ha", "description"},
	port.CatalogCrop:        {port.AttrIsRice, "is_group", "unit", "description"},
}

// scopeOf là phạm vi khóa của mục: loại cây "crop:<mã>", giống "crop:<mã loại cây>".
func scopeOf(it domain.Item) string {
	if it.Catalog == port.CatalogRiceVariety {
		return "crop:" + it.Attrs[port.AttrCropCode]
	}
	return "crop:" + it.Code
}

// checkCropShape: loại cây không có nhóm cha và phải có cờ "là lúa".
func checkCropShape(attrs map[string]string) error {
	if _, has := attrs[port.AttrParentCode]; has {
		return problem.Validation.WithField("attrs."+port.AttrParentCode, "Loại cây không có nhóm cha")
	}
	if _, has := attrs[port.AttrIsRice]; !has {
		return problem.Validation.WithField("attrs."+port.AttrIsRice, "bắt buộc")
	}
	return nil
}

// prepareCreate: mã do hệ thống sinh (UUIDv7), client không được gửi; kiểm dạng của loại cây.
func (s *Service) prepareCreate(in domain.Item) (domain.Item, error) {
	if in.Code != "" {
		return in, problem.Validation.WithField("code", "Mã do hệ thống sinh — bỏ trống")
	}
	in.Code = ids.New().String()
	if in.Catalog == port.CatalogCrop {
		if err := checkCropShape(in.Attrs); err != nil {
			return in, err
		}
	}
	return in, nil
}

// carryLegacyAttrs giữ thuộc tính cũ mà request sửa không gửi.
func carryLegacyAttrs(upd, cur domain.Item) domain.Item {
	attrs := make(map[string]string, len(upd.Attrs))
	for k, v := range upd.Attrs {
		attrs[k] = v
	}
	for _, k := range legacyAttrs[upd.Catalog] {
		if _, sent := attrs[k]; !sent {
			if v, had := cur.Attrs[k]; had {
				attrs[k] = v
			}
		}
	}
	upd.Attrs = attrs
	return upd
}

// guardWritable: mục đã xóa coi như không có; mục cũ có nhóm cha không còn là loại cây — chỉ xem.
func guardWritable(cur domain.Item) error {
	if cur.Deleted {
		return ErrItemNotFound
	}
	if cur.Catalog == port.CatalogCrop && !domain.IsCropType(cur) {
		return ErrCatalogItemReadOnly
	}
	return nil
}

// lockScope khóa phạm vi của mục (loại cây, giống) trong tx.
func (s *Service) lockScope(ctx context.Context, tx pgx.Tx, tid uuid.UUID, it domain.Item) error {
	return store.LockCatalogScope(ctx, tx, tid, scopeOf(it))
}

// nameTaken kiểm trùng tên: loại cây trong các loại cây (kể cả đã ẩn, không tính mục cũ có cha); giống trong cùng
// loại cây (kể cả đã ẩn); mục đã xóa không tham gia. Danh mục khác giữ cách cũ.
func (s *Service) nameTaken(ctx context.Context, tx pgx.Tx, tid uuid.UUID, it domain.Item) error {
	var existing []string
	var err error
	switch it.Catalog {
	case port.CatalogCrop:
		if err = store.LockCatalogScope(ctx, tx, tid, "crop"); err == nil {
			existing, err = store.CropTypeNames(ctx, tx, it.Code)
		}
	case port.CatalogRiceVariety:
		if err = store.LockCatalogScope(ctx, tx, tid, scopeOf(it)); err == nil {
			existing, err = store.VarietyNames(ctx, tx, it.Attrs[port.AttrCropCode], it.Code)
		}
	default:
		taken, e := store.ItemNameTaken(ctx, tx, it.Catalog, it.Name, it.Code)
		if e != nil {
			return e
		}
		if taken {
			return ErrItemNameTaken
		}
		return nil
	}
	if err != nil {
		return err
	}
	want := domain.NameKey(it.Name)
	for _, n := range existing {
		if domain.NameKey(n) == want {
			return ErrItemNameTaken.WithField("name", "Tên đã tồn tại")
		}
	}
	return nil
}

// checkVarietyCrop: loại cây của giống phải tồn tại, chưa xóa, chưa ẩn và là loại cây (không có nhóm cha).
func (s *Service) checkVarietyCrop(ctx context.Context, tx pgx.Tx, code string) error {
	cr, err := store.Item(ctx, tx, port.CatalogCrop, code, false)
	if err != nil {
		return err
	}
	if cr == nil || cr.Deleted || !cr.Active || !domain.IsCropType(*cr) {
		return ErrCatalogRef.WithField("attrs."+port.AttrCropCode, "Loại cây không tồn tại hoặc đang ẩn")
	}
	return nil
}

// checkVarietyCropShown: không cho hiện lại giống khi loại cây của nó đang ẩn (chạy trước checkRefs).
func (s *Service) checkVarietyCropShown(ctx context.Context, tx pgx.Tx, v domain.Item) error {
	cr, err := store.Item(ctx, tx, port.CatalogCrop, v.Attrs[port.AttrCropCode], false)
	if err != nil {
		return err
	}
	if cr != nil && !cr.Deleted && !cr.Active {
		return ErrCropTypeHidden
	}
	return nil
}

// checkHideable: loại cây còn giống đang hiển thị thì không ẩn/xóa được (gọi sau khi đã khóa phạm vi).
func (s *Service) checkHideable(ctx context.Context, tx pgx.Tx, cur domain.Item) error {
	if cur.Catalog != port.CatalogCrop {
		return nil
	}
	n, err := store.CountVarieties(ctx, tx, cur.Code, true)
	if err != nil {
		return err
	}
	if n > 0 {
		return ErrCropTypeHasVisibleVarieties.WithDetail(fmt.Sprintf("Loại cây còn %d giống đang hiển thị. Ẩn các giống đó trước.", n))
	}
	return nil
}

// itemInUse hỏi các module khác (vụ, SOP, sản lượng, thẻ tài liệu, mẫu báo cáo) mục có đang được dùng không.
// Gọi NGOÀI giao dịch (không gọi port module khác trong tx). Thiếu nguồn đếm thì coi là đang được dùng (fail-closed).
func (s *Service) itemInUse(ctx context.Context, catalog, code string) (bool, error) {
	if len(s.Usage) == 0 || s.Forms == nil {
		return true, nil
	}
	for _, u := range s.Usage {
		used, err := u.CatalogItemInUse(ctx, catalog, code)
		if err != nil {
			return false, ErrDependency.WithCause(err)
		}
		if used {
			return true, nil
		}
	}
	m, err := s.Forms.FormsUsing(ctx, catalog, []string{code})
	if err != nil {
		return false, ErrDependency.WithCause(err)
	}
	return m[code] > 0, nil
}

// DeleteItem xóa loại cây hoặc giống: chưa được dùng ở đâu thì đánh dấu đã xóa (biến khỏi mọi danh sách, đồng bộ
// phát tombstone, bản ghi và lịch sử giữ lại); đang được dùng thì chỉ ẩn. Gọi lặp cùng Idempotency-Key trả lại
// kết quả cũ, replayed = true.
func (s *Service) DeleteItem(ctx context.Context, catalog, code, key string) (DeleteResult, bool, error) {
	p, err := authz.Require(ctx, port.RoleMasterdataAdmin)
	if err != nil {
		return DeleteResult{}, false, err
	}
	if key == "" {
		return DeleteResult{}, false, problem.IdempotencyRequired
	}
	if err := s.knownCatalog(catalog); err != nil {
		return DeleteResult{}, false, err
	}
	if !cropVariety(catalog) {
		return DeleteResult{}, false, ErrDeleteUnsupported
	}
	var res DeleteResult
	replayed := false
	replay := func(ctx context.Context, tx pgx.Tx) (bool, error) {
		prev, err := store.ChangeByIdemKey(ctx, tx, key)
		if err != nil || prev == nil {
			return false, err
		}
		cur, err := store.Item(ctx, tx, prev.Catalog, prev.Code, false)
		if err != nil {
			return false, err
		}
		res = DeleteResult{Outcome: OutcomeHidden, Item: *cur}
		if prev.Action == domain.ActionDeleted {
			res.Outcome = OutcomeDeleted
		}
		replayed = true
		return true, nil
	}
	// Tx ngắn 1: phát lại, kiểm mục tồn tại và được phép ghi.
	var done bool
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		if done, e = replay(ctx, tx); e != nil || done {
			return e
		}
		cur, e := store.Item(ctx, tx, catalog, code, false)
		if e != nil {
			return e
		}
		if cur == nil {
			return ErrItemNotFound
		}
		return guardWritable(*cur)
	})
	if err != nil {
		return DeleteResult{}, false, err
	}
	if done {
		return res, true, nil
	}
	// Ngoài tx: hỏi module khác.
	inUse, err := s.itemInUse(ctx, catalog, code)
	if err != nil {
		return DeleteResult{}, false, err
	}
	now := s.Clock.Now()
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		if done, e := replay(ctx, tx); e != nil || done {
			return e
		}
		cur, e := store.Item(ctx, tx, catalog, code, true)
		if e != nil {
			return e
		}
		if cur == nil {
			return ErrItemNotFound
		}
		if e := guardWritable(*cur); e != nil {
			return e
		}
		if e := s.lockScope(ctx, tx, p.TenantID, *cur); e != nil {
			return e
		}
		if e := s.checkHideable(ctx, tx, *cur); e != nil {
			return e
		}
		if catalog == port.CatalogCrop && !inUse {
			if n, e := store.CountVarieties(ctx, tx, cur.Code, false); e != nil {
				return e
			} else if n > 0 {
				inUse = true
			} else if pest, e := store.PestsAffectingCrop(ctx, tx, cur.Code); e != nil {
				return e
			} else if pest {
				inUse = true
			}
		}
		if inUse && !cur.Active {
			res = DeleteResult{Outcome: OutcomeHidden, Item: *cur} // đã ẩn sẵn: không ghi sổ
			return nil
		}
		next := *cur
		next.Active, next.Revision, next.UpdatedBy, next.UpdatedAt = false, cur.Revision+1, p.ActorRef, now
		action, reason, outcome := domain.ActionDeleted, systemDeleteReason, OutcomeDeleted
		if inUse {
			action, reason, outcome = domain.ActionDeactivated, systemHideReason, OutcomeHidden
		} else {
			next.Deleted = true
		}
		next.DeactivationReason = reason
		before := cur.Snapshot()
		c := &domain.Change{ID: ids.New(), Catalog: catalog, Code: cur.Code, Action: action, Before: &before, After: next.Snapshot(),
			Reason: reason, EventTime: now, ActorRef: p.ActorRef, IdemKey: key}
		res = DeleteResult{Outcome: outcome, Item: next}
		return s.writeChange(ctx, tx, p.TenantID, c, next)
	})
	if err != nil {
		it, rep, e := s.replayOnDuplicate(ctx, p.TenantID, catalog, code, err)
		if e != nil {
			return DeleteResult{}, false, e
		}
		out := OutcomeHidden
		if it.Deleted {
			out = OutcomeDeleted
		}
		return DeleteResult{Outcome: out, Item: it}, rep, nil
	}
	return res, replayed, nil
}
