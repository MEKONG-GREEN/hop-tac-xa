package app

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/envelope"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/vault/identity/client"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/vault/identity/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/vault/identity/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var nativeNationalID = regexp.MustCompile(`^[0-9]{12}$`)
var nativePhone = regexp.MustCompile(`^0[0-9]{9}$`)

// Command ownership comes from PF, never a cooperative supplied in this body.
func (s *Service) WriteHouseholdIdentity(ctx context.Context, purpose client.Purpose, command uuid.UUID, in client.Identity) error {
	p, err := householdIdentityCaller(ctx, purpose)
	if err != nil {
		return err
	}
	verifier, ok := s.HouseholdScopes.(client.HouseholdCommandVerifier)
	if !ok {
		return client.ProblemHouseholdScopeUnavailable
	}
	scope, err := verifier.HouseholdCommandScope(ctx, command)
	if err != nil {
		return err
	}
	action := htxroles.Update
	if scope.Operation == "create" {
		action = htxroles.Create
	}
	if !htxroles.Allows(p.Has, "FN-04", action) {
		return problem.Forbidden
	}
	if scope.CommandRef != command || scope.HouseholdRef == uuid.Nil || scope.CooperativeRef == uuid.Nil || (!p.Has(htxroles.Admin) && scope.CooperativeRef != p.OrgRef) || (scope.Operation != "create" && scope.Operation != "update") {
		return problem.Forbidden
	}
	in.FullName = domain.NormalizeName(in.FullName)
	in.Phone = strings.TrimSpace(in.Phone)
	in.NationalID = strings.TrimSpace(in.NationalID)
	if !nativeNationalID.MatchString(in.NationalID) {
		return problem.Validation.WithField("national_id", "CCCD gồm đúng 12 chữ số")
	}
	if !nativePhone.MatchString(in.Phone) {
		return problem.Validation.WithField("phone", "SĐT gồm 10 chữ số, bắt đầu bằng 0")
	}
	if n := utf8.RuneCountInString(in.FullName); n < 1 || n > 200 {
		return problem.Validation.WithField("full_name", "Bắt buộc nhập, tối đa 200 ký tự")
	}
	if utf8.RuneCountInString(in.Address) > 500 {
		return problem.Validation.WithField("address", "Tối đa 500 ký tự")
	}
	in.Kind = client.SubjectHousehold
	in.AreaCode = scope.CommuneCode
	// Completion is mutable owner metadata, not part of the immutable command.
	scope.Completed = false
	raw, _ := json.Marshal(struct {
		Scope    client.HouseholdCommandScope
		Identity client.Identity
	}{scope, in})
	mac := hmac.New(sha256.New, s.BlindKey)
	_, _ = mac.Write([]byte("native-household:" + p.TenantID.String() + ":"))
	_, _ = mac.Write(raw)
	digest := mac.Sum(nil)
	for attempt := 0; attempt < 2; attempt++ {
		err = s.writeNativeHouseholdOnce(ctx, p, purpose, scope, in, digest)
		if !errors.Is(err, errBlindRace) && !errors.Is(err, errAmendKeyChanged) {
			return err
		}
	}
	return client.ProblemHouseholdScopeUnavailable.WithCause(err)
}

