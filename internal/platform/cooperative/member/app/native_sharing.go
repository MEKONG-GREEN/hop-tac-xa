package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/member/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/member/store"
	partyport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/party/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrSharingConflict = problem.New(http.StatusConflict, "cooperative.sharing_conflict", "Yêu cầu vừa thay đổi hoặc đã có yêu cầu đang hiệu lực")
	ErrSharingNotFound = problem.New(http.StatusNotFound, "cooperative.sharing_not_found", "Không tìm thấy yêu cầu chia sẻ")
)

func sharingCaller(ctx context.Context) (authz.Principal, error) {
	p, err := authz.Require(ctx, htxroles.Representative)
	if err != nil {
		return p, err
	}
	// Admin never administers a cooperative's choice, even for malformed mixed
	// legacy tokens. Native two-role tokens have already been narrowed by IAM.
	if p.Has(htxroles.Admin) || p.KN != nil || p.SelectedSession != nil {
		return p, problem.Forbidden
	}
	if p.OrgRef == uuid.Nil {
		return p, problem.NodeScopeRequired
	}
	if tid, err := tenant.FromContext(ctx); err != nil || tid != p.TenantID {
		return p, problem.Unauthenticated
	}
	return p, nil
}

type SharingCandidate struct {
	Ref  uuid.UUID `json:"ref"`
	Code string    `json:"code"`
	Name string    `json:"name"`
}

func (s *Service) NativeSharingList(ctx context.Context) ([]domain.NativeSharing, error) {
	p, err := sharingCaller(ctx)
	if err != nil {
		return nil, err
	}
	var out []domain.NativeSharing
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error { var e error; out, e = store.SharingList(ctx, tx, p.OrgRef); return e })
	if err != nil {
		return nil, err
	}
	if len(out) > 0 {
		if s.Dir == nil {
			return nil, ErrDependency
		}
		refs := make([]uuid.UUID, 0, len(out)*2)
		for _, g := range out {
			refs = append(refs, g.Requester, g.Owner)
		}
		orgs, e := s.Dir.OrgsByRefs(ctx, refs)
		if e != nil {
			return nil, e
		}
		for i := range out {
			out[i].RequesterName = orgs[out[i].Requester].Name
			out[i].OwnerName = orgs[out[i].Owner].Name
		}
	}
	return out, err
}

func (s *Service) NativeSharingCandidates(ctx context.Context) ([]SharingCandidate, error) {
	p, err := sharingCaller(ctx)
	if err != nil {
		return nil, err
	}
	if s.Dir == nil {
		return nil, ErrDependency
	}
	// Directory port is intentionally limited to non-personal labels, and is
	// called outside the owner's transaction. No profile/representative leaks.
	orgs, err := s.Dir.OrgsOfKind(ctx, partyport.OrgCooperative, nil, true)
	if err != nil {
		return nil, err
	}
	requests, err := s.NativeSharingList(ctx)
	if err != nil {
		return nil, err
	}
	blocked := map[uuid.UUID]bool{p.OrgRef: true}
	for _, r := range requests {
		if r.Requester == p.OrgRef && (r.State == "requested" || r.State == "approved") {
			blocked[r.Owner] = true
		}
	}
	out := []SharingCandidate{}
	for _, o := range orgs {
		if !blocked[o.Ref] {
			out = append(out, SharingCandidate{Ref: o.Ref, Code: o.Code, Name: o.Name})
		}
	}
	return out, nil
}

