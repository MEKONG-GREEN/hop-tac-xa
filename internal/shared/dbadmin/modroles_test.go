package dbadmin

import (
	"io/fs"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/modreg"
	"github.com/MEKONG-GREEN/hop-tac-xa/migrations"
)

func pubRoles() []string {
	var out []string
	for _, g := range ModuleGroups() {
		if g.Pub != "" {
			out = append(out, g.Pub)
		}
	}
	return out
}

// W6.3 (§14, ADR-0031 §2, R-DB-10): r_<module>_pub NOLOGIN cho đúng các module sở hữu schema _pub, tạo trước
// vai phân tích; chỉ r_metrics (cap/analytics), r_agg và bên đọc đã rà soát (consumes.publications, ADR-0053) là thành viên;
// không thuộc ag_shared_user, không vượt RLS. PFPUB: + r_forms_pub (pf_forms_pub, pf 20261013150108).
func TestPubRoles_W63(t *testing.T) {
	want := []string{"r_cooperative_pub", "r_cropcycle_pub", "r_extension_pub", "r_forms_pub", "r_geo_pub", "r_masterdata_pub", "r_mechanization_pub", "r_party_pub", "r_price_pub"}
	if got := pubRoles(); !slices.Equal(got, want) {
		t.Fatalf("vai _pub: %v, muốn %v", got, want)
	}
	_, ai := roleByName(t, modreg.DBRole(AnalyticsModule))
	for _, name := range want {
		r, ri := roleByName(t, name)
		if r.Login || r.BypassRLS || r.NoInherit || len(r.MemberOf) != 0 || !slices.Equal(r.Clusters, []migrations.Cluster{migrations.PF}) || ri > ai {
			t.Errorf("%s: %+v (vị trí %d, r_metrics %d)", name, r, ri, ai)
		}
		for _, o := range Roles {
			if slices.Contains(o.MemberOf, name) && o.Name != modreg.DBRole(AnalyticsModule) && o.Name != AggRole && !pubReader(o.Name, name) {
				t.Errorf("%s không được là thành viên %s", o.Name, name)
			}
		}
	}
}

// W6.4 (ADR-0031 §3, R-DB-11): r_agg NOLOGIN BYPASSRLS INHERIT, thành viên đúng mọi r_<module>_pub (không
// ag_shared_user, không vai module nào), tạo sau vai _pub và trước vai đăng nhập; thành viên DUY NHẤT là vai đăng nhập
// riêng app_agg (gói FC) — NOINHERIT, chỉ thuộc r_agg, mặc định chỉ đọc + statement_timeout; không app_<đơn vị> nào
// thuộc r_agg. Không vai BYPASSRLS mới nào khác.
func TestAggRole_W64(t *testing.T) {
	r, ri := roleByName(t, AggRole)
	if r.Login || !r.BypassRLS || r.NoInherit || !slices.Equal(r.MemberOf, pubRoles()) ||
		!slices.Equal(r.Clusters, []migrations.Cluster{migrations.PF}) {
		t.Errorf("r_agg: %+v", r)
	}
	for _, p := range pubRoles() {
		if _, pi := roleByName(t, p); pi > ri {
			t.Errorf("%s tạo sau r_agg", p)
		}
	}
	var members, bypass []string
	for i, o := range Roles {
		if slices.Contains(o.MemberOf, AggRole) {
			members = append(members, o.Name)
			if !o.Login || !o.NoInherit || i < ri {
				t.Errorf("%s: thành viên r_agg phải là vai đăng nhập NOINHERIT tạo sau r_agg: %+v", o.Name, o)
			}
		}
		if o.BypassRLS {
			bypass = append(bypass, o.Name)
		}
	}
	if !slices.Equal(members, []string{AppAgg}) || !slices.Equal(ExclusiveMembers[AggRole], members) {
		t.Errorf("thành viên r_agg: %v, muốn [app_agg] (ExclusiveMembers %v)", members, ExclusiveMembers[AggRole])
	}
	a, _ := roleByName(t, AppAgg)
	if !slices.Equal(a.MemberOf, []string{AggRole}) || a.BypassRLS || !slices.Equal(a.Clusters, []migrations.Cluster{migrations.PF}) ||
		a.Settings["default_transaction_read_only"] != "on" || a.Settings["statement_timeout"] == "" || !slices.Contains(LoginRoles(), AppAgg) {
		t.Errorf("app_agg: %+v", a)
	}
	slices.Sort(bypass)
	// ADR-0031 §6: danh mục vai BYPASSRLS.
	if want := []string{OpsJobs, OutboxPublisher, MKGMonitorOwner, AggRole, TSMonitorOwner}; !slices.Equal(bypass, want) {
		t.Errorf("vai BYPASSRLS: %v, muốn %v", bypass, want)
	}
}

