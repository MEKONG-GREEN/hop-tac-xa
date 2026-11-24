package app

import (
	"context"
	"errors"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/store"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ledger"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
)

// NativeUpdateInput là màn sửa tài khoản FN-07 (AH-276). Tên đăng nhập và vai chính không sửa được
// (FN-07/AC-06, BR-03); Member chỉ bật/tắt kiêm Thành viên HTX cho tài khoản Đại diện HTX (QTPQ-Q-23).
type NativeUpdateInput struct {
	FullName       string    `json:"full_name" pii:"true"`
	Phone          string    `json:"phone" pii:"true"`
	Email          string    `json:"email" pii:"true"`
	CooperativeRef uuid.UUID `json:"cooperative_ref"`
	Member         *bool     `json:"also_member"`
}

const nativeLockReason = "FN-07: Admin khóa hoặc mở khóa tài khoản"

// nativeTarget đọc tài khoản trong realm HTX của tenant người gọi, kèm vai FN-07 đã kiểm.
func (s *Service) nativeTarget(ctx context.Context, a *actor, id uuid.UUID) (UserView, error) {
	users, err := s.nativeUsers(ctx, a)
	if err != nil {
		return UserView{}, err
	}
	for _, u := range users {
		if u.User.ID == id {
			return u, nil
		}
	}
	return UserView{}, problem.NotFound
}

// nativeNext áp thay đổi lên tài khoản hiện tại: giữ tên đăng nhập và vai chính, kiểm như lúc tạo.
func nativeNext(cur UserView, in NativeUpdateInput) (domain.NativeAccountInput, error) {
	main := cur.Roles[0]
	roles := []string{main}
	if in.Member != nil && *in.Member {
		if main != htxroles.Representative {
			return domain.NativeAccountInput{}, domain.FieldError{Field: "also_member", Msg: "Chỉ tài khoản Đại diện HTX được kiêm Thành viên HTX"}
		}
		roles = append(roles, htxroles.Member)
	} else if in.Member == nil && slices.Contains(cur.Roles, htxroles.Member) && main == htxroles.Representative {
		roles = append(roles, htxroles.Member)
	}
	return domain.NativeAccountInput{
		Username: cur.User.Username, FullName: in.FullName, Phone: in.Phone, Email: in.Email,
		CooperativeRef: in.CooperativeRef, Roles: roles,
	}.Normalize()
}

func nativeChanged(cur UserView, next domain.NativeAccountInput) (fields []string, rolesChanged, orgChanged bool) {
	first, last := domain.SplitFullName(next.FullName)
	if first != cur.User.FirstName || last != cur.User.LastName {
		fields = append(fields, "full_name")
	}
	if next.Phone != cur.User.Phone {
		fields = append(fields, "phone")
	}
	if next.Email != cur.User.Email {
		fields = append(fields, "email")
	}
	curRoles := slices.Sorted(slices.Values(cur.Roles))
	if rolesChanged = !slices.Equal(curRoles, next.Roles); rolesChanged {
		fields = append(fields, "roles")
	}
	if orgChanged = next.CooperativeRef != cur.User.OrgRef; orgChanged {
		fields = append(fields, "cooperative_ref")
	}
	return fields, rolesChanged, orgChanged
}

// NativeUpdate sửa tài khoản FN-07. Đổi HTX: cả hai vai cùng chuyển, mọi phiên đang mở bị thu hồi để thao tác kế
// tiếp phải đăng nhập lại và chỉ thấy HTX mới (QTPQ-Q-21, Q-22). Bật/tắt kiêm Thành viên HTX có hiệu lực từ lần
// đăng nhập kế tiếp (QTPQ-Q-23). Gọi lại cùng Idempotency-Key với cùng nội dung → trả kết quả, không ghi lại.
func (s *Service) NativeUpdate(ctx context.Context, id uuid.UUID, input NativeUpdateInput, key string) (UserView, bool, error) {
	a, err := s.nativeAccountCaller(ctx)
	if err != nil {
		return UserView{}, false, err
	}
	return s.nativeUpdate(ctx, a, id, input, key)
}

