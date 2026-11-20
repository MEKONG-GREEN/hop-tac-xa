package store

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func NativeCooperativeScopeReady(ctx context.Context, tx pgx.Tx, ref uuid.UUID) (bool, error) {
	var ready bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM k_ref.org o JOIN k_ref.node_closure n
 ON n.tenant_id=o.tenant_id AND n.ancestor=o.ref AND n.descendant=o.ref AND n.depth=0
 WHERE o.tenant_id=shared.current_tenant() AND o.ref=$1 AND o.kind='cooperative' AND o.active)`, ref).Scan(&ready)
	return ready, err
}
