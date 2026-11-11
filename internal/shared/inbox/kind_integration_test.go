package inbox_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/outbox"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
	"github.com/MEKONG-GREEN/hop-tac-xa/migrations"
)

func pgCode(err error) string {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return pe.Code
	}
	return ""
}

// Sự kiện nội bộ chỉ có tên mkg.* — kiểm ở Enqueue và ở CHECK DB (outbox_internal_is_mkg).
func TestOutboxInternalKindMustBeMKG(t *testing.T) {
	ctx := context.Background()
	env := pgtest.Get(t)
	app := env.Pool(t, migrations.PF, "worker_platform")
	tid := ids.New()
	err := tenant.WithTenantTx(ctx, app, tid, func(tx pgx.Tx) error {
		return outbox.Enqueue(ctx, tx, outbox.Event{ID: ids.New(), TenantID: tid, Type: "pf.party.consent.recorded",
			SchemaVersion: 1, OccurredAt: time.Now(), Producer: "pf/party", Payload: map[string]string{}, Kind: outbox.KindInternal})
	})
	if err == nil {
		t.Fatal("Enqueue phải từ chối sự kiện nội bộ không thuộc mkg")
	}
	err = tenant.WithTenantTx(ctx, app, tid, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO shared.outbox (id, tenant_id, event_type, schema_version, occurred_at, producer, payload, kind)
		  VALUES ($1, $2, 'pf.party.consent.recorded', 1, now(), 'pf/party', '{}', 'internal')`, ids.New(), tid)
		return err
	})
	if pgCode(err) != "23514" {
		t.Fatalf("CHECK outbox_internal_is_mkg phải chặn: %v", err)
	}
}