func (s *Service) nativeUpdate(ctx context.Context, a *actor, id uuid.UUID, input NativeUpdateInput, key string) (UserView, bool, error) {
	if err := requireKey(key); err != nil {
		return UserView{}, false, err
	}
	up, ok := s.Dir.(port.ProfileUpdater)
	if !ok {
		return UserView{}, false, ErrProfileUnsupported
	}
	idem := "native-profile:" + key
	var prev *store.ProfileChange
	if err := tenant.WithTenantTx(ctx, s.DB, a.p.TenantID, func(tx pgx.Tx) error {
		var e error
		prev, e = store.ProfileChangeByIdem(ctx, tx, idem)
		return e
	}); err != nil {
		return UserView{}, false, err
	}
	cur, err := s.nativeTarget(ctx, a, id)
	if err != nil {
		return UserView{}, false, err
	}
	if input, err = coopUpdateInput(a, cur, input); err != nil {
		return UserView{}, false, err
	}
	next, err := nativeNext(cur, input)
	if err != nil {
		return UserView{}, false, fieldProblem(err)
	}
	fields, rolesChanged, orgChanged := nativeChanged(cur, next)
	// Đổi HTX/vai ghi ở sổ tài khoản (assignment_changed); sổ chỉnh sửa chỉ nhận trường hồ sơ (ràng buộc của bảng).
	prevAssignment, err := s.replayFor(ctx, a, "native-edit:"+key, domain.ActionAssignmentChanged, id)
	if err != nil {
		return UserView{}, false, err
	}
	if prev != nil || prevAssignment != nil {
		// Sổ chỉ giữ tên trường: phát lại hợp lệ khi cùng người gọi, cùng tài khoản và nội dung đã đúng hiện trạng.
		if (prev != nil && (prev.ActorRef != a.p.ActorRef || prev.UserRef != id)) || len(fields) > 0 {
			return UserView{}, false, problem.IdempotencyConflict
		}
		return cur, true, nil
	}
	profileFields := slices.DeleteFunc(slices.Clone(fields), func(f string) bool { return f == "roles" || f == "cooperative_ref" })
	if len(fields) == 0 {
		return UserView{}, false, ErrState.WithDetail("Không có thay đổi.")
	}
	// Đại diện HTX được tự bật/tắt kiêm Thành viên HTX cho chính mình (FN-08/AC-06, BR-04); Admin thì không.
	if id == a.p.ActorRef && (orgChanged || (rolesChanged && a.coop == uuid.Nil)) {
		return UserView{}, false, ErrSelfAction
	}
	if slices.Contains(fields, "phone") {
		users, err := s.Dir.FindUsers(ctx, port.Query{Attributes: map[string]string{port.AttrPhone: next.Phone}, Max: 2})
		if err != nil {
			return UserView{}, false, dirErr(err)
		}
		if slices.ContainsFunc(users, func(u port.User) bool { return u.ID != id }) {
			return UserView{}, false, ErrPhoneTaken.WithDetail("SĐT đã được dùng cho tài khoản khác")
		}
	}
	if slices.Contains(fields, "email") && next.Email != "" {
		users, err := s.Dir.FindUsers(ctx, port.Query{Email: next.Email, Max: 2})
		if err != nil {
			return UserView{}, false, dirErr(err)
		}
		if slices.ContainsFunc(users, func(u port.User) bool { return u.ID != id }) {
			return UserView{}, false, ErrDuplicateIdentity.WithDetail("Email đã được dùng cho tài khoản khác")
		}
	}
	if orgChanged {
		// FN-TK-02/EX-04: chuyển Đại diện HTX cuối cùng sang HTX khác để HTX cũ không còn người duyệt.
		if err := s.guardLastRepresentative(ctx, a, cur); err != nil {
			return UserView{}, false, err
		}
	}
	reservation := uuid.Nil
	if orgChanged {
		o, err := s.checkOrg(ctx, a, next.CooperativeRef)
		if err != nil {
			return UserView{}, false, err
		}
		if string(o.Kind) != "cooperative" || !o.Active {
			return UserView{}, false, problem.Validation.WithField("cooperative_ref", "Chọn HTX")
		}
		// Rào xóa HTX (AH-283): HTX mới không bị xóa trong lúc chuyển tài khoản sang.
		reservation, err = s.reserveCooperativeIdentity(ctx, a, next.CooperativeRef, "FN-07-edit:"+key, next)
		if err != nil {
			return UserView{}, false, err
		}
	}
	release := func(cause error) error {
		if reservation == uuid.Nil {
			return cause
		}
		return errors.Join(cause, s.releaseCooperativeIdentity(context.WithoutCancel(ctx), a, reservation, next.CooperativeRef))
	}
	first, last := domain.SplitFullName(next.FullName)
	if slices.ContainsFunc(fields, func(f string) bool { return f == "full_name" || f == "phone" || f == "email" }) {
		if err := up.UpdateProfile(ctx, id, port.ProfileUpdate{Username: cur.User.Username, Email: next.Email, FirstName: first, LastName: last, Phone: next.Phone}); err != nil {
			if errors.Is(err, port.ErrUserExists) {
				return UserView{}, false, release(ErrDuplicateIdentity)
			}
			return UserView{}, false, release(dirErr(err))
		}
	}
	if orgChanged {
		if err := s.Dir.SetOrgRef(ctx, id, next.CooperativeRef); err != nil {
			return UserView{}, false, release(dirErr(err))
		}
	}
	if rolesChanged {
		var err error
		if slices.Contains(next.Roles, htxroles.Member) {
			err = s.Dir.AddRealmRoles(ctx, id, []string{htxroles.Member})
		} else {
			err = s.Dir.RemoveRealmRoles(ctx, id, []string{htxroles.Member})
		}
		if err != nil {
			return UserView{}, false, release(dirErr(err))
		}
	}
	revoked := false
	if orgChanged {
		// QTPQ-Q-22: phiên cũ không xem tiếp dữ liệu HTX cũ.
		if err := s.Dir.Logout(ctx, id); err != nil {
			return UserView{}, false, release(dirErr(err))
		}
		revoked = true
	}
	err = tenant.WithTenantTx(ctx, s.DB, a.p.TenantID, func(tx pgx.Tx) error {
		if len(profileFields) > 0 {
			pc := store.ProfileChange{ID: ids.New(), TenantID: a.p.TenantID, UserRef: id, Fields: profileFields, EventTime: s.now(), ActorRef: a.p.ActorRef, IdemKey: idem}
			if err := ledger.Classify(store.InsertProfileChange(ctx, tx, &pc)); err != nil {
				return err
			}
		}
		if rolesChanged || orgChanged {
			enabled := cur.User.Enabled
			acc := domain.Account{UserRef: id, OrgRef: next.CooperativeRef, Roles: next.Roles, Enabled: enabled, LockCause: cur.LockCause}
			ev := newEvent(a.p.TenantID, a.p.ActorRef, acc, domain.ActionAssignmentChanged, s.now(), "native-edit:"+key, "")
			ev.Details = map[string]any{"flow": "FN-07", "fields": fields, "sessions_revoked": revoked}
			if err := record(ctx, tx, &ev); err != nil {
				return err
			}
		}
		if reservation != uuid.Nil {
			return store.ReleaseCooperativeIdentity(ctx, tx, reservation, next.CooperativeRef)
		}
		return nil
	})
	if err != nil {
		return UserView{}, false, err
	}
	out, err := s.nativeTarget(ctx, a, id)
	return out, false, err
}

