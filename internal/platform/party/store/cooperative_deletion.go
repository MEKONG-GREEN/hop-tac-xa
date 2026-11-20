package store

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"strings"
)

type CooperativeDeletionUsage struct {
	Households, Accounts, PendingAccounts int64
	HasSharing, HasBusiness               bool
}

func LockCooperativeDeletion(ctx context.Context, tx pgx.Tx, ref uuid.UUID) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('cooperative-life:'||shared.current_tenant()::text||':'||$1::text,283))`, ref.String())
	return err
}

// Chỉ đọc metadata catalog và các view có hợp đồng của owner. View mới của
// owner mới cũng phải được đọc: thiếu quyền publication sẽ fail-closed.
func CooperativeDeletionReferences(ctx context.Context, tx pgx.Tx, ref uuid.UUID, iamUsers []uuid.UUID) (CooperativeDeletionUsage, error) {
	var uncovered bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM shared.unprotected_cooperative_references())`).Scan(&uncovered); err != nil {
		return CooperativeDeletionUsage{}, err
	}
	if uncovered {
		return CooperativeDeletionUsage{}, fmt.Errorf("cooperative deletion has unprotected reference sources")
	}
	rows, err := tx.Query(ctx, `SELECT n.nspname,c.relname FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
WHERE n.nspname LIKE 'pf\_%\_pub' AND c.relname='v1_cooperative_references' AND c.relkind='v' ORDER BY n.nspname`)
	if err != nil {
		return CooperativeDeletionUsage{}, err
	}
	var sources []string
	for rows.Next() {
		var schema, view string
		if err := rows.Scan(&schema, &view); err != nil {
			rows.Close()
			return CooperativeDeletionUsage{}, err
		}
		sources = append(sources, `SELECT category,record_ref FROM `+pgx.Identifier{schema, view}.Sanitize()+` WHERE tenant_id=shared.current_tenant() AND cooperative_ref=$1`)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return CooperativeDeletionUsage{}, err
	}
	if len(sources) == 0 {
		return CooperativeDeletionUsage{}, fmt.Errorf("cooperative deletion has no published sources")
	}
	sources = append(sources, `SELECT 'accounts'::text AS category,value AS record_ref FROM unnest($2::uuid[]) value`)
	query := `SELECT count(DISTINCT record_ref) FILTER(WHERE category='households'),
count(DISTINCT record_ref) FILTER(WHERE category='accounts'),
count(DISTINCT record_ref) FILTER(WHERE category='pending_accounts'),
coalesce(bool_or(category='sharing'),false),coalesce(bool_or(category='business'),false)
FROM (` + strings.Join(sources, ` UNION ALL `) + `) refs`
	userRefs := make([]string, 0, len(iamUsers))
	for _, id := range iamUsers {
		userRefs = append(userRefs, id.String())
	}
	var out CooperativeDeletionUsage
	err = tx.QueryRow(ctx, query, ref, userRefs).Scan(&out.Households, &out.Accounts, &out.PendingAccounts, &out.HasSharing, &out.HasBusiness)
	return out, err
}

func CooperativeWasDeleted(ctx context.Context, tx pgx.Tx, ref uuid.UUID) (bool, error) {
	var deleted bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pf_party.org_event WHERE org_ref=$1 AND action='deleted' AND kind='cooperative')`, ref).Scan(&deleted)
	return deleted, err
}

func RemoveCooperativeProjection(ctx context.Context, tx pgx.Tx, ref uuid.UUID) error {
	_, err := tx.Exec(ctx, `DELETE FROM pf_party.org WHERE ref=$1 AND kind='cooperative'`, ref)
	return err
}

func CooperativeDeletionCompleted(ctx context.Context, tx pgx.Tx, command uuid.UUID) (bool, error) {
	var completed bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pf_party.htx_deletion_completion WHERE command_ref=$1)`, command).Scan(&completed)
	return completed, err
}

func CompleteCooperativeDeletion(ctx context.Context, tx pgx.Tx, command uuid.UUID) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_party.htx_deletion_completion(tenant_id,command_ref) VALUES(shared.current_tenant(),$1) ON CONFLICT DO NOTHING`, command)
	return err
}
