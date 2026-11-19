package masterdata_test

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/masterdata"
	partyport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/party/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tabular"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
	"github.com/MEKONG-GREEN/hop-tac-xa/migrations"
)

const managedUnitsPath = "/v1/masterdata/admin-unit-profiles"

func TestAH271_PastNamesAndDisabledParentPreserveScope(t *testing.T) {
	f := newFixture(t)
	admin := f.caller(f.seeded(), authz.RolePlatformAdmin)
	status, provinces := admin.do(f.app, http.MethodGet, "/v1/masterdata/admin-units?limit=1", "", nil)
	if status != 200 {
		t.Fatalf("provinces: %d %v", status, provinces)
	}
	p := provinces["items"].([]any)[0].(map[string]any)
	status, communes := admin.do(f.app, http.MethodGet, "/v1/masterdata/admin-units?limit=1&parent="+p["code"].(string), "", nil)
	if status != 200 {
		t.Fatalf("communes: %d %v", status, communes)
	}
	c := communes["items"].([]any)[0].(map[string]any)
	code := c["code"].(string)
	status, b := admin.do(f.admin, http.MethodPost, managedUnitsPath+"/"+code+"/revisions", "ah271-old-name", map[string]any{"name": "Tên quản trị mới AH271", "active": true, "expected_revision": 0})
	if status != 200 {
		t.Fatalf("amend legacy: %d %v", status, b)
	}
	past, err := f.mod.AdminUnits().Get(admin.ctx(), code, time.Now().AddDate(0, 0, -1))
	if err != nil || past.Name != c["name"] {
		t.Fatalf("historical label changed: %+v %v", past, err)
	}
	status, b = admin.do(f.admin, http.MethodPost, managedUnitsPath+"/"+p["code"].(string)+"/revisions", "ah271-disable-parent", map[string]any{"name": p["name"], "active": false, "expected_revision": 0})
	if status != 200 {
		t.Fatalf("disable parent: %d %v", status, b)
	}
	current, err := f.mod.AdminUnits().Get(admin.ctx(), code, time.Now())
	if err != nil || current.Name != "Tên quản trị mới AH271" || !current.Disabled {
		t.Fatalf("old reference must remain readable: %+v %v", current, err)
	}
	codes, err := f.mod.AdminUnits().Descendants(admin.ctx(), p["code"].(string), time.Now())
	if err != nil || !slices.Contains(codes, code) {
		t.Fatalf("operational switch cut geographic scope: %v %v", codes, err)
	}
	children, err := f.mod.AdminUnits().(partyport.AdminUnitTree).Children(admin.ctx(), p["code"].(string), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, child := range children {
		if child.Code == code {
			found = child.Disabled
		}
	}
	if !found {
		t.Fatal("geographic tree must retain disabled nodes for boundaries and old profiles")
	}
	status, b = admin.do(f.app, http.MethodGet, "/v1/masterdata/admin-units?parent="+p["code"].(string), "", nil)
	if status != 200 || len(b["items"].([]any)) != 0 {
		t.Fatalf("disabled parent picker: %d %v", status, b)
	}
}

func TestAH271_CatalogCRUDAndOldReferences(t *testing.T) {
	f := newFixture(t)
	tid := f.seeded()
	admin := f.caller(tid, authz.RolePlatformAdmin)
	create := func(name, level, parent, key string) map[string]any {
		t.Helper()
		status, b := admin.do(f.admin, http.MethodPost, managedUnitsPath, "ah271-"+key, map[string]any{"name": name, "level": level, "parent_code": parent, "active": true})
		if status != 201 {
			t.Fatalf("create %s: %d %v", name, status, b)
		}
		return b
	}
	province := create("Tỉnh thử AH271", "province", "", "p-create")
	p := province["code"].(string)
	commune := create("Xã Tân Hòa AH271", "commune", p, "c-create")
	c := commune["code"].(string)
	// Public consumers (Vault area_code/device key distribution) accept at most
	// 32 ASCII identifier characters, even though the legal catalog allows 40.
	for _, code := range []string{p, c} {
		if len(code) > 32 || strings.Trim(code, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789._-") != "" {
			t.Fatalf("generated administrative code cannot be used by identity consumers: %q", code)
		}
	}
	if p == c || p == "" || c == "" {
		t.Fatal("server must generate distinct codes")
	}
	for _, role := range []string{"coop_manager", "htx_member"} {
		reader := roleCaller(t, f, tid, role)
		status, b := reader.do(f.app, http.MethodGet, "/v1/masterdata/admin-units?parent="+p, "", nil)
		if status != 200 || len(b["items"].([]any)) != 1 {
			t.Fatalf("picker %s: %d %v", role, status, b)
		}
		status, _ = reader.do(f.admin, http.MethodPost, managedUnitsPath, "denied-"+role, map[string]any{"name": "Forbidden", "level": "province"})
		if status != 403 {
			t.Fatalf("%s can create: %d", role, status)
		}
		status, _ = reader.do(f.admin, http.MethodGet, managedUnitsPath, "", nil)
		if status != 403 {
			t.Fatalf("%s can manage: %d", role, status)
		}
	}
	status, b := admin.do(f.admin, http.MethodPost, managedUnitsPath, "duplicate", map[string]any{"name": " XÃ  TÂN HÒA AH271 ", "level": "commune", "parent_code": p})
	if status != 409 || b["code"] != "masterdata.admin_unit_name_taken" {
		t.Fatalf("Vietnamese duplicate: %d %v", status, b)
	}
	other := create("Tỉnh khác AH271", "province", "", "p-other")
	create("Xã Tân Hòa AH271", "commune", other["code"].(string), "same-name-other-parent")
	status, _ = admin.do(f.admin, http.MethodPost, managedUnitsPath, "no-parent", map[string]any{"name": "Xã thiếu tỉnh", "level": "commune"})
	if status != 422 {
		t.Fatalf("missing parent: %d", status)
	}
	status, _ = admin.do(f.admin, http.MethodPost, managedUnitsPath, "client-code", map[string]any{"name": "Xã sai", "level": "commune", "parent_code": p, "code": "CLIENT"})
	if status != 400 {
		t.Fatalf("client code: %d", status)
	}
	update := map[string]any{"name": "Xã đổi tên AH271", "active": false, "expected_revision": commune["revision"]}
	status, b = admin.do(f.admin, http.MethodPost, managedUnitsPath+"/"+c+"/revisions", "ah271-disable", update)
	if status != 200 {
		t.Fatalf("disable: %d %v", status, b)
	}
	status, picker := admin.do(f.app, http.MethodGet, "/v1/masterdata/admin-units?parent="+p, "", nil)
	if status != 200 || len(picker["items"].([]any)) != 0 {
		t.Fatalf("disabled still selectable: %d %v", status, picker)
	}
	status, old := admin.do(f.app, http.MethodGet, "/v1/masterdata/admin-units/"+c, "", nil)
	if status != 200 || old["name"] != "Xã đổi tên AH271" || old["active"] != false {
		t.Fatalf("old reference unreadable: %d %v", status, old)
	}
	status, replayed := admin.do(f.admin, http.MethodPost, managedUnitsPath+"/"+c+"/revisions", "ah271-disable", update)
	if status != 200 || replayed["revision"] != b["revision"] {
		t.Fatalf("retry: %d %v", status, replayed)
	}
	status, _ = admin.do(f.admin, http.MethodPost, managedUnitsPath+"/"+c+"/revisions", "ah271-stale", update)
	if status != 409 {
		t.Fatalf("stale revision: %d", status)
	}
	status, enabled := admin.do(f.admin, http.MethodPost, managedUnitsPath+"/"+c+"/revisions", "ah271-enable", map[string]any{"name": "Xã đổi lần hai cùng ngày", "active": true, "expected_revision": b["revision"]})
	if status != 200 || enabled["revision"] == b["revision"] {
		t.Fatalf("same day edits: %d %v", status, enabled)
	}
	if _, err := f.env.AdminPool(t, migrations.PF).Exec(context.Background(), "DELETE FROM shared.idempotency_key WHERE tenant_id=$1 AND idem_key=$2", tid, "ah271-disable"); err != nil {
		t.Fatal(err)
	}
	status, replayed = admin.do(f.admin, http.MethodPost, managedUnitsPath+"/"+c+"/revisions", "ah271-disable", update)
	if status != 200 || replayed["revision"] != b["revision"] {
		t.Fatalf("durable replay after later edit: %d %v", status, replayed)
	}
	if err := f.mod.RebuildUnitProfiles(context.Background(), tid); err != nil {
		t.Fatal(err)
	}
	if err := tenant.WithTenantTx(context.Background(), f.env.Pool(t, migrations.PF, masterdata.DBRole), tid, func(tx pgx.Tx) error {
		var name string
		var revision int64
		var active bool
		if err := tx.QueryRow(context.Background(), "SELECT name,revision,active FROM pf_masterdata.admin_unit_profile_current WHERE code=$1", c).Scan(&name, &revision, &active); err != nil {
			return err
		}
		if name != "Xã đổi lần hai cùng ngày" || !active || float64(revision) != enabled["revision"] {
			t.Fatalf("rebuilt projection: %s %d %v", name, revision, active)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	status, rebuilt := admin.do(f.admin, http.MethodGet, managedUnitsPath+"?parent="+p, "", nil)
	if status != 200 || len(rebuilt["items"].([]any)) != 1 {
		t.Fatalf("rebuild: %d %v", status, rebuilt)
	}
	foreign := f.caller(f.seeded(), authz.RolePlatformAdmin)
	status, _ = foreign.do(f.admin, http.MethodPost, managedUnitsPath+"/"+c+"/revisions", "ah271-foreign", map[string]any{"name": "Foreign", "active": true, "expected_revision": enabled["revision"]})
	if status != 404 {
		t.Fatalf("foreign tenant: %d", status)
	}
}

func TestAH271_ExcelPartialImportAndFileOrder(t *testing.T) {
	f := newFixture(t)
	admin := f.caller(f.seeded(), authz.RolePlatformAdmin)
	rows := [][]string{{"Tỉnh Excel AH271", "Tỉnh", ""}}
	for i := 1; i < 100; i++ {
		parent := "Tỉnh Excel AH271"
		if i == 50 || i == 99 {
			parent = ""
		}
		rows = append(rows, []string{fmt.Sprintf("Xã Excel %03d", i), "Xã", parent})
	}
	var file bytes.Buffer
	if err := tabular.WriteXLSX(&file, "ĐVHC", []string{"Tên", "Cấp", "Tỉnh / thành phố trực thuộc"}, rows); err != nil {
		t.Fatal(err)
	}
	status, b := admin.do(f.admin, http.MethodPost, "/v1/masterdata/admin-unit-imports", "ah271-excel-100", file.Bytes())
	if status != 200 || b["total"] != float64(100) || b["created"] != float64(98) || b["rejected"] != float64(2) {
		t.Fatalf("partial import: %d %v", status, b)
	}
	resultRows := b["rows"].([]any)
	for _, index := range []int{50, 99} {
		r := resultRows[index].(map[string]any)
		if r["status"] != "rejected" || r["reason"] == "" {
			t.Fatalf("row %d: %v", index, r)
		}
	}
	if _, err := f.env.AdminPool(t, migrations.PF).Exec(context.Background(), "DELETE FROM shared.idempotency_key WHERE tenant_id=$1 AND idem_key=$2", admin.tid, "ah271-excel-100"); err != nil {
		t.Fatal(err)
	}
	status, replayed := admin.do(f.admin, http.MethodPost, "/v1/masterdata/admin-unit-imports", "ah271-excel-100", file.Bytes())
	if status != 200 || replayed["import_id"] != b["import_id"] {
		t.Fatalf("import retry: %d %v", status, replayed)
	}
	csv := []byte("Tên,Cấp,Tỉnh / thành phố trực thuộc\nXã đến trước,Xã,Tỉnh đến sau\nTỉnh đến sau,Tỉnh,\nXã đến trước,Xã,Tỉnh đến sau\nXÃ ĐẾN TRƯỚC,Xã,Tỉnh đến sau\n")
	status, b = admin.do(f.admin, http.MethodPost, "/v1/masterdata/admin-unit-imports", "ah271-file-order", csv)
	if status != 200 || b["created"] != float64(2) || b["rejected"] != float64(2) {
		t.Fatalf("file order/duplicates: %d %v", status, b)
	}
	if _, err := f.env.AdminPool(t, migrations.PF).Exec(context.Background(), "DELETE FROM shared.idempotency_key WHERE tenant_id=$1 AND idem_key=$2", admin.tid, "ah271-excel-100"); err != nil {
		t.Fatal(err)
	}
	status, b = admin.do(f.admin, http.MethodPost, "/v1/masterdata/admin-unit-imports", "ah271-excel-100", csv)
	if status != 422 || b["code"] != "idempotency_key_reused" {
		t.Fatalf("reused key different file: %d %v", status, b)
	}
	status, _ = admin.do(f.admin, http.MethodPost, "/v1/masterdata/admin-unit-imports", "ah271-bad-file", []byte("name,level\nWrong,province\n"))
	if status != 422 {
		t.Fatalf("wrong template: %d", status)
	}
}