// NativeSetLocked khóa hoặc mở khóa tài khoản FN-07 (AC-07). Không tự khóa chính mình (BR-07); khóa thu hồi mọi
// phiên đang mở; dữ liệu đã nhập vẫn giữ, không xóa tài khoản (BR-06, DATA-03).
func (s *Service) NativeSetLocked(ctx context.Context, id uuid.UUID, locked bool, key string) (UserView, bool, error) {
	a, err := s.nativeAccountCaller(ctx)
	if err != nil {
		return UserView{}, false, err
	}
	return s.nativeSetLocked(ctx, a, id, locked, key)
}

func (s *Service) nativeSetLocked(ctx context.Context, a *actor, id uuid.UUID, locked bool, key string) (UserView, bool, error) {
	if id == a.p.ActorRef {
		return UserView{}, false, ErrSelfAction
	}
	target, err := s.nativeTarget(ctx, a, id)
	if err != nil {
		return UserView{}, false, err
	}
	if locked {
		// FN-TK-02/EX-04: khóa Đại diện HTX cuối cùng còn hoạt động của HTX phải được xác nhận.
		if err := s.guardLastRepresentative(ctx, a, target); err != nil {
			return UserView{}, false, err
		}
	}
	in := LockInput{UserID: id, IdemKey: key, Reason: nativeLockReason}
	var res Result
	if locked {
		res, err = s.lock(ctx, a, in)
	} else {
		res, err = s.unlock(ctx, a, in)
	}
	if err != nil {
		return UserView{}, false, err
	}
	out, err := s.nativeTarget(ctx, a, id)
	return out, res.Replayed, err
}
