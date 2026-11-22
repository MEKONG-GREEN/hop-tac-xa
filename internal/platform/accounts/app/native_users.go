package app

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/store"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
)

type NativeCreateResult struct {
	User              UserView
	TemporaryPassword string `pii:"true"`
	Replayed          bool
}

func (s *Service) nativeAccountCaller(ctx context.Context) (*actor, error) {
	if len(s.NativeHTXKey) < 32 {
		return nil, ErrDependency.WithDetail("chưa cấu hình tài khoản FN-07 trong realm HTX")
	}
	a, err := s.caller(ctx)
	if err != nil {
		return nil, err
	}
	if !a.p.Has(htxroles.Admin) || !a.p.MFA || a.p.TenantID == uuid.Nil || a.p.ActorRef == uuid.Nil {
		return nil, problem.Forbidden
	}
	return a, nil
}

func (s *Service) nativeDigest(kind string, value []byte) []byte {
	h := hmac.New(sha256.New, s.NativeHTXKey)
	h.Write([]byte("FN-07/v1/" + kind + "\x00"))
	h.Write(value)
	return h.Sum(nil)
}

// NativeCreate enforces the dedicated realm's single-cooperative policy.
// A durable claim precedes IAM calls. The credential never enters a ledger or replay cache.
func (s *Service) NativeCreate(ctx context.Context, input domain.NativeAccountInput, key string) (NativeCreateResult, error) {
	a, err := s.nativeAccountCaller(ctx)
	if err != nil {
		return NativeCreateResult{}, err
	}
	return s.nativeCreate(ctx, a, input, key)
}

