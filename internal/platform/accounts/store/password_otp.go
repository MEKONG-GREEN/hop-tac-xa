package store

// pf_accounts.password_otp_challenge (AH-280, ADR-0063): mã OTP đặt lại mật khẩu gửi qua SMS. Không có mã thô, số
// điện thoại hay IP. Dùng lại kiểu LoginOTPChallenge vì cùng cột và cùng quy tắc mở/hết hạn.

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// LockPasswordOTPUser khóa advisory theo tài khoản trong giao dịch: đếm, đóng và ghi mã không chạy song song.
func LockPasswordOTPUser(ctx context.Context, tx pgx.Tx, user uuid.UUID) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('pf_accounts.password_otp:' || $1::text, 0))`, user)
	return err
}

// PasswordOTPStats trả số mã đã tạo từ since và mã mới nhất (nil nếu chưa có) của tài khoản.
func PasswordOTPStats(ctx context.Context, tx pgx.Tx, user uuid.UUID, since time.Time) (int, *LoginOTPChallenge, error) {
	var n int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM pf_accounts.password_otp_challenge WHERE user_ref = $1 AND created_at > $2`,
		user, since.UTC()).Scan(&n); err != nil {
		return 0, nil, err
	}
	c, err := scanLoginOTP(tx.QueryRow(ctx, resetOTPSelect+` WHERE user_ref = $1 ORDER BY created_at DESC, id DESC LIMIT 1`, user))
	return n, c, err
}

// InsertPasswordOTP đóng mọi mã còn mở của tài khoản rồi ghi mã mới: chỉ mã mới nhất dùng được.
func InsertPasswordOTP(ctx context.Context, tx pgx.Tx, tid uuid.UUID, c LoginOTPChallenge) error {
	if _, err := tx.Exec(ctx, `UPDATE pf_accounts.password_otp_challenge SET closed_at = $2
	  WHERE user_ref = $1 AND used_at IS NULL AND closed_at IS NULL`, c.UserRef, c.CreatedAt.UTC()); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO pf_accounts.password_otp_challenge (tenant_id, id, user_ref, code_hash, created_at, expires_at)
	  VALUES ($1, $2, $3, $4, $5, $6)`, tid, c.ID, c.UserRef, c.CodeHash, c.CreatedAt.UTC(), c.ExpiresAt.UTC())
	return err
}

// PasswordOTPByID đọc và khóa (FOR UPDATE) một mã. Không có → nil.
func PasswordOTPByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*LoginOTPChallenge, error) {
	return scanLoginOTP(tx.QueryRow(ctx, resetOTPSelect+` WHERE id = $1 FOR UPDATE`, id))
}

// FailPasswordOTP ghi một lần nhập sai; đủ maxAttempts thì hủy mã. Trả số lần sai sau khi ghi.
func FailPasswordOTP(ctx context.Context, tx pgx.Tx, id uuid.UUID, maxAttempts int, at time.Time) (int, error) {
	var n int
	err := tx.QueryRow(ctx, `UPDATE pf_accounts.password_otp_challenge
	  SET attempts = attempts + 1, closed_at = CASE WHEN attempts + 1 >= $2 THEN $3 ELSE closed_at END
	  WHERE id = $1 AND used_at IS NULL AND closed_at IS NULL RETURNING attempts`, id, maxAttempts, at.UTC()).Scan(&n)
	return n, err
}

// ClosePasswordOTP hủy mã chưa dùng (gửi SMS thất bại, đặt mật khẩu thất bại).
func ClosePasswordOTP(ctx context.Context, tx pgx.Tx, id uuid.UUID, at time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE pf_accounts.password_otp_challenge SET closed_at = $2
	  WHERE id = $1 AND used_at IS NULL AND closed_at IS NULL`, id, at.UTC())
	return err
}

// UsePasswordOTP đánh dấu mã đã dùng; false nếu mã không còn mở (dùng song song).
func UsePasswordOTP(ctx context.Context, tx pgx.Tx, id uuid.UUID, at time.Time) (bool, error) {
	tag, err := tx.Exec(ctx, `UPDATE pf_accounts.password_otp_challenge SET used_at = $2
	  WHERE id = $1 AND used_at IS NULL AND closed_at IS NULL`, id, at.UTC())
	return tag.RowsAffected() == 1, err
}

const resetOTPSelect = `SELECT id, user_ref, code_hash, created_at, expires_at, attempts, used_at, closed_at
  FROM pf_accounts.password_otp_challenge`
