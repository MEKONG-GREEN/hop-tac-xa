package app

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"

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

// HTXProfile là hồ sơ FN-03; số liệu diện tích lấy từ nguồn hộ. Thông tin liên
// hệ chỉ tồn tại trong bộ nhớ sau khi kiểm vai và HTX, không đưa vào sổ nghiệp vụ.
type HTXProfile struct {
	Cooperative
	RepresentativeName string `pii:"true"`
	Phone              string `pii:"true"`
	ContactMasked      bool
	CanEdit            bool
}

type HTXProfileInput struct {
	Name               string
	CommuneCode        string
	HeadOffice         string
	RepresentativeName string `pii:"true"`
	Phone              string `pii:"true"`
	IdemKey            string
}

var htxContactPhone = regexp.MustCompile(`^0[0-9]{9}$`)

func validateHTXProfile(in *HTXProfileInput) error {
	in.Name = strings.TrimSpace(in.Name)
	in.CommuneCode = strings.TrimSpace(in.CommuneCode)
	in.HeadOffice = strings.TrimSpace(in.HeadOffice)
	in.RepresentativeName = strings.TrimSpace(in.RepresentativeName)
	in.Phone = strings.TrimSpace(in.Phone)
	if n := utf8.RuneCountInString(in.Name); n == 0 || n > 200 {
		return problem.Validation.WithField("name", "Tên HTX bắt buộc, tối đa 200 ký tự")
	}
	if in.CommuneCode == "" {
		return problem.Validation.WithField("commune_code", "Chọn xã / phường / đặc khu")
	}
	if n := utf8.RuneCountInString(in.HeadOffice); n == 0 || n > domain.MaxAddress {
		return problem.Validation.WithField("head_office", "Địa chỉ trụ sở bắt buộc, tối đa 500 ký tự")
	}
	if utf8.RuneCountInString(in.RepresentativeName) > 200 {
		return problem.Validation.WithField("representative_name", "Tối đa 200 ký tự")
	}
	if !htxContactPhone.MatchString(in.Phone) {
		return problem.Validation.WithField("phone", "SĐT gồm 10 chữ số, bắt đầu bằng 0")
	}
	return nil
}

func htxProfileCaller(ctx context.Context, ref uuid.UUID, write bool) (authz.Principal, error) {
	p, err := authz.Require(ctx, authz.RolePlatformAdmin, port.RoleCoopManager, port.RoleHTXMember)
	if err != nil {
		return authz.Principal{}, err
	}
	if !p.Has(authz.RolePlatformAdmin) && p.OrgRef == uuid.Nil {
		return authz.Principal{}, problem.NodeScopeRequired
	}
	if !p.Has(authz.RolePlatformAdmin) && p.OrgRef != ref {
		return authz.Principal{}, ErrOrgNotFound
	}
	if write && !p.Has(authz.RolePlatformAdmin) && !p.Has(port.RoleCoopManager) {
		return authz.Principal{}, problem.Forbidden
	}
	return p, nil
}

func (r *Registry) HTXProfile(ctx context.Context, ref uuid.UUID) (HTXProfile, error) {
	p, err := htxProfileCaller(ctx, ref, false)
	if err != nil {
		return HTXProfile{}, err
	}
	if pending, err := r.profilePending(ctx, p.TenantID, ref); err != nil {
		return HTXProfile{}, err
	} else if pending {
		return HTXProfile{}, ErrHTXProfilePending
	}
	c, err := r.GetCooperative(ctx, ref)
	if err != nil {
		return HTXProfile{}, err
	}
	fullContact := p.Has(authz.RolePlatformAdmin) || p.Has(port.RoleCoopManager)
	out := HTXProfile{Cooperative: c, CanEdit: fullContact && c.Active, ContactMasked: !fullContact}
	if c.RepresentativeRef == uuid.Nil {
		return out, nil
	}
	if r.Vault == nil {
		return HTXProfile{}, ErrDependency.WithDetail("chưa nối kho định danh")
	}
	if !fullContact {
		contact, err := r.Vault.Display(ctx, client.PurposeContact, c.RepresentativeRef)
		if err != nil {
			return HTXProfile{}, vaultError(err)
		}
		out.RepresentativeName, out.Phone = contact.Label, contact.MaskedPhone
		return out, nil
	}
	contact, err := r.Vault.Resolve(ctx, client.PurposeRegistryAdmin, c.RepresentativeRef)
	if err != nil {
		return HTXProfile{}, vaultError(err)
	}
	out.RepresentativeName, out.Phone = contact.FullName, localHTXPhone(contact.Phone)
	return out, nil
}

func localHTXPhone(phone string) string {
	if strings.HasPrefix(phone, "84") && len(phone) == 11 {
		return "0" + phone[2:]
	}
	return phone
}

// CreateHTXProfile là luồng Admin của FN-03, không bắt MST/neo KN. Vẫn dùng lõi
// Registry tạo Nút + sổ/outbox, rồi hoàn tất hồ sơ CoopProfiles hai bước.
func (r *Registry) CreateHTXProfile(ctx context.Context, in HTXProfileInput) (OrgResult, error) {
	p, err := authz.Require(ctx, authz.RolePlatformAdmin)
	if err != nil {
		return OrgResult{}, err
	}
	if in.IdemKey == "" {
		return OrgResult{}, problem.IdempotencyRequired
	}
	key := "htx-profile:create:" + p.ActorRef.String() + ":" + in.IdemKey
	if prev, err := r.replay(ctx, p.TenantID, key); err != nil {
		return OrgResult{}, err
	} else if prev != nil {
		return r.completeCoop(ctx, p.TenantID, *prev)
	}
	if err := validateHTXProfile(&in); err != nil {
		return OrgResult{}, err
	}
	unit, err := r.unitInfo(ctx, port.OrgCooperative, in.CommuneCode, "commune_code")
	if err != nil {
		return OrgResult{}, err
	}
	if unit.Disabled {
		return OrgResult{}, problem.Validation.WithField("commune_code", "Đơn vị hành chính đã ngừng sử dụng")
	}
	// Kiểm sớm trước khi ghi PII; lõi CreateOrg kiểm lại dưới khóa khi ghi.
	if err := tenant.WithTenantTx(ctx, r.DB, p.TenantID, func(tx pgx.Tx) error {
		owner, err := store.CooperativeNameOwner(ctx, tx, in.Name)
		if err != nil {
			return err
		}
		if owner != uuid.Nil {
			return ErrCoopNameTaken
		}
		return nil
	}); err != nil {
		return OrgResult{}, err
	}
	if r.Vault == nil {
		return OrgResult{}, ErrDependency.WithDetail("chưa nối kho định danh")
	}
	contact, err := r.Vault.Create(ctx, client.PurposeRegistryAdmin, client.Identity{
		Kind: client.SubjectPerson, FullName: in.RepresentativeName, Phone: in.Phone, AreaCode: in.CommuneCode,
	})
	if err != nil && (!errors.Is(err, client.ErrDuplicate) || contact.Ref == uuid.Nil) {
		return OrgResult{}, vaultError(err)
	}
	return r.CreateOrg(ctx, CreateOrgInput{Kind: port.OrgCooperative, Name: in.Name, AdminUnitCode: in.CommuneCode,
		RepresentativeRef: contact.Ref, Cooperative: &domain.CooperativeProfile{Address: in.HeadOffice}, IdemKey: key})
}
