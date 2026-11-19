package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/party/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/party/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/party/store"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/vault/identity/client"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"net/http"
	"strconv"
)

var ErrCooperativeInUse = problem.New(http.StatusConflict, "party.cooperative_in_use", "HTX đã phát sinh dữ liệu, không thể xóa")

func (r *Registry) ensureCooperativeNotDeleted(ctx context.Context, tid, ref uuid.UUID) error {
	return tenant.WithTenantTx(ctx, r.DB, tid, func(tx pgx.Tx) error {
		deleted, err := store.CooperativeWasDeleted(ctx, tx, ref)
		if err != nil {
			return err
		}
		if deleted {
			return ErrOrgNotFound
		}
		return nil
	})
}

type CooperativeDeletion struct {
	Ref        uuid.UUID
	Code, Name string
	Revision   int64
	store.CooperativeDeletionUsage
}

func (d CooperativeDeletion) CanDelete() bool {
	return d.Households == 0 && d.Accounts == 0 && d.PendingAccounts == 0 && !d.HasSharing && !d.HasBusiness
}

func (r *Registry) cooperativeForDeletion(ctx context.Context, tid, ref uuid.UUID) (*store.OrgState, error) {
	var org *store.OrgState
	err := tenant.WithTenantTx(ctx, r.DB, tid, func(tx pgx.Tx) error {
		var e error
		org, e = store.OrgByRef(ctx, tx, ref, false)
		if e != nil {
			return e
		}
		if org == nil || org.Kind != port.OrgCooperative {
			return ErrOrgNotFound
		}
		pending, e := store.HTXProfilePending(ctx, tx, ref)
		if e != nil {
			return e
		}
		if pending {
			return ErrHTXProfilePending
		}
		return nil
	})
	return org, err
}

func (r *Registry) cooperativeIAMUsers(ctx context.Context, ref uuid.UUID) ([]uuid.UUID, error) {
	if r.AccountUsers == nil {
		return nil, ErrDependency.WithDetail("Chưa kiểm tra được tài khoản để xóa HTX")
	}
	users, err := r.AccountUsers.CooperativeAccountUsers(ctx, ref)
	if err != nil {
		return nil, ErrDependency.WithDetail("Chưa kiểm tra được tài khoản để xóa HTX").WithCause(err)
	}
	return users, nil
}

func (r *Registry) CooperativeDeletion(ctx context.Context, ref uuid.UUID) (CooperativeDeletion, error) {
	p, err := authz.Require(ctx, authz.RolePlatformAdmin)
	if err != nil {
		return CooperativeDeletion{}, err
	}
	org, err := r.cooperativeForDeletion(ctx, p.TenantID, ref)
	if err != nil {
		return CooperativeDeletion{}, err
	}
	users, err := r.cooperativeIAMUsers(ctx, ref)
	if err != nil {
		return CooperativeDeletion{}, err
	}
	out := CooperativeDeletion{Ref: ref, Code: org.Code, Name: org.Name, Revision: org.LastSeq}
	err = tenant.WithTenantTx(ctx, r.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		out.CooperativeDeletionUsage, e = store.CooperativeDeletionReferences(ctx, tx, ref, users)
		return e
	})
	if err != nil {
		return CooperativeDeletion{}, ErrDependency.WithDetail("Chưa kiểm tra đủ dữ liệu để xóa HTX").WithCause(err)
	}
	return out, nil
}

type DeleteCooperativeInput struct {
	Ref              uuid.UUID
	ExpectedRevision int64
	IdemKey          string
}

func deletionDigest(in DeleteCooperativeInput) string {
	hash := sha256.Sum256([]byte(in.Ref.String() + ":" + strconv.FormatInt(in.ExpectedRevision, 10) + ":" + in.IdemKey))
	return hex.EncodeToString(hash[:])
}

