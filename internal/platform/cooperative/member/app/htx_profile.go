package app

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/member/store"
	partyport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/party/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
)

var _ partyport.HTXProfileEditor = (*Service)(nil)

// ApplyHTXProfile ghi địa chỉ và sổ bù tại owner, không gọi lại party hay Vault
// trong transaction. Nút/danh mục/người đại diện do caller kiểm trước.
func (s *Service) ApplyHTXProfile(ctx context.Context, patch partyport.HTXProfilePatch) error {
	tid, err := tenant.FromContext(ctx)
	if err != nil {
		return err
	}
	return tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		profile, err := store.ProfileForUpdate(ctx, tx, patch.Ref)
		if err != nil {
			return err
		}
		if profile == nil {
			return ErrDependency.WithDetail("hồ sơ HTX chưa được khởi tạo")
		}
		previous, err := store.ProfileEventByIdem(ctx, tx, patch.IdemKey)
		if err != nil {
			return err
		}
		if previous != nil {
			var receipt struct {
				Command string `json:"source_org_event"`
				Address string `json:"head_office"`
			}
			if err := json.Unmarshal(previous.After, &receipt); err != nil {
				return err
			}
			if previous.SubjectRef != patch.Ref || receipt.Command != patch.CommandRef.String() || receipt.Address != patch.HeadOffice {
				return problem.IdempotencyConflict
			}
			return nil
		}
		// Chụp các trường gốc của hồ sơ, không sao chép PII hoặc tổng diện tích hộ.
		before := map[string]any{"head_office": profile.Address, "tax_code": profile.TaxCode, "area_ha": profile.AreaHa,
			"established_on": profile.EstablishedOn, "lat": profile.Lat, "lon": profile.Lon}
		old, err := json.Marshal(before)
		if err != nil {
			return err
		}
		before["head_office"], before["source_org_event"] = patch.HeadOffice, patch.CommandRef.String()
		after, err := json.Marshal(before)
		if err != nil {
			return err
		}
		event := store.ProfileEvent{ID: ids.New(), TenantID: tid, SubjectKind: store.ProfileCooperative, SubjectRef: patch.Ref,
			CommuneCode: patch.CommuneCode, Action: "updated", Before: old, After: after,
			ActorRef: patch.ActorRef, EventTime: patch.EventTime, IdemKey: patch.IdemKey}
		if err := store.InsertProfileEvent(ctx, tx, &event); err != nil {
			return err
		}
		return store.UpdateProfileAddress(ctx, tx, patch.Ref, patch.HeadOffice)
	})
}