func roleByName(t *testing.T, name string) (Role, int) {
	t.Helper()
	for i, r := range Roles {
		if r.Name == name {
			return r, i
		}
	}
	t.Fatalf("thiếu vai %s", name)
	return Role{}, -1
}

// W2.6: mọi đơn vị triển khai/lệnh ghép module có vai đăng nhập theo đơn vị, và vai đó là thành viên r_* của
// MỌI module có schema mà nó ghép (không còn pool vai <module>_app nào trong cmd/*).
func TestEveryUnitHasLogin_ADR0028(t *testing.T) {
	covered := map[string]LoginUnit{}
	for _, u := range LoginUnits {
		covered[u.Unit] = u
		for _, tool := range slices.Concat(u.Tools, u.Aliases) {
			covered[tool] = u
		}
	}
	for _, unit := range []string{"api-platform", "integration-gateway", "api-public", "api-internal", "worker", "worker-internal", "ingest",
		"identity-vault", "drill", "formimport", "rebuilddrill", "objsync"} {
		u, ok := covered[unit]
		if !ok {
			t.Errorf("cmd/%s chưa có vai đăng nhập đơn vị", unit)
			continue
		}
		r, _ := roleByName(t, u.Role)
		for _, m := range modreg.UnitModules(unit) {
			e, _ := modreg.Entry(m)
			if len(e.OwnsSchema) > 0 && !slices.Contains(r.MemberOf, modreg.DBRole(m)) {
				t.Errorf("%s (cmd/%s) thiếu %s", u.Role, unit, modreg.DBRole(m))
			}
		}
	}
	// Số bí mật đăng nhập mỗi cụm (ADR-0028 §4: 4–6, + ag_ops_jobs của rà soát 08/10) — vai app_* + ag_migrator +
	// ag_outbox_publisher + ag_ops_jobs + giám sát.
	for _, c := range migrations.Clusters {
		n := 0
		for _, r := range Roles {
			if !r.Login || !slices.Contains(r.Clusters, c) {
				continue
			}
			if slices.Contains(LoginRoles(), r.Name) || r.Name == Migrator || r.Name == OutboxPublisher || r.Name == OpsJobs || r.Name == MKGMonitor || r.Name == TSMonitor {
				n++
			}
		}
		// pf: + app_agg (ADR-0031 §3, gói FC) → 8.
		if n < 4 || n > 8 {
			t.Errorf("cụm %s: %d vai đăng nhập (muốn 4–8)", c, n)
		}
	}
}

var pairRe = regexp.MustCompile(`\['([a-z_]+)', '([a-z_]+)'(?:, '([a-z]+)')?\]`)

// legacyGroups: vai nhóm của module có vai cũ <module>_app.
func legacyGroups() []ModuleGroup {
	var out []ModuleGroup
	for _, g := range ModuleGroups() {
		if g.Legacy != "" {
			out = append(out, g)
		}
	}
	return out
}

func legacyGroupsIn(c migrations.Cluster) []ModuleGroup {
	var out []ModuleGroup
	for _, g := range ClusterGroups(c) {
		if g.Legacy != "" {
			out = append(out, g)
		}
	}
	return out
}

type migPair struct{ legacy, group, how string }

func migrationPairs(t *testing.T, file string) []migPair {
	t.Helper()
	src, err := fs.ReadFile(migrations.FS, file)
	if err != nil {
		t.Fatal(err)
	}
	var got []migPair
	for _, m := range pairRe.FindAllStringSubmatch(string(src), -1) {
		got = append(got, migPair{m[1], m[2], m[3]})
	}
	return got
}

