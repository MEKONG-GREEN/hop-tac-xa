package store

// pf_accounts.login_probe (FN-01, ADR-0060): bộ đếm lần đăng nhập sai của mã định danh không khớp tài khoản nào.
// Khóa là HMAC của mã đã chuẩn hóa — không có tên đăng nhập hay SĐT ở dạng đọc được. Bảng ag:no-rls, chỉ qua hai hàm
// SECURITY DEFINER.

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/domain"
)

// LockLoginProbe khóa dòng bộ đếm của key (32 byte) trong giao dịch và trả trạng thái hiện tại (rỗng nếu chưa có).
func LockLoginProbe(ctx context.Context, tx pgx.Tx, key []byte) (domain.LoginProbe, error) {
	var (
		p            domain.LoginProbe
		last, locked *time.Time
	)
	if err := tx.QueryRow(ctx, `SELECT failures, last_failure_at, locked_until FROM pf_accounts.login_probe_lock($1)`, key).
		Scan(&p.Failures, &last, &locked); err != nil {
		return domain.LoginProbe{}, err
	}
	if last != nil {
		p.LastFailure = last.UTC()
	}
	if locked != nil {
		p.LockedUntil = locked.UTC()
	}
	return p, nil
}

// SaveLoginProbe ghi trạng thái bộ đếm của key (sau LockLoginProbe, cùng giao dịch).
func SaveLoginProbe(ctx context.Context, tx pgx.Tx, key []byte, p domain.LoginProbe) error {
	var last, locked *time.Time
	if !p.LastFailure.IsZero() {
		t := p.LastFailure.UTC()
		last = &t
	}
	if !p.LockedUntil.IsZero() {
		t := p.LockedUntil.UTC()
		locked = &t
	}
	_, err := tx.Exec(ctx, `SELECT pf_accounts.login_probe_save($1, $2, $3, $4)`, key, p.Failures, last, locked)
	return err
}
