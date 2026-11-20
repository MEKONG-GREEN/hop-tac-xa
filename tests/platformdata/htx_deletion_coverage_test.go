package platformdata_test

import (
	"context"
	"testing"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/hop-tac-xa/migrations"
)

// Mỗi tham chiếu cấu trúc đến HTX phải tham gia chốt ghi/xóa. Không cho
// thêm bảng nghiệp vụ mới rồi âm thầm bỏ qua điều kiện DATA-03 khi xóa HTX.
func TestAH283DeletionReferenceCoverage(t *testing.T) {
	env := pgtest.Get(t)
	pool := env.AdminPool(t, migrations.PF)
	rows, err := pool.Query(context.Background(), `SELECT n.nspname,c.relname,a.attname
FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
JOIN pg_attribute a ON a.attrelid=c.oid AND a.attnum>0 AND NOT a.attisdropped
WHERE n.nspname LIKE 'pf\_%' AND c.relkind IN ('r','p') AND NOT c.relispartition
AND a.atttypid IN ('uuid'::regtype,'uuid[]'::regtype)
AND a.attname ~ '(org_ref|coop_ref|cooperative_ref|htx_ref|node_ref|node_id)s?$'
ORDER BY 1,2,3`)
	if err != nil {
		t.Fatal(err)
	}
	var references [][3]string
	for rows.Next() {
		var entry [3]string
		if err := rows.Scan(&entry[0], &entry[1], &entry[2]); err != nil {
			t.Fatal(err)
		}
		references = append(references, entry)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range references {
		t.Logf("reference %s.%s.%s", entry[0], entry[1], entry[2])
	}
	var coverage bool
	if err := pool.QueryRow(context.Background(), `SELECT to_regprocedure('shared.unprotected_cooperative_references()') IS NOT NULL`).Scan(&coverage); err != nil {
		t.Fatal(err)
	}
	if !coverage {
		t.Fatal("missing cooperative deletion reference coverage function")
	}
	rows, err = pool.Query(context.Background(), `SELECT source_schema,source_table,source_column FROM shared.unprotected_cooperative_references()`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var schema, table, column string
		if err := rows.Scan(&schema, &table, &column); err != nil {
			t.Fatal(err)
		}
		t.Errorf("unprotected/unpublished cooperative reference: %s.%s.%s", schema, table, column)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	// Nút hiện tại cũng phải có guard chống phục hồi snapshot đã xóa, dù
	// tên cột là ref chứ không có hậu tố *_org_ref để kiểm kê tự động.
	var lifecycle bool
	if err := pool.QueryRow(context.Background(), `SELECT EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid='pf_party.org'::regclass AND tgfoid='shared.guard_deleted_cooperative()'::regprocedure AND tgenabled IN ('O','A'))`).Scan(&lifecycle); err != nil || !lifecycle {
		t.Fatalf("missing organization resurrection guard: %v", err)
	}
}