func (s *Service) writeNativeHouseholdOnce(ctx context.Context, p authz.Principal, purpose client.Purpose, scope client.HouseholdCommandScope, in client.Identity, digest []byte) error {
	ref := scope.HouseholdRef
	var subject *store.Subject
	var oldKey *store.Key
	var replay bool
	checkReplay := func(tx pgx.Tx) error {
		oldRef, oldDigest, err := store.NativeHouseholdCommand(ctx, tx, scope.CommandRef)
		if err != nil {
			return err
		}
		if oldRef != uuid.Nil {
			if oldRef != ref || !hmac.Equal(oldDigest, digest) {
				return problem.IdempotencyConflict
			}
			replay = true
		}
		return nil
	}
	err := tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		if err := checkReplay(tx); err != nil || replay {
			return err
		}
		var err error
		subject, err = store.GetSubject(ctx, tx, ref, false)
		if err != nil {
			return err
		}
		if subject != nil {
			oldKey, err = store.GetKey(ctx, tx, ref)
		}
		return err
	})
	if err != nil || replay {
		return err
	}
	// A completed PF command whose Vault receipt is missing must not be replayed
	// as a fresh mutation. This also fences restore after a metadata deletion.
	latest, err := s.HouseholdScopes.(client.HouseholdCommandVerifier).HouseholdCommandScope(ctx, scope.CommandRef)
	if err != nil {
		return err
	}
	if latest.Completed {
		return client.ProblemHouseholdScopeUnavailable
	}
	if scope.Operation == "create" && subject != nil {
		return problem.IdempotencyConflict
	}
	if scope.Operation == "update" && (subject == nil || subject.ErasedAt != nil || subject.Kind != "household" || oldKey == nil) {
		return client.ProblemNotFound
	}
	var key store.Key
	var dk []byte
	if subject == nil {
		pub, priv, err := envelope.GenerateKeyPair(s.rand())
		if err != nil {
			return err
		}
		defer clear(priv[:])
		dk = make([]byte, 32)
		if _, err = io.ReadFull(s.rand(), dk); err != nil {
			return err
		}
		key.PublicKey = pub[:]
		key.KEKID = s.KMS.KeyID()
		key.WrappedPrivateKey, err = s.KMS.Wrap(ctx, priv[:], domain.AAD(useX25519, p.TenantID, ref))
		if err != nil {
			return err
		}
		key.WrappedDataKey, err = s.KMS.Wrap(ctx, dk, domain.AAD(useDataKey, p.TenantID, ref))
		if err != nil {
			return err
		}
	} else {
		key = *oldKey
		dk, err = s.KMS.Unwrap(ctx, key.KEKID, key.WrappedDataKey, domain.AAD(useDataKey, p.TenantID, ref))
		if err != nil {
			return err
		}
	}
	defer clear(dk)
	// FN-04 validates exactly ten local digits starting with 0. Keep that
	// contract rather than silently adding the generic API's prefix rules.
	phone := "84" + in.Phone[1:]
	var row store.Identity
	for _, f := range []struct {
		dst        *[]byte
		use, value string
	}{{&row.FullNameCT, useName, in.FullName}, {&row.PhoneCT, usePhone, phone}, {&row.NationalIDCT, useNational, in.NationalID}, {&row.AddressCT, useAddress, in.Address}} {
		*f.dst, err = sealField(s.rand(), dk, domain.AAD(f.use, p.TenantID, ref), f.value)
		if err != nil {
			return err
		}
	}
	blinds := []store.Blind{{Kind: domain.BlindNationalID, Value: domain.BlindIndex(s.BlindKey, p.TenantID, domain.BlindNationalID, in.NationalID)},
		{Kind: domain.BlindPhone, Value: domain.BlindIndex(s.BlindKey, p.TenantID, domain.BlindPhone, phone)}}
	return tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('native-hh:'||shared.current_tenant()::text||':'||$1::text,285))`, ref.String()); err != nil {
			return err
		}
		if err := checkReplay(tx); err != nil || replay {
			return err
		}
		current, err := store.GetSubject(ctx, tx, ref, true)
		if err != nil {
			return err
		}
		if subject == nil && current != nil {
			return problem.IdempotencyConflict
		}
		if subject != nil {
			if current == nil || current.ErasedAt != nil {
				return client.ProblemNotFound
			}
			now, err := store.GetKey(ctx, tx, ref)
			if err != nil {
				return err
			}
			if now == nil || now.KEKID != key.KEKID || !bytes.Equal(now.WrappedDataKey, key.WrappedDataKey) {
				return errAmendKeyChanged
			}
			rev, err := store.NativeHouseholdRevision(ctx, tx, ref)
			if err != nil {
				return err
			}
			if rev > scope.ExpectedRevision {
				return problem.IdempotencyConflict
			}
		}
		for _, b := range blinds {
			other, found, err := store.FindBlind(ctx, tx, []store.Blind{b})
			if err != nil {
				return err
			}
			if found && other != ref {
				if b.Kind == domain.BlindNationalID {
					return client.ProblemHouseholdNationalIDTaken
				}
				return client.ProblemHouseholdPhoneTaken
			}
		}
		if subject == nil {
			if err := store.InsertSubject(ctx, tx, store.Subject{TenantID: p.TenantID, Ref: ref, Kind: "household", AreaCode: in.AreaCode, CreatedAt: s.Clock.Now(), CreatedBy: p.ActorRef}); err != nil {
				return err
			}
			if err := store.InsertIdentity(ctx, tx, p.TenantID, ref, row); err != nil {
				return err
			}
			if err := store.InsertKey(ctx, tx, p.TenantID, ref, key); err != nil {
				return err
			}
		} else {
			old, err := store.GetIdentity(ctx, tx, ref)
			if err != nil {
				return err
			}
			if old == nil {
				return client.ProblemNotFound
			}
			if err := store.InsertRevision(ctx, tx, p.TenantID, store.Revision{Ref: scope.CommandRef, SubjectRef: ref, PrevNameCT: old.FullNameCT, PrevPhoneCT: old.PhoneCT, NextNameCT: row.FullNameCT, NextPhoneCT: row.PhoneCT}, p.ActorRef); err != nil {
				return err
			}
			if err := store.UpdateNativeHouseholdIdentity(ctx, tx, ref, row, in.AreaCode); err != nil {
				return err
			}
			for _, b := range blinds {
				if err := store.DeleteBlind(ctx, tx, ref, b.Kind); err != nil {
					return err
				}
			}
		}
		for _, b := range blinds {
			ok, err := store.InsertBlind(ctx, tx, p.TenantID, ref, b)
			if err != nil {
				return err
			}
			if !ok {
				return errBlindRace
			}
		}
		if err := store.CompleteNativeHouseholdCommand(ctx, tx, p.TenantID, scope.CommandRef, ref, scope.ExpectedRevision+1, digest); err != nil {
			return err
		}
		op := domain.OpAmend
		if subject == nil {
			op = domain.OpCreate
		}
		return s.logAccess(ctx, tx, p, &ref, true, op, purpose, domain.OutcomeOK)
	})
}

var _ client.HouseholdIdentityWriter = (*Service)(nil)
