package store

// pf_accounts.data_consent (AH-23, REQ-011): sổ chỉ ghi thêm bằng chứng đồng ý thu thập dữ liệu. Chỉ chứa mã tài khoản.

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ledger"
)

// LockDataConsent khóa advisory theo tài khoản trong giao dịch: hai lần bấm Đồng ý song song không ghi hai dòng.
func LockDataConsent(ctx context.Context, tx pgx.Tx, user uuid.UUID) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('pf_accounts.data_consent:' || $1::text, 0))`, user)
	return err
}

// InsertDataConsent ghi một dòng sổ; seq, recorded_at do DB tính (shared.fn_chain). Người ghi chính là người đồng ý.
func InsertDataConsent(ctx context.Context, tx pgx.Tx, c *domain.DataConsent) error {
	return tx.QueryRow(ctx, `INSERT INTO pf_accounts.data_consent
	    (id, tenant_id, stream_key, user_ref, text_version, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $4, $7, $8)
	  RETURNING seq, recorded_at`,
		c.ID, c.TenantID, domain.DataConsentStreamKey(c.UserRef), c.UserRef, c.TextVersion, c.EventTime.UTC(), c.IdemKey,
		domain.DataConsentRuleVersion).Scan(&c.Seq, &c.RecordedAt)
}

const dataConsentCols = `id, tenant_id, user_ref, text_version, event_time, idem_key, seq, recorded_at`

func scanDataConsent(row pgx.Row) (*domain.DataConsent, error) {
	var c domain.DataConsent
	err := row.Scan(&c.ID, &c.TenantID, &c.UserRef, &c.TextVersion, &c.EventTime, &c.IdemKey, &c.Seq, &c.RecordedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// DataConsentFor trả lần đồng ý sớm nhất của tài khoản với một phiên bản nội dung; chưa có → nil.
func DataConsentFor(ctx context.Context, tx pgx.Tx, user uuid.UUID, version string) (*domain.DataConsent, error) {
	return scanDataConsent(tx.QueryRow(ctx, `SELECT `+dataConsentCols+` FROM pf_accounts.data_consent
	  WHERE user_ref = $1 AND text_version = $2 ORDER BY seq LIMIT 1`, user, version))
}

// DataConsentByIdemKey tìm dòng sổ đã ghi với idem_key (gọi lặp); chưa có → nil.
func DataConsentByIdemKey(ctx context.Context, tx pgx.Tx, key string) (*domain.DataConsent, error) {
	return scanDataConsent(tx.QueryRow(ctx, `SELECT `+dataConsentCols+` FROM pf_accounts.data_consent WHERE idem_key = $1`+
		ledger.NewestByIdem(""), key))
}
