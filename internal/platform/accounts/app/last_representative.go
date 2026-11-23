package app

import (
	"context"
	"net/http"
	"slices"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
)

// FN-TK-02/EX-04 (AH-223): thao tác làm một HTX không còn Đại diện HTX nào đang hoạt động — khóa tài khoản Đại diện HTX
// hoặc chuyển tài khoản đó sang HTX khác — phải được người dùng xác nhận. Đây là bước hỏi lại trên giao diện, không phải
// bất biến dữ liệu: hai Admin cùng lúc khóa hai Đại diện cuối cùng vẫn có thể để HTX trống.

// MsgLastRepresentative là câu hỏi xác nhận nguyên văn BRD.
const MsgLastRepresentative = "HTX sẽ không còn người duyệt kết quả lô"

// ErrLastRepresentative: chưa xác nhận; gửi lại với confirm_no_representative=true (khóa Idempotency-Key mới).
var ErrLastRepresentative = problem.New(http.StatusConflict, "accounts.last_representative", MsgLastRepresentative)

type lastRepresentativeKey struct{}

// WithLastRepresentativeConfirmed đánh dấu request đã được người dùng xác nhận EX-04 (handler đặt từ query).
func WithLastRepresentativeConfirmed(ctx context.Context) context.Context {
	return context.WithValue(ctx, lastRepresentativeKey{}, true)
}

func lastRepresentativeConfirmed(ctx context.Context) bool {
	v, _ := ctx.Value(lastRepresentativeKey{}).(bool)
	return v
}

// lastActiveRepresentative cho biết target là Đại diện HTX đang hoạt động duy nhất của HTX target.User.OrgRef.
func lastActiveRepresentative(users []UserView, target UserView) bool {
	active := func(u UserView) bool {
		return u.User.Enabled && u.User.OrgRef != uuid.Nil && slices.Contains(u.Roles, htxroles.Representative)
	}
	if !active(target) {
		return false
	}
	return !slices.ContainsFunc(users, func(u UserView) bool {
		return u.User.ID != target.User.ID && u.User.OrgRef == target.User.OrgRef && active(u)
	})
}

// guardLastRepresentative trả ErrLastRepresentative khi thao tác sẽ để HTX của target không còn Đại diện HTX đang hoạt
// động mà request chưa xác nhận. Chỉ đọc danh sách IAM khi target là Đại diện HTX đang hoạt động.
func (s *Service) guardLastRepresentative(ctx context.Context, a *actor, target UserView) error {
	if lastRepresentativeConfirmed(ctx) || !target.User.Enabled || !slices.Contains(target.Roles, htxroles.Representative) {
		return nil
	}
	users, err := s.nativeUsers(ctx, a)
	if err != nil {
		return err
	}
	if lastActiveRepresentative(users, target) {
		return ErrLastRepresentative.WithDetail(MsgLastRepresentative + ". Vẫn tiếp tục?")
	}
	return nil
}
