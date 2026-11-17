package store

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// QueryExecModeExec (the production PgBouncer mode) encodes arguments as text
// without server-provided parameter OIDs. Exercise that driver boundary here.
type textProtocolTx struct{ pgx.Tx }

func (textProtocolTx) Query(_ context.Context, _ string, args ...any) (pgx.Rows, error) {
	for i, arg := range args {
		if _, err := pgtype.NewMap().Encode(0, pgtype.TextFormatCode, arg, nil); err != nil {
			return nil, fmt.Errorf("PgBouncer argument %d: %w", i, err)
		}
	}
	return emptyAreaRows{}, nil
}

type emptyAreaRows struct{ pgx.Rows }

func (emptyAreaRows) Close()     {}
func (emptyAreaRows) Err() error { return nil }
func (emptyAreaRows) Next() bool { return false }

func TestActiveAreasPgBouncerEncoding(t *testing.T) {
	_, err := ActiveAreas(context.Background(), textProtocolTx{}, uuid.New(), []uuid.UUID{uuid.New()})
	if err != nil {
		t.Fatal(err)
	}
}