// pf 000090 chép quyền theo danh sách cặp ghi tay — đúng 17 module pf có vai cũ lúc W2.3.
func TestMigration90MatchesModuleGroups(t *testing.T) {
	got := migrationPairs(t, "pf/000090_pf_module_group_roles.up.sql")
	groups := legacyGroupsIn(migrations.PF)
	if len(got) != len(groups) {
		t.Fatalf("000090 có %d cặp, ModuleGroups %d", len(got), len(groups))
	}
	for i, g := range groups {
		if got[i].legacy != g.Legacy || got[i].group != g.Role {
			t.Errorf("cặp %d: %+v, muốn %s → %s", i, got[i], g.Legacy, g.Role)
		}
	}
}

func migrationRoles(t *testing.T, file string) []string {
	t.Helper()
	src, err := fs.ReadFile(migrations.FS, file)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, m := range regexp.MustCompile(`'(r_[a-z_]+)'`).FindAllStringSubmatch(string(src), -1) {
		got = append(got, m[1])
	}
	return got
}

func roles(gs []ModuleGroup) []string {
	var out []string
	for _, g := range gs {
		out = append(out, g.Role)
	}
	return out
}

// pf 000091 (W2.4) cấp EXECUTE k_ref.ctx_* cho danh sách r_* ghi tay — khớp các vai nhóm pf có vai cũ.
func TestMigration91MatchesModuleGroups(t *testing.T) {
	if got, want := migrationRoles(t, "pf/000091_pf_k_ref_ctx_functions.up.sql"), roles(legacyGroupsIn(migrations.PF)); !slices.Equal(got, want) {
		t.Fatalf("000091 cấp cho %v, ModuleGroups %v", got, want)
	}
}

// laterGroups: module có vai nhóm sinh SAU pf 000092 — migration của chính module cấp k_ref cho r_* của nó.
var laterGroups = map[string]string{
	"pf/guestlink":   "pf/20261102151657_pf_guestlink_core.up.sql", // W10.18: generic dormant capability, tự cấp k_ref
	"pf/workflow":    "pf/000118_pf_workflow.up.sql",
	"pf/forms":       "pf/000122_pf_forms_grants.up.sql",
	"pf/evidence":    "pf/000123_pf_evidence.up.sql",
	"pf/cooperative": "pf/000131_pf_cooperative_schema.up.sql",        // W3.3: vai r_cooperative sinh khi module có owns_schema
	"pf/price":       "pf/000138_pf_price.up.sql",                     // W11.71 (ADR-0049): khung cap/price
	"pf/hrm":         "pf/000147_pf_hrm_catalog.up.sql",               // W11.21 (ADR-0050): dom/hrm, danh mục nhân sự
	"pf/contact":     "pf/20261013230912_pf_contact_directory.up.sql", // W11.23: cap/directory, Contact (FN-13)
	"pf/export":      "pf/20261024090957_pf_export_docx.up.sql",       // W10.17 (ADR-0051): cap/export, mẫu văn bản + .docx
}

// pf 000092 (W2.5) cấp SELECT k_ref.* cho MỌI r_* của module pf lúc đó (R-DB-05) — danh sách ghi tay khớp
// ModuleGroups cụm pf trừ module sinh sau (laterGroups).
func TestMigration92MatchesModuleGroups(t *testing.T) {
	var want []ModuleGroup
	for _, g := range ClusterGroups(migrations.PF) {
		if _, later := laterGroups[g.Module]; !later && strings.HasPrefix(g.Module, "pf/") {
			want = append(want, g)
		}
	}
	if got := migrationRoles(t, "pf/000092_pf_kref_relations.up.sql"); !slices.Equal(got, roles(want)) {
		t.Fatalf("000092 cấp cho %v, ModuleGroups %v", got, roles(want))
	}
}