func (r *Registry) DeleteCooperative(ctx context.Context, in DeleteCooperativeInput) (OrgResult, error) {
	p, err := authz.Require(ctx, authz.RolePlatformAdmin)
	if err != nil {
		return OrgResult{}, err
	}
	ctx = authz.WithPurpose(ctx, "registry.cooperative_delete")
	if in.IdemKey == "" {
		return OrgResult{}, problem.IdempotencyRequired
	}
	if in.ExpectedRevision < 1 {
		return OrgResult{}, problem.Validation.WithField("expected_revision", "Cần phiên bản hồ sơ đã kiểm tra")
	}
	key := "htx-profile:delete:" + p.ActorRef.String() + ":" + in.Ref.String() + ":" + in.IdemKey
	digest := deletionDigest(in)
	if previous, err := r.replay(ctx, p.TenantID, key); err != nil {
		return OrgResult{}, err
	} else if previous != nil {
		return r.finishCooperativeDeletion(ctx, p.TenantID, *previous, in.Ref, digest)
	}
	if _, err := r.cooperativeForDeletion(ctx, p.TenantID, in.Ref); err != nil {
		// Yêu cầu cùng khóa có thể vừa thắng giữa replay và lần đọc Nút.
		if previous, replayErr := r.replay(ctx, p.TenantID, key); replayErr == nil && previous != nil {
			return r.finishCooperativeDeletion(ctx, p.TenantID, *previous, in.Ref, digest)
		}
		return OrgResult{}, err
	}
	eraser, ok := client.AsHTXContactEraser(r.Vault)
	if !ok {
		return OrgResult{}, ErrDependency.WithDetail("Chưa nối thao tác xóa liên hệ HTX")
	}
	if err := eraser.ValidateHTXContactErasure(ctx, client.PurposeRegistryAdmin, in.Ref); err != nil {
		return OrgResult{}, ErrDependency.WithDetail("Nguồn xóa liên hệ HTX chưa sẵn sàng").WithCause(err)
	}
	users, err := r.cooperativeIAMUsers(ctx, in.Ref)
	if err != nil {
		return OrgResult{}, err
	}
	result, err := r.write(ctx, p.TenantID, key, func(tx pgx.Tx) (OrgResult, error) {
		if err := store.LockCooperativeDeletion(ctx, tx, in.Ref); err != nil {
			return OrgResult{}, err
		}
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
		usage, err := store.CooperativeDeletionReferences(ctx, tx, in.Ref, users)
		if err != nil {
			return OrgResult{}, ErrDependency.WithDetail("Chưa kiểm tra đủ dữ liệu để xóa HTX").WithCause(err)
		}
		if !(CooperativeDeletion{CooperativeDeletionUsage: usage}).CanDelete() {
			return OrgResult{}, ErrCooperativeInUse.WithDetail(fmt.Sprintf("HTX còn %d hộ, %d tài khoản; chỉ xóa HTX chưa phát sinh dữ liệu.", usage.Households, usage.Accounts)).
				With("households", usage.Households).With("accounts", usage.Accounts).With("pending_accounts", usage.PendingAccounts).
				With("has_sharing", usage.HasSharing).With("has_business", usage.HasBusiness)
		}
		org := cur.Org
		org.Active = false
		event := newEvent(p, org, domain.ActionDeleted, r.now(), key)
		event.Reason = "Xóa HTX tạo nhầm, chưa phát sinh dữ liệu"
		event.Details = map[string]any{"request_sha256": digest}
		if err := store.InsertOrgEvent(ctx, tx, &event); err != nil {
			return OrgResult{}, err
		}
		// Cho projection k/ref nhận đúng seq xóa khi trigger DELETE chạy.
		if err := store.UpdateOrg(ctx, tx, org, event.Reason, event); err != nil {
			return OrgResult{}, err
		}
		if err := enqueueStatusEvent(ctx, tx, event, 0); err != nil {
			return OrgResult{}, err
		}
		if err := store.RemoveCooperativeProjection(ctx, tx, in.Ref); err != nil {
			return OrgResult{}, err
		}
		return OrgResult{Org: org, Event: event}, nil
	})
	if err != nil {
		return OrgResult{}, err
	}
	return r.finishCooperativeDeletion(ctx, p.TenantID, result, in.Ref, digest)
}

func (r *Registry) finishCooperativeDeletion(ctx context.Context, tid uuid.UUID, result OrgResult, ref uuid.UUID, digest string) (OrgResult, error) {
	if result.Event.Action != domain.ActionDeleted || result.Event.Org.Ref != ref || result.Event.Details["request_sha256"] != digest {
		return OrgResult{}, problem.IdempotencyConflict
	}
	var done bool
	if err := tenant.WithTenantTx(ctx, r.DB, tid, func(tx pgx.Tx) error {
		var e error
		done, e = store.CooperativeDeletionCompleted(ctx, tx, result.Event.ID)
		return e
	}); err != nil {
		return OrgResult{}, err
	}
	if done {
		return result, nil
	}
	eraser, ok := client.AsHTXContactEraser(r.Vault)
	if !ok {
		return OrgResult{}, ErrDependency.WithDetail("Chưa nối thao tác xóa liên hệ HTX")
	}
	if err := eraser.EraseHTXContact(ctx, client.PurposeRegistryAdmin, ref, result.Event.ID); err != nil {
		return OrgResult{}, ErrDependency.WithDetail("Chưa hoàn tất xóa liên hệ HTX; thử lại cùng thao tác").WithCause(err)
	}
	if err := tenant.WithTenantTx(ctx, r.DB, tid, func(tx pgx.Tx) error { return store.CompleteCooperativeDeletion(ctx, tx, result.Event.ID) }); err != nil {
		return OrgResult{}, err
	}
	return result, nil
}
