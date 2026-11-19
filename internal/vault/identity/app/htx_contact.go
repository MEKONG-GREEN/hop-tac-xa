package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/vault/identity/client"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/vault/identity/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/vault/identity/store"
)

var errHTXContactReplay = errors.New("vault: HTX contact command already stored")

// Không suy rộng vai nghiệp vụ thành vault_registrar: chỉ API này được quyền
// hẹp theo cooperative_ref trong token. Member không thể yêu cầu dữ liệu rõ.
func htxContactCaller(ctx context.Context, purpose client.Purpose, ref uuid.UUID, write, masked bool) (authz.Principal, error) {
	p, err := authz.Require(ctx, authz.RolePlatformAdmin, "coop_manager", "htx_member")
	if err != nil {
		return authz.Principal{}, err
	}
	if ref == uuid.Nil {
		return authz.Principal{}, problem.Validation.WithField("ref", "Cần mã HTX")
	}
	admin := p.Has(authz.RolePlatformAdmin)
	if !admin && (p.OrgRef == uuid.Nil || p.OrgRef != ref) {
		return authz.Principal{}, client.ProblemNotFound
	}
	if (write || !masked) && !admin && !p.Has("coop_manager") {
		return authz.Principal{}, problem.Forbidden
	}
	expected := client.PurposeRegistryAdmin
	if masked && !write {
		expected = client.PurposeContact
	}
	if purpose == "" {
		return authz.Principal{}, client.ProblemPurposeRequired
	}
	if purpose != expected {
		return authz.Principal{}, problem.Validation.WithField("purpose", "Mục đích không phù hợp với liên hệ HTX")
	}
	return p, nil
}

func htxContactAAD(use string, tid, ref, command uuid.UUID) []byte {
	return domain.AAD("htx-contact:"+use+":"+ref.String(), tid, command)
}

func (s *Service) openHTXContact(ctx context.Context, tid uuid.UUID, row store.HTXContactCommand) (client.HTXContact, error) {
	key, err := s.KMS.Unwrap(ctx, row.KEKID, row.WrappedKey, htxContactAAD("key", tid, row.CooperativeRef, row.CommandRef))
	if err != nil {
		return client.HTXContact{}, err
	}
	defer clear(key)
	plain, err := openField(key, htxContactAAD("payload", tid, row.CooperativeRef, row.CommandRef), row.PayloadCT)
	if err != nil {
		return client.HTXContact{}, err
	}
	var out client.HTXContact
	if err := json.Unmarshal([]byte(plain), &out); err != nil {
		return client.HTXContact{}, err
	}
	return out, nil
}

func (s *Service) ReadHTXContact(ctx context.Context, purpose client.Purpose, ref uuid.UUID, masked bool) (client.HTXContact, error) {
	p, err := htxContactCaller(ctx, purpose, ref, false, masked)
	if err != nil {
		return client.HTXContact{}, err
	}
	op := domain.OpResolve
	if masked {
		op = domain.OpDisplay
	}
	// Đây là liên hệ FN-03 theo HTX, không mở định danh đầy đủ hoặc DEK.
	// Giữ quyền Đại diện theo ma trận HTX; MFA của Admin kiểm tại Admin API.
	var row *store.HTXContactCommand
	if err := tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error { var e error; row, e = store.HTXContactCurrent(ctx, tx, ref); return e }); err != nil {
		return client.HTXContact{}, err
	}
	if row == nil {
		if err := s.logAccessTx(ctx, p, ref, op, purpose, domain.OutcomeNotFound); err != nil {
			return client.HTXContact{}, err
		}
		return client.HTXContact{}, client.ProblemNotFound
	}
	out, err := s.openHTXContact(ctx, p.TenantID, *row)
	if err != nil {
		return client.HTXContact{}, err
	}
	if masked {
		out.FullName = domain.Label(out.FullName)
		out.Phone = domain.MaskPhone(out.Phone)
		out.Masked = true
	}
	if err := s.logAccessTx(ctx, p, ref, op, purpose, domain.OutcomeOK); err != nil {
		return client.HTXContact{}, err
	}
	return out, nil
}

func (s *Service) replayHTXContact(ctx context.Context, p authz.Principal, ref, command uuid.UUID, in client.HTXContact) (bool, error) {
	var row *store.HTXContactCommand
	if err := tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error { var e error; row, e = store.HTXContactCommandByRef(ctx, tx, command); return e }); err != nil {
		return false, err
	}
	if row == nil {
		return false, nil
	}
	if row.CooperativeRef != ref {
		return false, problem.IdempotencyConflict
	}
	old, err := s.openHTXContact(ctx, p.TenantID, *row)
	if err != nil {
		return false, err
	}
	if old.FullName != in.FullName || old.Phone != in.Phone {
		return false, problem.IdempotencyConflict
	}
	return true, nil
}

func (s *Service) WriteHTXContact(ctx context.Context, purpose client.Purpose, ref, command uuid.UUID, in client.HTXContact) error {
	p, err := htxContactCaller(ctx, purpose, ref, true, false)
	if err != nil {
		return err
	}
	if command == uuid.Nil || command.Version() != 7 {
		return problem.Validation.WithField("command", "Cần UUIDv7 của thao tác")
	}
	if in.Masked {
		return problem.Validation.WithField("contact", "Không ghi liên hệ đã che")
	}
	in.FullName = domain.NormalizeName(in.FullName)
	if utf8.RuneCountInString(in.FullName) > 200 {
		return problem.Validation.WithField("full_name", "Tối đa 200 ký tự")
	}
	// Hợp đồng FN-03 nhận đúng số địa phương 10 chữ số; bản mã dùng dạng chuẩn.
	localPhone := strings.TrimSpace(in.Phone)
	if len(localPhone) != 10 || localPhone[0] != '0' {
		return problem.Validation.WithField("phone", "SĐT gồm 10 chữ số, bắt đầu bằng 0")
	}
	if in.Phone, err = domain.NormalizePhone(localPhone); err != nil {
		return problem.Validation.WithField("phone", err.Error())
	}
	if replay, err := s.replayHTXContact(ctx, p, ref, command, in); err != nil || replay {
		return err
	}
	key := make([]byte, 32)
	if _, err := io.ReadFull(s.rand(), key); err != nil {
		return err
	}
	defer clear(key)
	plain, err := json.Marshal(in)
	if err != nil {
		return err
	}
	defer clear(plain)
	row := store.HTXContactCommand{CommandRef: command, CooperativeRef: ref, KEKID: s.KMS.KeyID()}
	row.PayloadCT, err = sealField(s.rand(), key, htxContactAAD("payload", p.TenantID, ref, command), string(plain))
	if err != nil {
		return err
	}
	row.WrappedKey, err = s.KMS.Wrap(ctx, key, htxContactAAD("key", p.TenantID, ref, command))
	if err != nil {
		return err
	}
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		// Command được khóa trước scope: không thể dùng một command cho hai HTX.
		if err := store.LockHTXContactCommand(ctx, tx, command); err != nil {
			return err
		}
		if old, err := store.HTXContactCommandByRef(ctx, tx, command); err != nil {
			return err
		} else if old != nil {
			return errHTXContactReplay
		}
		if err := store.LockHTXContact(ctx, tx, ref); err != nil {
			return err
		}
		if err := store.ApplyHTXContact(ctx, tx, row); err != nil {
			return err
		}
		return s.logAccess(ctx, tx, p, &ref, true, domain.OpAmend, purpose, domain.OutcomeOK)
	})
	if errors.Is(err, errHTXContactReplay) {
		_, err = s.replayHTXContact(ctx, p, ref, command, in)
	}
	return err
}
