package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/party/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/party/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/party/store"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/vault/identity/client"
)

var (
	ErrHTXProfilePending = problem.New(http.StatusConflict, "party.cooperative_profile_pending", "Hồ sơ đang được lưu, hãy thử lại thao tác trước")
	ErrHTXProfileChanged = problem.New(http.StatusConflict, "party.cooperative_profile_changed", "Hồ sơ đã thay đổi, tải lại trước khi lưu")
)

type UpdateHTXProfileInput struct {
	HTXProfileInput
	Ref              uuid.UUID
	ExpectedRevision int64
}

func htxProfileHash(in UpdateHTXProfileInput) (string, error) {
	// Chỉ giữ digest như receipt HTTP; tuyệt đối không ghi JSON/PII này vào PF.
	body, err := json.Marshal(in)
	if err != nil {
		return "", err
	}
	defer clear(body)
	hash := sha256.Sum256(body)
	return hex.EncodeToString(hash[:]), nil
}

func (r *Registry) profilePending(ctx context.Context, tid, ref uuid.UUID) (bool, error) {
	var pending bool
	err := tenant.WithTenantTx(ctx, r.DB, tid, func(tx pgx.Tx) error {
		var err error
		pending, err = store.HTXProfilePending(ctx, tx, ref)
		return err
	})
	return pending, err
}

// UpdateHTXProfile ghi ý định và Nút trong một transaction của party; sau đó
// Vault và cooperative áp command lũy đẳng. Chỉ khi cả hai owner xong mới ghi
// receipt hoàn tất. Ý định chưa xong chặn thao tác mới trên cùng HTX.
func (r *Registry) UpdateHTXProfile(ctx context.Context, in UpdateHTXProfileInput) (OrgResult, error) {
	p, err := htxProfileCaller(ctx, in.Ref, true)
	if err != nil {
		return OrgResult{}, err
	}
	if in.IdemKey == "" {
		return OrgResult{}, problem.IdempotencyRequired
	}
	if in.ExpectedRevision < 1 {
		return OrgResult{}, problem.Validation.WithField("expected_revision", "Cần phiên bản hồ sơ đã tải")
	}
	if err := validateHTXProfile(&in.HTXProfileInput); err != nil {
		return OrgResult{}, err
	}
	digest, err := htxProfileHash(in)
	if err != nil {
		return OrgResult{}, err
	}
	key := "htx-profile:update:" + p.ActorRef.String() + ":" + in.Ref.String() + ":" + in.IdemKey
	if previous, err := r.replay(ctx, p.TenantID, key); err != nil {
		return OrgResult{}, err
	} else if previous != nil {
		return r.finishHTXProfile(ctx, p, *previous, in, digest)
	}
	if pending, err := r.profilePending(ctx, p.TenantID, in.Ref); err != nil {
		return OrgResult{}, err
	} else if pending {
		return OrgResult{}, ErrHTXProfilePending
	}
	current, err := r.GetCooperative(ctx, in.Ref)
	if err != nil {
		return OrgResult{}, err
	}
	if current.LastSeq != in.ExpectedRevision {
		return OrgResult{}, ErrHTXProfileChanged
	}
	if !current.Active {
		return OrgResult{}, ErrOrgState.WithDetail("HTX đã ngừng hoạt động")
	}
	if r.ProfileEditor == nil {
		return OrgResult{}, ErrDependency.WithDetail("chưa nối thao tác sửa hồ sơ của HTX")
	}
	unit, err := r.unitInfo(ctx, port.OrgCooperative, in.CommuneCode, "commune_code")
	if err != nil {
		return OrgResult{}, err
	}
	if unit.Disabled && in.CommuneCode != current.AdminUnitCode {
		return OrgResult{}, problem.Validation.WithField("commune_code", "Đơn vị hành chính đã ngừng sử dụng")
	}
	// Kiểm tên trước Vault; kiểm lại dưới khóa khi ghi ý định.
	if err := tenant.WithTenantTx(ctx, r.DB, p.TenantID, func(tx pgx.Tx) error {
		owner, err := store.CooperativeNameOwner(ctx, tx, in.Name, in.Ref)
		if err == nil && owner != uuid.Nil {
			return ErrCoopNameTaken
		}
		return err
	}); err != nil {
		return OrgResult{}, err
	}
	contact, amend, err := r.prepareHTXContact(ctx, current.RepresentativeRef, in.HTXProfileInput)
	if err != nil {
		return OrgResult{}, err
	}
	result, err := r.write(ctx, p.TenantID, key, func(tx pgx.Tx) (OrgResult, error) {
		cur, err := store.OrgByRef(ctx, tx, in.Ref, true)
		if err != nil {
			return OrgResult{}, err
		}
		if cur == nil || cur.Kind != port.OrgCooperative {
			return OrgResult{}, ErrOrgNotFound
		}
		if cur.LastSeq != in.ExpectedRevision {
			return OrgResult{}, ErrHTXProfileChanged
		}
		if pending, err := store.HTXProfilePending(ctx, tx, in.Ref); err != nil {
			return OrgResult{}, err
		} else if pending {
			return OrgResult{}, ErrHTXProfilePending
		}
		if err := store.LockCooperativeName(ctx, tx, in.Name); err != nil {
			return OrgResult{}, err
		}
		owner, err := store.CooperativeNameOwner(ctx, tx, in.Name, in.Ref)
		if err != nil {
			return OrgResult{}, err
		}
		if owner != uuid.Nil {
			return OrgResult{}, ErrCoopNameTaken
		}
		org := cur.Org
		org.Name, org.AdminUnitCode, org.RepresentativeRef = in.Name, in.CommuneCode, contact
		// HTX là đơn vị ngang hàng. Khi đổi địa bàn của hồ sơ cũ, tách khỏi
		// cha KN cũ để không giữ một cây có xã thuộc sai tỉnh.
		if org.AdminUnitCode != cur.AdminUnitCode {
			org.ParentRef = uuid.Nil
		}
		event := newEvent(p, org, domain.ActionProfileUpdated, r.now(), key)
		event.PrevAdminUnit, event.PrevParentRef = cur.AdminUnitCode, cur.ParentRef
		event.Details = map[string]any{"request_sha256": digest, "head_office": in.HeadOffice, "contact_amend": amend,
			"previous_name": cur.Name, "previous_representative_ref": cur.RepresentativeRef.String()}
		if err := store.InsertOrgEvent(ctx, tx, &event); err != nil {
			return OrgResult{}, err
		}
		if err := store.UpdateOrg(ctx, tx, org, cur.StatusReason, event); err != nil {
			return OrgResult{}, err
		}
		if org.AdminUnitCode != cur.AdminUnitCode {
			moved := event
			moved.Action = domain.ActionMoved
			moved.Reason = "Cập nhật địa chỉ hồ sơ HTX"
			if err := enqueueStatusEvent(ctx, tx, moved, 0); err != nil {
				return OrgResult{}, err
			}
		}
		return OrgResult{Org: org, Event: event}, nil
	})
	if err != nil {
		return OrgResult{}, err
	}
	return r.finishHTXProfile(ctx, p, result, in, digest)
}