// Module sinh sau 000092 phải tự cấp k_ref (docs/dev/modules.md, vai DB bước 5).
func TestLaterGroupsGrantKRef(t *testing.T) {
	for m, file := range laterGroups {
		src, err := fs.ReadFile(migrations.FS, file)
		if err != nil {
			t.Fatal(err)
		}
		role := modreg.DBRole(m)
		for _, want := range []string{"GRANT USAGE ON SCHEMA k_ref TO " + role, "k_ref.ctx_uuid(text) TO " + role,
			"k_ref.share_scope TO " + role} {
			if !regexp.MustCompile(regexp.QuoteMeta(want)).Match(src) {
				t.Errorf("%s: thiếu %q", file, want)
			}
		}
	}
}

// pubReader: vai nhóm reader là thành viên vai _pub theo khai báo hợp đồng (ADR-0053 §4).
func pubReader(reader, pub string) bool {
	for _, entry := range modreg.Catalog() {
		if modreg.DBRole(entry.Name) != reader {
			continue
		}
		for _, o := range entry.PubReadOwners {
			if modreg.PubRole(o) == pub {
				return true
			}
		}
	}
	return false
}

// ADR-0053 §4 (PFPUB): KHTC trong pf/extension đọc họ hợp đồng closed_figures/v1 của hai nguồn — r_extension là thành
// viên các chủ công bố đã duyệt: closed figures/giá và HTX/cây tổ chức; khai báo chỉ trỏ
// module có schema _pub.
func TestPubReaders_PFPUB(t *testing.T) {
	r, ri := roleByName(t, "r_extension")
	for _, pub := range []string{"r_cooperative_pub", "r_extension_pub", "r_forms_pub", "r_masterdata_pub", "r_party_pub", "r_price_pub"} {
		p, pi := roleByName(t, pub)
		if !slices.Contains(r.MemberOf, pub) || pi > ri || p.BypassRLS {
			t.Errorf("r_extension phải là thành viên %s (tạo trước): %+v", pub, r.MemberOf)
		}
	}
	for _, m := range r.MemberOf {
		if m != SharedUser && m != "r_extension_pub" && m != "r_forms_pub" && m != "r_price_pub" && m != "r_cooperative_pub" && m != "r_party_pub" && m != "r_masterdata_pub" {
			t.Errorf("r_extension thành viên thừa %s", m)
		}
	}
	groups := ModuleGroups()
	for _, entry := range modreg.Catalog() {
		if got := pubReaderRoles(entry.Name, groups); len(got) != len(entry.PubReadOwners) {
			t.Errorf("%s: chủ khai báo không có vai _pub (%v / %v)", entry.Name, entry.PubReadOwners, got)
		}
	}
}

// Một module mới chỉ cần khai báo hợp đồng; tên reader không chọn mặc định hay fallback.
func TestPublicationReaderRolesFollowDeclaration(t *testing.T) {
	groups := []ModuleGroup{
		{Module: "pf/alpha", Pub: "r_alpha_pub"},
		{Module: "pf/beta", Pub: "r_beta_pub"},
		{Module: "pf/other", Pub: "r_other_pub"},
		{Module: "pf/extension", Pub: "r_extension_pub"},
		{Module: "pf/forms", Pub: "r_forms_pub"},
		{Module: "pf/price", Pub: "r_price_pub"},
	}
	reader := modreg.CatalogEntry{Name: "pf/example", PubReadOwners: []string{"pf/beta", "pf/alpha"}}
	if got, want := declaredPubReaderRoles(reader, groups), []string{"r_beta_pub", "r_alpha_pub"}; !slices.Equal(got, want) {
		t.Fatalf("vai theo khai báo = %v, muốn %v", got, want)
	}
	for _, name := range []string{"pf/extension", "pf/example", "pf/other"} {
		if got := declaredPubReaderRoles(modreg.CatalogEntry{Name: name}, groups); len(got) != 0 {
			t.Errorf("%s không khai nhưng được cấp %v", name, got)
		}
	}
	got := declaredPubReaderRoles(reader, groups)
	got[0] = "changed"
	if reader.PubReadOwners[0] != "pf/beta" || groups[1].Pub != "r_beta_pub" {
		t.Fatal("kết quả làm thay đổi metadata đầu vào")
	}
	if got := pubReaderRoles("pf/unregistered", groups); len(got) != 0 {
		t.Fatalf("module không đăng ký được cấp %v", got)
	}
}