func (s *Service) nativeCreate(ctx context.Context, a *actor, input domain.NativeAccountInput, key string) (NativeCreateResult, error) {
	if err := coopCreateAllowed(a, input); err != nil {
		return NativeCreateResult{}, err
	}
	if err := requireKey(key); err != nil {
		return NativeCreateResult{}, err
	}
	in, err := input.Normalize()
	if err != nil {
		return NativeCreateResult{}, fieldProblem(err)
	}
	setter, ok := s.Dir.(port.TemporaryPasswordSetter)
	if !ok {
		return NativeCreateResult{}, ErrDependency
	}
	if in.CooperativeRef != uuid.Nil {
		o, err := s.checkOrg(ctx, a, in.CooperativeRef)
		if err != nil {
			return NativeCreateResult{}, err
		}
		if string(o.Kind) != "cooperative" {
			return NativeCreateResult{}, problem.Validation.WithField("cooperative_ref", "Chọn HTX")
		}
	}
	if err := s.checkModules(ctx, a, in.Roles); err != nil {
		return NativeCreateResult{}, err
	}
	body, err := json.Marshal(in)
	if err != nil {
		return NativeCreateResult{}, err
	}
	defer clear(body)
	operation := s.nativeDigest("operation", []byte(a.p.ActorRef.String()+":"+key))
	digest := s.nativeDigest("request", body)
	var claim store.NativeCreation
	var fresh bool
	err = tenant.WithTenantTx(ctx, s.DB, a.p.TenantID, func(tx pgx.Tx) error {
		var e error
		claim, fresh, e = store.ClaimNativeCreation(ctx, tx, ids.New(), a.p.ActorRef, operation, digest, key)
		return e
	})
	if store.IsNativeCreationBusy(err) {
		return NativeCreateResult{}, ErrDependency.WithDetail("Một lượt tạo tài khoản đang xử lý; vui lòng thử lại")
	}
	if err != nil {
		return NativeCreateResult{}, err
	}
	if !fresh {
		if claim.State != "complete" {
			return NativeCreateResult{}, problem.IdempotencyInFlight
		}
		u, err := s.Dir.GetUser(ctx, claim.UserRef)
		if err != nil {
			return NativeCreateResult{}, dirErr(err)
		}
		if u.TenantID != a.p.TenantID || u.OrgRef != in.CooperativeRef || u.CreationRef != claim.ID {
			return NativeCreateResult{}, ErrOutOfScope
		}
		return NativeCreateResult{User: UserView{User: u, Roles: in.Roles, Tracked: true}, Replayed: true}, nil
	}
	finishFailure := func(cause error) (NativeCreateResult, error) {
		err := tenant.WithTenantTx(context.WithoutCancel(ctx), s.DB, a.p.TenantID, func(tx pgx.Tx) error {
			return store.FinishNativeCreation(context.WithoutCancel(ctx), tx, claim.ID, uuid.Nil, true)
		})
		return NativeCreateResult{}, errors.Join(cause, err)
	}
	// Check every account in the HTX realm, not only the current custodial tenant.
	queries := []port.Query{{Username: in.Username, Max: 1}, {Attributes: map[string]string{port.AttrPhone: in.Phone}, Max: 1}}
	if in.Email != "" {
		queries = append(queries, port.Query{Email: in.Email, Max: 1})
	}
	for i, q := range queries {
		users, err := s.Dir.FindUsers(ctx, q)
		if err != nil {
			return finishFailure(dirErr(err))
		}
		if len(users) > 0 {
			if i == 1 {
				return finishFailure(ErrPhoneTaken.WithDetail("SĐT đã được dùng cho tài khoản khác"))
			}
			return finishFailure(ErrUserExists)
		}
	}
	password, err := nativeTemporaryPassword()
	if err != nil {
		return finishFailure(err)
	}
	reservation := uuid.Nil
	if in.CooperativeRef != uuid.Nil {
		// This child key can be derived from operation metadata during reconciliation,
		// without retaining the caller's raw key or IAM profile in PF.
		reservation, err = s.reserveCooperativeIdentity(ctx, a, in.CooperativeRef, "FN-07:"+claim.ID.String(), in)
		if err != nil {
			return finishFailure(err)
		}
	}
	first, last := domain.SplitFullName(in.FullName)
	id, err := s.Dir.CreateUser(ctx, port.NewUser{CreationRef: claim.ID, Username: in.Username, FirstName: first, LastName: last, Phone: in.Phone, Email: in.Email, EmailVerified: in.Email != "", TenantID: a.p.TenantID, OrgRef: in.CooperativeRef, RequiredActions: []string{port.ActionUpdatePassword}})
	// IAM may have committed before a network error. Keep both claims pending for reconciliation.
	if err != nil {
		if errors.Is(err, port.ErrCreationRejected) || errors.Is(err, port.ErrUserExists) || errors.Is(err, port.ErrInvalidProfile) {
			cause := dirErr(err)
			if errors.Is(err, port.ErrInvalidProfile) {
				cause = problem.Validation.WithDetail("Họ tên hoặc thông tin tài khoản không được IAM chấp nhận")
			}
			if reservation != uuid.Nil {
				if releaseErr := s.releaseCooperativeIdentity(context.WithoutCancel(ctx), a, reservation, in.CooperativeRef); releaseErr != nil {
					return NativeCreateResult{}, errors.Join(cause, releaseErr)
				}
			}
			return finishFailure(cause)
		}
		return NativeCreateResult{}, dirErr(err)
	}
	compensate := func(cause error) (NativeCreateResult, error) {
		cleanup := context.WithoutCancel(ctx)
		if err := s.Dir.DeleteUser(cleanup, id); err != nil {
			return NativeCreateResult{}, errors.Join(cause, err)
		}
		if reservation != uuid.Nil {
			if err := s.releaseCooperativeIdentity(cleanup, a, reservation, in.CooperativeRef); err != nil {
				return NativeCreateResult{}, errors.Join(cause, err)
			}
		}
		return finishFailure(cause)
	}
	if err := s.Dir.AddRealmRoles(ctx, id, in.Roles); err != nil {
		return compensate(dirErr(err))
	}
	if err := setter.SetTemporaryPassword(ctx, id, password); err != nil {
		return compensate(dirErr(err))
	}
	u, err := s.Dir.GetUser(ctx, id)
	if err != nil {
		return compensate(dirErr(err))
	}
	if u.CreationRef != claim.ID || u.TenantID != a.p.TenantID || u.OrgRef != in.CooperativeRef || u.Phone != in.Phone || u.Username != in.Username || !slices.Contains(u.RequiredActions, port.ActionUpdatePassword) {
		return compensate(ErrDependency.WithDetail("IAM không giữ đúng thông tin, phạm vi hoặc yêu cầu đổi mật khẩu"))
	}
	acc := domain.Account{UserRef: id, OrgRef: in.CooperativeRef, Roles: in.Roles, Enabled: true}
	ev := newEvent(a.p.TenantID, a.p.ActorRef, acc, domain.ActionCreated, s.now(), key, "")
	ev.Details = map[string]any{"flow": "FN-07", "required_actions": []string{port.ActionUpdatePassword}}
	err = tenant.WithTenantTx(ctx, s.DB, a.p.TenantID, func(tx pgx.Tx) error {
		if err := record(ctx, tx, &ev); err != nil {
			return err
		}
		if err := store.FinishNativeCreation(ctx, tx, claim.ID, id, false); err != nil {
			return err
		}
		if reservation != uuid.Nil {
			return store.ReleaseCooperativeIdentity(ctx, tx, reservation, in.CooperativeRef)
		}
		return nil
	})
	// A failed COMMIT can be ambiguous: never delete IAM after it may have been committed.
	if err != nil {
		// Deny login until reconciliation, whether or not the commit reached the database.
		return NativeCreateResult{}, errors.Join(err, s.Dir.SetEnabled(context.WithoutCancel(ctx), id, false))
	}
	return NativeCreateResult{User: UserView{User: u, Roles: in.Roles, Tracked: true}, TemporaryPassword: password}, nil
}

func nativeTemporaryPassword() (string, error) {
	return nativeTemporaryPasswordFrom(rand.Reader)
}

func nativeTemporaryPasswordFrom(rnd io.Reader) (string, error) {
	if rnd == nil {
		rnd = rand.Reader
	}
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	var password [8]byte
	for i := range password {
		n, err := rand.Int(rnd, big.NewInt(int64(len(alphabet))))
		if err != nil {
			return "", err
		}
		password[i] = alphabet[n.Int64()]
	}
	return string(password[:]), nil
}
