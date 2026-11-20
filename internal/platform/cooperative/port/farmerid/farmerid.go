// Package farmerid là bộ điều hợp port.FarmerIdentity, port.HouseholdIdentities của pf/cooperative trên vault client (R-04): tên nông dân của
// thửa KN (KN v7.7 FN-XA-03.05; chuyển từ geo/port/farmerid ở W3.3). Tách khỏi gốc module để cmd/api-public (dựng
// pf/cooperative) không kéo vault (RB-3).
package farmerid

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/vault/identity/client"
)

type adapter struct{ c client.Client }

// New bọc vault client; c nil → nil (pf/cooperative coi như chưa nối kho định danh).
func New(c client.Client) port.FarmerIdentity {
	if c == nil {
		return nil
	}
	return adapter{c: c}
}

func (a adapter) RegisterFarmer(ctx context.Context, fullName, areaCode string) (uuid.UUID, error) {
	cr, err := a.c.Create(ctx, client.PurposeContact, client.Identity{Kind: client.SubjectPerson, FullName: fullName, AreaCode: areaCode})
	if err != nil {
		if errors.Is(err, client.ErrDuplicate) && cr.Ref != uuid.Nil {
			return cr.Ref, nil
		}
		return uuid.Nil, err
	}
	return cr.Ref, nil
}

func (a adapter) FarmerLabel(ctx context.Context, ref uuid.UUID) (string, error) {
	d, err := a.c.Display(ctx, client.PurposeContact, ref)
	return d.Label, err
}

// Households bọc vault client thành port.HouseholdIdentities (cooperative/member); c nil → nil.
func Households(c client.Client) port.HouseholdIdentities {
	if c == nil {
		return nil
	}
	return households{c: c}
}

type households struct{ c client.Client }

func (h households) WriteNativeHouseholdIdentity(ctx context.Context, command uuid.UUID, id port.HouseholdIdentity) error {
	w, ok := h.c.(client.HouseholdIdentityWriter)
	if !ok {
		return client.ProblemHouseholdScopeUnavailable
	}
	return w.WriteHouseholdIdentity(ctx, client.PurposeContact, command, client.Identity{FullName: id.FullName, Phone: id.Phone, NationalID: id.NationalID, Address: id.Address})
}

func (h households) NativeHouseholdIdentity(ctx context.Context, ref uuid.UUID) (port.HouseholdIdentity, error) {
	r, ok := client.AsHouseholdIdentityReader(h.c)
	if !ok {
		return port.HouseholdIdentity{}, client.ProblemHouseholdScopeUnavailable
	}
	id, err := r.ReadHouseholdIdentity(ctx, client.PurposeContact, ref)
	if err != nil {
		return port.HouseholdIdentity{}, err
	}
	return port.HouseholdIdentity{FullName: id.FullName, Phone: id.Phone, NationalID: id.NationalID, Address: id.Address, AreaCode: id.AreaCode}, nil
}

func (h households) RegisterHousehold(ctx context.Context, id port.HouseholdIdentity) (uuid.UUID, error) {
	cr, err := h.c.Create(ctx, client.PurposeRegistryAdmin, client.Identity{Kind: client.SubjectHousehold, FullName: id.FullName,
		Phone: id.Phone, NationalID: id.NationalID, Address: id.Address, AreaCode: id.AreaCode})
	if errors.Is(err, client.ErrDuplicate) {
		return cr.Ref, port.ErrIdentityDuplicate
	}
	return cr.Ref, err
}

func (h households) Label(ctx context.Context, purpose port.IdentityPurpose, ref uuid.UUID) (port.IdentityLabel, error) {
	_, nativeAvailable := client.AsHouseholdIdentityReader(h.c)
	if p, ok := authz.FromContext(ctx); nativeAvailable && ok && p.KN == nil && (p.Has(htxroles.Admin) || p.Has(htxroles.Representative) || p.Has(htxroles.Member)) {
		id, err := h.NativeHouseholdIdentity(ctx, ref)
		phone := id.Phone
		if len(phone) > 7 {
			phone = phone[:4] + "***" + phone[len(phone)-3:]
		}
		words := strings.Fields(id.FullName)
		label := ""
		for i, word := range words {
			if i > 0 {
				label += " "
			}
			if i == 0 && len(words) > 1 {
				label += word
			} else {
				r, _ := utf8.DecodeRuneInString(word)
				label += string(r) + "."
			}
		}
		return port.IdentityLabel{Label: label, MaskedPhone: phone}, err
	}
	p := client.PurposeContact
	if purpose == port.IdentityFieldSupport {
		p = client.PurposeFieldSupport
	}
	d, err := h.c.Display(ctx, p, ref)
	return port.IdentityLabel{Label: d.Label, MaskedPhone: d.MaskedPhone}, err
}

func (h households) Phone(ctx context.Context, ref uuid.UUID) (string, error) {
	if p, ok := authz.FromContext(ctx); ok && p.KN == nil && (p.Has(htxroles.Admin) || p.Has(htxroles.Representative) || p.Has(htxroles.Member)) {
		if _, ok := client.AsHouseholdIdentityReader(h.c); ok {
			id, err := h.NativeHouseholdIdentity(ctx, ref)
			if errors.Is(err, client.ErrNotFound) || errors.Is(err, client.ErrErased) {
				return "", port.ErrIdentityNotFound
			}
			return id.Phone, err
		}
	}
	id, err := h.c.Resolve(ctx, client.PurposeContact, ref)
	if errors.Is(err, client.ErrNotFound) || errors.Is(err, client.ErrErased) {
		return "", port.ErrIdentityNotFound
	}
	return id.Phone, err
}

func (h households) LookupPhone(ctx context.Context, phone string) (uuid.UUID, error) {
	if p, ok := authz.FromContext(ctx); ok && p.KN == nil && (p.Has(htxroles.Admin) || p.Has(htxroles.Representative) || p.Has(htxroles.Member)) {
		if q, ok := h.c.(client.HouseholdPhoneLookup); ok {
			ref, err := q.LookupHouseholdPhone(ctx, client.PurposeContact, phone)
			if errors.Is(err, client.ErrNotFound) {
				return uuid.Nil, port.ErrIdentityNotFound
			}
			return ref, err
		}
	}
	ref, err := h.c.Lookup(ctx, client.PurposeRegistryAdmin, phone)
	if errors.Is(err, client.ErrNotFound) {
		return uuid.Nil, port.ErrIdentityNotFound
	}
	return ref, err
}
