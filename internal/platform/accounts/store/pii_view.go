package store

// pf_accounts.pii_view (AH-223, FN-TK-02/BR-04): sổ truy cập khi xem SĐT đầy đủ. Chỉ chứa mã tham chiếu.

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/domain"
)

// InsertPIIView ghi một dòng sổ; seq, recorded_at do DB tính (shared.fn_chain).
func InsertPIIView(ctx context.Context, tx pgx.Tx, v *domain.PIIView) error {
	return tx.QueryRow(ctx, `INSERT INTO pf_accounts.pii_view
	    (id, tenant_id, stream_key, user_ref, field, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	  RETURNING seq, recorded_at`,
		v.ID, v.TenantID, domain.PIIViewStreamKey(v.UserRef), v.UserRef, v.Field, v.EventTime.UTC(), v.ActorRef, v.IdemKey,
		domain.PIIViewRuleVersion).Scan(&v.Seq, &v.RecordedAt)
}
