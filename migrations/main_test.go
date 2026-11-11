package migrations_test

import (
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/pgtest"
)

// TestMain: pgtest dựng cụm thử một lần cho cả gói (CI bắt buộc AG_TEST_PG_ADMIN_URL).
func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

func pgCode(err error) string {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return pe.Code
	}
	return ""
}