func (r *Registry) prepareHTXContact(ctx context.Context, ref uuid.UUID, in HTXProfileInput) (uuid.UUID, bool, error) {
	if r.Vault == nil {
		return uuid.Nil, false, ErrDependency.WithDetail("chưa nối kho định danh")
	}
	var identity client.Identity
	var err error
	if ref != uuid.Nil {
		identity, err = r.Vault.Resolve(ctx, client.PurposeRegistryAdmin, ref)
		if err != nil {
			return uuid.Nil, false, vaultError(err)
		}
	}
	if ref == uuid.Nil || localHTXPhone(identity.Phone) != in.Phone {
		created, err := r.Vault.Create(ctx, client.PurposeRegistryAdmin, client.Identity{Kind: client.SubjectPerson,
			FullName: in.RepresentativeName, Phone: in.Phone, AreaCode: in.CommuneCode})
		if err != nil && (!errors.Is(err, client.ErrDuplicate) || created.Ref == uuid.Nil) {
			return uuid.Nil, false, vaultError(err)
		}
		ref = created.Ref
		identity, err = r.Vault.Resolve(ctx, client.PurposeRegistryAdmin, ref)
		if err != nil {
			return uuid.Nil, false, vaultError(err)
		}
	}
	amend := identity.FullName != in.RepresentativeName || localHTXPhone(identity.Phone) != in.Phone
	if amend {
		contacts, ok := client.AsRegistryContacts(r.Vault)
		if !ok {
			return uuid.Nil, false, ErrDependency.WithDetail("chưa nối thao tác sửa liên hệ HTX")
		}
		if err := contacts.ValidateRegistryContact(ctx, client.PurposeRegistryAdmin, ref); err != nil {
			return uuid.Nil, false, vaultError(err)
		}
	}
	return ref, amend, nil
}

func (r *Registry) finishHTXProfile(ctx context.Context, p authz.Principal, result OrgResult, in UpdateHTXProfileInput, digest string) (OrgResult, error) {
	event := result.Event
	if event.Action != domain.ActionProfileUpdated || event.Org.Ref != in.Ref || event.Details["request_sha256"] != digest {
		return OrgResult{}, problem.IdempotencyConflict
	}
	var done bool
	if err := tenant.WithTenantTx(ctx, r.DB, p.TenantID, func(tx pgx.Tx) error {
		var err error
		done, err = store.HTXProfileCompleted(ctx, tx, event.ID)
		return err
	}); err != nil {
		return OrgResult{}, err
	}
	if done {
		return result, nil
	}
	if event.Details["contact_amend"] == true {
		contacts, ok := client.AsRegistryContacts(r.Vault)
		if !ok {
			return OrgResult{}, ErrDependency.WithDetail("chưa nối thao tác sửa liên hệ HTX")
		}
		if _, err := contacts.AmendRegistryContact(ctx, client.PurposeRegistryAdmin, event.Org.RepresentativeRef, event.ID,
			client.Identity{FullName: in.RepresentativeName, Phone: in.Phone}); err != nil {
			return OrgResult{}, ErrDependency.WithDetail("chưa hoàn tất liên hệ HTX, thử lại cùng thao tác").WithCause(err)
		}
	}
	if r.ProfileEditor == nil {
		return OrgResult{}, ErrDependency.WithDetail("chưa nối thao tác sửa hồ sơ HTX")
	}
	address, ok := event.Details["head_office"].(string)
	if !ok {
		return OrgResult{}, ErrDependency.WithDetail("ý định sửa hồ sơ không đủ dữ liệu")
	}
	if err := r.ProfileEditor.ApplyHTXProfile(ctx, port.HTXProfilePatch{
		Ref: event.Org.Ref, CommandRef: event.ID, CommuneCode: event.Org.AdminUnitCode, HeadOffice: address,
		ActorRef: event.ActorRef, EventTime: event.EventTime, IdemKey: event.IdemKey + ":address",
	}); err != nil {
		return OrgResult{}, ErrDependency.WithDetail("chưa hoàn tất hồ sơ HTX, thử lại cùng thao tác").WithCause(err)
	}
	if err := tenant.WithTenantTx(ctx, r.DB, p.TenantID, func(tx pgx.Tx) error {
		return store.CompleteHTXProfile(ctx, tx, event.Org.Ref, event.ID)
	}); err != nil {
		return OrgResult{}, err
	}
	return result, nil
}
