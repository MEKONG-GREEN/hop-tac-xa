package app

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/vault/identity/client"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/vault/identity/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/vault/identity/store"
)

var errRegistryContactReplayed = errors.New("vault: registry contact command already applied")

func validateRegistryContact(ctx context.Context, tx pgx.Tx, subject *store.Subject) error {
	contact, err := store.CreatedForPurpose(ctx, tx, subject.Ref, string(client.PurposeRegistryAdmin))
	if err != nil {
		return err
	}
	if subject.Kind != string(client.SubjectPerson) || !contact {
		return problem.Validation.WithField("purpose", "registry.admin chỉ sửa liên hệ được tạo cho quản trị hồ sơ")
	}
	return nil
}

// ValidateRegistryContact không sửa định danh: caller kiểm trước khi ghi ý định
// sửa hồ sơ HTX. Không cho dùng đường này để sửa chủ hộ hoặc nhân sự.
func (s *Service) ValidateRegistryContact(ctx context.Context, purpose client.Purpose, ref uuid.UUID) error {
	if purpose != client.PurposeRegistryAdmin {
		return problem.Validation.WithField("purpose", "Chỉ dùng registry.admin cho liên hệ HTX")
	}
	p, err := s.begin(ctx, domain.OpAmend, purpose, client.RoleRegistrar)
	if err != nil {
		return err
	}
	var result error
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		state, err := s.subjectState(ctx, tx, p, ref, domain.OpAmend, purpose, false)
		if err != nil || state.fail != nil {
			result = state.fail
			return err
		}
		result = validateRegistryContact(ctx, tx, state.subj)
		return nil
	})
	if err != nil {
		return err
	}
	return result
}

// AmendRegistryContact dùng ID sự kiện ý định của caller làm revision_ref bền
// vững. Retry sau khi HTTP receipt hết hạn vẫn không áp lại tên/SĐT cũ lên lần
// sửa mới. Chỉ nhận tên/SĐT; không sửa CCCD/địa chỉ hay loại chủ thể.
func (s *Service) AmendRegistryContact(ctx context.Context, purpose client.Purpose, ref, command uuid.UUID, in client.Identity) (uuid.UUID, error) {
	if command == uuid.Nil || command.Version() != 7 || in.Address != "" || in.NationalID != "" {
		return uuid.Nil, problem.Validation.WithField("command", "Cần UUIDv7 của thao tác và chỉ cập nhật tên/SĐT")
	}
	if in.Phone == "" {
		return uuid.Nil, problem.Validation.WithField("phone", "SĐT liên hệ HTX là bắt buộc")
	}
	if err := s.ValidateRegistryContact(ctx, purpose, ref); err != nil {
		return uuid.Nil, err
	}
	return s.amendRevision(ctx, purpose, ref, command, in, true)
}

func (s *Service) registryContactReplay(ctx context.Context, p authz.Principal, ref, command uuid.UUID, name, phone string) (bool, error) {
	var revision *store.Revision
	var identity sealedIdentity
	var result error
	err := tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		rows, err := store.Revisions(ctx, tx, []uuid.UUID{command})
		if err != nil || len(rows) == 0 {
			return err
		}
		if rows[0].SubjectRef != ref {
			return problem.IdempotencyConflict
		}
		state, err := s.subjectState(ctx, tx, p, ref, domain.OpAmend, client.PurposeRegistryAdmin, false)
		if err != nil || state.fail != nil {
			result = state.fail
			return err
		}
		revision = &rows[0]
		identity, err = fetchIdentity(ctx, tx, ref)
		return err
	})
	if err != nil || result != nil {
		if err != nil {
			return false, err
		}
		return false, result
	}
	if revision == nil {
		return false, nil
	}
	// KMS và giải mã ngoài transaction, như Resolve/Amend hiện tại.
	key, err := s.KMS.Unwrap(ctx, identity.key.KEKID, identity.key.WrappedDataKey, domain.AAD(useDataKey, p.TenantID, ref))
	if err != nil {
		return false, err
	}
	defer clear(key)
	storedName, err := openField(key, domain.AAD(useName, p.TenantID, ref), revision.NextNameCT)
	if err != nil {
		return false, err
	}
	storedPhone, err := openField(key, domain.AAD(usePhone, p.TenantID, ref), revision.NextPhoneCT)
	if err != nil {
		return false, err
	}
	if storedName != name || storedPhone != phone {
		return false, problem.IdempotencyConflict
	}
	return true, nil
}
