package dbadmin

import (
	"context"
	"fmt"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/hop-tac-xa/migrations"
)

func roleNames(rs []Role) []string {
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		out = append(out, r.Name)
	}
	return out
}

func TestRolesForMemberOfIsCloned(t *testing.T) {
	migrator := slices.IndexFunc(Roles, func(r Role) bool { return r.Name == Migrator })
	before := slices.Clone(Roles[migrator].MemberOf)
	selected := RolesFor(migrations.Clusters)
	selected[migrator].MemberOf[0] = "changed"
	if !slices.Equal(Roles[migrator].MemberOf, before) {
		t.Fatal("sửa kết quả làm thay đổi Roles toàn cục")
	}
}

// G4: BootstrapClusters chỉ tạo database của cụm đã chọn (database kiểm thử riêng, dọn khi xong).
func TestBootstrapClustersOnlySelected_G4(t *testing.T) {
	admin := os.Getenv("AG_TEST_PG_ADMIN_URL")
	if admin == "" {
		t.Skip("không có AG_TEST_PG_ADMIN_URL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	suffix := fmt.Sprintf("bc%d", time.Now().UnixNano()%1_000_000_000)
	if err := BootstrapClusters(ctx, admin, suffix, []migrations.Cluster{migrations.TS}); err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, admin)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(context.Background()) }()
	t.Cleanup(func() {
		for _, c := range migrations.Clusters {
			_, _ = conn.Exec(context.Background(), "DROP DATABASE IF EXISTS "+ident(DatabaseName(c, suffix)))
		}
	})
	for _, c := range migrations.Clusters {
		var n int
		if err := conn.QueryRow(ctx, "SELECT count(*) FROM pg_database WHERE datname = $1", DatabaseName(c, suffix)).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if want := map[bool]int{true: 1, false: 0}[c == migrations.TS]; n != want {
			t.Errorf("database %s: %d (muốn %d)", DatabaseName(c, suffix), n, want)
		}
	}
	if err := BootstrapClusters(ctx, admin, suffix, nil); err == nil {
		t.Error("không có cụm phải lỗi")
	}
}

// W7.1 (R-DB-12, ADR-0038): mỗi module có schema có vai chủ own_<module> NOLOGIN, tạo trước ag_migrator (thành viên).
func TestOwnerRoles_W71(t *testing.T) {
	names := roleNames(Roles)
	i := slices.Index(names, Migrator)
	if i < 0 {
		t.Fatal("thiếu ag_migrator")
	}
	mig := Roles[i]
	for _, g := range ModuleGroups() {
		o := OwnerRole(g.Module)
		j := slices.Index(names, o)
		if j < 0 {
			t.Errorf("thiếu vai %s", o)
			continue
		}
		if r := Roles[j]; r.Login || r.BypassRLS || len(r.MemberOf) != 0 {
			t.Errorf("%s phải NOLOGIN, không BYPASSRLS, không thuộc vai nào: %+v", o, r)
		}
		if j > i || !slices.Contains(mig.MemberOf, o) {
			t.Errorf("ag_migrator phải là thành viên %s (tạo trước)", o)
		}
	}
	if OwnerRole("pf/party") != "own_party" || OwnerRole("mkg/measurement") != "own_measurement" {
		t.Error("tên vai chủ")
	}
}