// NativeChangeSharing executes one state transition and its audit and replay
// receipt atomically. No other module or external service is called in the tx.
func (s *Service) NativeChangeSharing(ctx context.Context, ref, target uuid.UUID, action string, revision int64, key string) (domain.NativeSharing, error) {
	p, err := sharingCaller(ctx)
	if err != nil {
		return domain.NativeSharing{}, err
	}
	if strings.TrimSpace(key) == "" || len(key) > 200 {
		return domain.NativeSharing{}, problem.Validation.WithField("Idempotency-Key", "Bắt buộc nhập, tối đa 200 ký tự")
	}
	if action == "request" {
		if ref != uuid.Nil || revision != 0 {
			return domain.NativeSharing{}, problem.Validation
		}
		if target == uuid.Nil {
			return domain.NativeSharing{}, problem.Validation.WithField("owner_cooperative_ref", "Bắt buộc nhập")
		}
		if target == p.OrgRef {
			return domain.NativeSharing{}, problem.Validation.WithField("owner_cooperative_ref", "Không gửi yêu cầu tới chính HTX của mình")
		}
		for _, coop := range []uuid.UUID{p.OrgRef, target} {
			o, e := s.coopOrg(ctx, p.TenantID, coop)
			if e != nil {
				return domain.NativeSharing{}, e
			}
			if o == nil || !o.Active {
				return domain.NativeSharing{}, ErrOrgNotFound
			}
		}
	} else if ref == uuid.Nil || revision < 1 || target != uuid.Nil || (action != "approve" && action != "reject" && action != "revoke") {
		return domain.NativeSharing{}, problem.Validation
	}
	// Approval requires active cooperatives, without retaining a DB connection
	// during the authoritative organization lookup. Revocation is always possible.
	if action == "approve" {
		var current *domain.NativeSharing
		err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error { var e error; current, e = store.SharingByRef(ctx, tx, ref, false); return e })
		if err != nil {
			return domain.NativeSharing{}, err
		}
		if current == nil {
			return domain.NativeSharing{}, ErrSharingNotFound
		}
		for _, coop := range []uuid.UUID{current.Requester, current.Owner} {
			o, e := s.coopOrg(ctx, p.TenantID, coop)
			if e != nil {
				return domain.NativeSharing{}, e
			}
			if o == nil || !o.Active {
				return domain.NativeSharing{}, ErrOrgState
			}
		}
	}
	var out domain.NativeSharing
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		receipt, e := store.SharingReplay(ctx, tx, p.ActorRef, key)
		if e != nil {
			return e
		}
		if receipt != nil {
			if receipt.Action != action || receipt.Revision != revision || (action == "request" && (receipt.Owner != target || receipt.Requester != p.OrgRef)) || (action != "request" && receipt.RequestRef != ref) {
				return problem.IdempotencyConflict
			}
			return json.Unmarshal(receipt.Response, &out)
		}
		if action == "request" {
			out = domain.NativeSharing{Ref: ids.New(), Requester: p.OrgRef, Owner: target, State: "requested", Revision: 1, RequestedAt: s.now(), ActorRef: p.ActorRef}
			if e := store.InsertSharing(ctx, tx, p.TenantID, out); e != nil {
				return e
			}
		} else {
			current, e := store.SharingByRef(ctx, tx, ref, true)
			if e != nil {
				return e
			}
			if current == nil {
				return ErrSharingNotFound
			}
			if current.Revision != revision {
				return ErrSharingConflict
			}
			next, e := domain.NextSharingState(*current, p.OrgRef, action)
			if e != nil {
				return ErrSharingConflict
			}
			out = *current
			out.State = next
			out.Revision++
			out.ActorRef = p.ActorRef
			when := s.now()
			out.ProcessedAt = &when
			if e := store.UpdateSharing(ctx, tx, out); e != nil {
				return e
			}
		}
		if e := store.InsertSharingEvent(ctx, tx, p.TenantID, out, action, "native-sharing:"+p.ActorRef.String()+":"+key); e != nil {
			return e
		}
		raw, e := json.Marshal(out)
		if e != nil {
			return e
		}
		return store.SaveSharingReplay(ctx, tx, p.TenantID, p.ActorRef, key, store.SharingOperation{RequestRef: out.Ref, Action: action, Revision: revision, Requester: out.Requester, Owner: out.Owner, Response: raw})
	})
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" && pg.ConstraintName == "native_sharing_active_pair" {
		return domain.NativeSharing{}, ErrSharingConflict
	}
	return out, err
}
