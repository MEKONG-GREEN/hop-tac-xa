package store

// pf_accounts.login_otp_challenge và pf_accounts.login_mfa_session (ADR-0061): mã OTP email của đăng nhập hai bước và
// phiên Keycloak đã qua mã. Không có mã thô, email hay IP.

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// LoginOTPChallenge là một dòng pf_accounts.login_otp_challenge.
type LoginOTPChallenge struct {
	ID        uuid.UUID
	UserRef   uuid.UUID
	CodeHash  []byte
	CreatedAt time.Time
	ExpiresAt time.Time
	Attempts  int
	UsedAt    *time.Time
	ClosedAt  *time.Time
}

// Open cho biết mã còn dùng được lúc now: chưa dùng, chưa hủy, chưa hết hạn.
func (c LoginOTPChallenge) Open(now time.Time) bool {
	return c.UsedAt == nil && c.ClosedAt == nil && now.Before(c.ExpiresAt)
}

// LockLoginOTPUser khóa advisory theo tài khoản trong giao dịch: đếm, đóng và ghi mã không chạy song song.
func LockLoginOTPUser(ctx context.Context, tx pgx.Tx, user uuid.UUID) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('pf_accounts.login_otp:' || $1::text, 0))`, user)
	return err
}

// LoginOTPStats trả số mã đã tạo từ since và mã mới nhất (nil nếu chưa có) của tài khoản.
func LoginOTPStats(ctx context.Context, tx pgx.Tx, user uuid.UUID, since time.Time) (int, *LoginOTPChallenge, error) {
	var n int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM pf_accounts.login_otp_challenge WHERE user_ref = $1 AND created_at > $2`,
		user, since.UTC()).Scan(&n); err != nil {
		return 0, nil, err
	}
	c, err := scanLoginOTP(tx.QueryRow(ctx, loginOTPSelect+` WHERE user_ref = $1 ORDER BY created_at DESC, id DESC LIMIT 1`, user))
	return n, c, err
}

// InsertLoginOTP đóng mọi mã còn mở của tài khoản rồi ghi mã mới: chỉ mã mới nhất dùng được.
func InsertLoginOTP(ctx context.Context, tx pgx.Tx, tid uuid.UUID, c LoginOTPChallenge) error {
	if _, err := tx.Exec(ctx, `UPDATE pf_accounts.login_otp_challenge SET closed_at = $2
	  WHERE user_ref = $1 AND used_at IS NULL AND closed_at IS NULL`, c.UserRef, c.CreatedAt.UTC()); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO pf_accounts.login_otp_challenge (tenant_id, id, user_ref, code_hash, created_at, expires_at)
	  VALUES ($1, $2, $3, $4, $5, $6)`, tid, c.ID, c.UserRef, c.CodeHash, c.CreatedAt.UTC(), c.ExpiresAt.UTC())
	return err
}

// LoginOTPByID đọc và khóa (FOR UPDATE) một mã. Không có → nil.
func LoginOTPByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*LoginOTPChallenge, error) {
	return scanLoginOTP(tx.QueryRow(ctx, loginOTPSelect+` WHERE id = $1 FOR UPDATE`, id))
}

// FailLoginOTP ghi một lần nhập sai; đủ maxAttempts thì hủy mã. Trả số lần sai sau khi ghi.
func FailLoginOTP(ctx context.Context, tx pgx.Tx, id uuid.UUID, maxAttempts int, at time.Time) (int, error) {
	var n int
	err := tx.QueryRow(ctx, `UPDATE pf_accounts.login_otp_challenge
	  SET attempts = attempts + 1, closed_at = CASE WHEN attempts + 1 >= $2 THEN $3 ELSE closed_at END
	  WHERE id = $1 AND used_at IS NULL AND closed_at IS NULL RETURNING attempts`, id, maxAttempts, at.UTC()).Scan(&n)
	return n, err
}

// CloseLoginOTP hủy mã chưa dùng (gửi email thất bại).
func CloseLoginOTP(ctx context.Context, tx pgx.Tx, id uuid.UUID, at time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE pf_accounts.login_otp_challenge SET closed_at = $2
	  WHERE id = $1 AND used_at IS NULL AND closed_at IS NULL`, id, at.UTC())
	return err
}

// UseLoginOTP đánh dấu mã đã dùng và ghi phiên Keycloak đã qua mã trong cùng giao dịch.
func UseLoginOTP(ctx context.Context, tx pgx.Tx, tid uuid.UUID, c LoginOTPChallenge, sessionID string, at, sessionExpires time.Time) error {
	tag, err := tx.Exec(ctx, `UPDATE pf_accounts.login_otp_challenge SET used_at = $2
	  WHERE id = $1 AND used_at IS NULL AND closed_at IS NULL`, c.ID, at.UTC())
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return errors.New("store: mã OTP không còn mở")
	}
	_, err = tx.Exec(ctx, `INSERT INTO pf_accounts.login_mfa_session (tenant_id, session_id, user_ref, challenge_id, verified_at, expires_at)
	  VALUES ($1, $2, $3, $4, $5, $6)`, tid, sessionID, c.UserRef, c.ID, at.UTC(), sessionExpires.UTC())
	return err
}

// LoginMFASessionValid cho biết phiên Keycloak sessionID của tài khoản user đã qua mã email và còn hạn lúc now.
func LoginMFASessionValid(ctx context.Context, tx pgx.Tx, sessionID string, user uuid.UUID, now time.Time) (bool, error) {
	var ok bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pf_accounts.login_mfa_session
	  WHERE session_id = $1 AND user_ref = $2 AND expires_at > $3)`, sessionID, user, now.UTC()).Scan(&ok)
	return ok, err
}

const loginOTPSelect = `SELECT id, user_ref, code_hash, created_at, expires_at, attempts, used_at, closed_at
  FROM pf_accounts.login_otp_challenge`

func scanLoginOTP(row pgx.Row) (*LoginOTPChallenge, error) {
	var c LoginOTPChallenge
	err := row.Scan(&c.ID, &c.UserRef, &c.CodeHash, &c.CreatedAt, &c.ExpiresAt, &c.Attempts, &c.UsedAt, &c.ClosedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}
