package handler_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
	port "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/port"
	syncport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tabular"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
)

// Ma trận Mục VI BRD Kế hoạch mùa vụ (dòng KHMV-* của contracts/htxroles) trên API vụ mùa và cơ sở dữ liệu thật
// (AH-39, NFR-05). Phạm vi theo HTX gắn với tài khoản (KH-02), không theo quan hệ Đại diện – Thành viên – nông hộ.

// khmvData là dữ liệu gieo sẵn: HTX A (dữ liệu "của mình") và HTX B (dữ liệu HTX khác).
type khmvData struct {
	sop            string // SOP hệ thống
	sopB           string // SOP riêng của HTX B
	s1, s2, s5, s6 string // vụ HTX A: p1 (đọc, sửa, nhật ký), p2 (hủy), p5 (khai sản lượng), p6 (đã khai — sửa sản lượng)
	l1             string // nhật ký HTX A trên p1
	h6             string // khai báo sản lượng HTX A trên p6
	sb, lb, hb     string // vụ, nhật ký, khai báo của HTX B (p4)
}

// khmvSeed gieo dữ liệu bằng Admin (ghi được mọi HTX) rồi dời tới cửa sổ thu hoạch. Cổng thửa giả chỉ trả thửa cho
// đúng tenant, như cổng thật dưới RLS.
func khmvSeed(t *testing.T, f *fixture) khmvData {
	t.Helper()
	var d khmvData
	d.sop, _ = f.publishedSOP("SOP OM5451", "OM5451")
	p6 := ids.New()
	f.plots.put(port.Plot{Ref: p6, Code: "TH0000006", CommuneCode: "31117", CooperativeRef: f.coopA, HouseholdRef: f.h2,
		Status: port.PlotActive, Version: 1, AreaHa: decimal.RequireFromString("1.5000")}, 10)
	f.plots.tenant = f.tenant
	adm := f.platformAdmin()
	d.s1, d.s2, d.s5, d.s6, d.sb = adm.mustOpen(f.p1), adm.mustOpen(f.p2), adm.mustOpen(f.p5), adm.mustOpen(p6), adm.mustOpen(f.p4)
	l := adm.log(f.p1, map[string]any{"activity_code": "kiem_tra", "sop_step_seq": nil})
	expect(t, l, http.StatusCreated, "gieo nhật ký HTX A")
	lb := adm.log(f.p4, map[string]any{"activity_code": "kiem_tra", "sop_step_seq": nil})
	expect(t, lb, http.StatusCreated, "gieo nhật ký HTX B")
	d.l1, d.lb = str(l.body, "log", "id"), str(lb.body, "log", "id")
	r := f.sopAdmin.adminPost("/v1/cropcycle/sops", sopBody("SOP riêng HTX B", map[string]any{"code": "SOP-B" + ids.New().String()[30:],
		"scope": "htx", "cooperative_ref": f.coopB.String()}))
	expect(t, r, http.StatusCreated, "SOP riêng HTX B")
	d.sopB = str(r.body, "ref")
	expect(t, f.sopAdmin.adminPost("/v1/cropcycle/sop-versions/"+str(list(r.body, "versions")[0], "id")+"/publish", nil), http.StatusOK, "phát hành SOP B")
	f.advanceDays(96)
	h6 := adm.post("/v1/cropcycle/seasons/"+d.s6+"/harvest", map[string]any{"mass_kg": "9000"})
	expect(t, h6, http.StatusCreated, "gieo sản lượng HTX A")
	hb := adm.post("/v1/cropcycle/seasons/"+d.sb+"/harvest", map[string]any{"mass_kg": "9000"})
	expect(t, hb, http.StatusCreated, "gieo sản lượng HTX B")
	d.h6, d.hb = str(h6.body, "harvest", "id"), str(hb.body, "harvest", "id")
	return d
}

// currentEvent đọc sự kiện hiện hành của vụ (bằng Admin của tenant).
func (f *fixture) currentEvent(season string) string {
	return str(f.platformAdmin().get("/v1/cropcycle/seasons/"+season).body, "current_event_id")
}

// noData: phản hồi bị từ chối (4xx) hoặc không mang dữ liệu nào (danh sách rỗng, tổng 0, không có dòng ghi được).
func noData(r resp) bool {
	if r.status >= 400 && r.status < 500 {
		for _, k := range []string{"season", "log", "harvest", "items", "plots", "features", "rows"} {
			if get(r.body, k) != nil {
				return false
			}
		}
		return true
	}
	items := list(r.body, "items")
	if num(r.body, "skipped") > 0 && num(r.body, "opened") == 0 {
		items = nil // mở vụ hàng loạt: chỉ có dòng bị bỏ qua
	}
	return r.status < 300 && len(items) == 0 && len(list(r.body, "plots")) == 0 &&
		num(r.body, "totals", "seasons") == 0 && num(r.body, "accepted") == 0 && num(r.body, "opened") == 0
}

// khmvCall là một API của một ô ma trận. mine: dữ liệu HTX A (của mình); không thì dữ liệu HTX B.
type khmvCall struct {
	fn   string
	act  htxroles.Action
	api  string
	ok   int
	call func(t *testing.T, f *fixture, c caller, d khmvData, mine bool) resp
}

func pick(mine bool, own, other string) string {
	if mine {
		return own
	}
	return other
}

// khmvCalls theo thứ tự chạy: đọc trước, sửa/hủy, rồi tạo (thao tác sau không phá dữ liệu của thao tác trước).
func khmvCalls() []khmvCall {
	getPath := func(p func(f *fixture, d khmvData, mine bool) string) func(*testing.T, *fixture, caller, khmvData, bool) resp {
		return func(_ *testing.T, f *fixture, c caller, d khmvData, mine bool) resp { return c.get(p(f, d, mine)) }
	}
	coopQ := func(f *fixture, mine bool) string { return pick(mine, f.coopA.String(), f.coopB.String()) }
	return []khmvCall{
		{"KHMV-FN-01", htxroles.Read, "GET /v1/cropcycle/sops/:ref", http.StatusOK, getPath(func(_ *fixture, d khmvData, mine bool) string {
			return "/v1/cropcycle/sops/" + pick(mine, d.sop, d.sopB)
		})},
		{"KHMV-FN-03", htxroles.Read, "GET /v1/cropcycle/seasons/:ref (vụ đã mở)", http.StatusOK, getPath(func(_ *fixture, d khmvData, mine bool) string {
			return "/v1/cropcycle/seasons/" + pick(mine, d.s1, d.sb)
		})},
		{"KHMV-FN-03-XEM", htxroles.Read, "GET /v1/cropcycle/seasons", http.StatusOK, getPath(func(f *fixture, _ khmvData, mine bool) string {
			return "/v1/cropcycle/seasons?cooperative_ref=" + coopQ(f, mine)
		})},
		{"KHMV-FN-03-XEM", htxroles.Read, "GET /v1/cropcycle/seasons/:ref", http.StatusOK, getPath(func(_ *fixture, d khmvData, mine bool) string {
			return "/v1/cropcycle/seasons/" + pick(mine, d.s1, d.sb)
		})},
		{"KHMV-FN-03-XEM", htxroles.Read, "GET /v1/cropcycle/dashboard", http.StatusOK, getPath(func(f *fixture, _ khmvData, mine bool) string {
			return "/v1/cropcycle/dashboard?cooperative_ref=" + coopQ(f, mine)
		})},
		{"KHMV-FN-03-XEM", htxroles.Read, "GET /v1/cropcycle/stage-map", http.StatusOK, getPath(func(f *fixture, _ khmvData, mine bool) string {
			return "/v1/cropcycle/stage-map?cooperative_ref=" + coopQ(f, mine)
		})},
		// Kế hoạch mùa vụ (FN-04) chưa có API riêng: kế hoạch là các bước SOP trong chi tiết vụ.
		{"KHMV-FN-04", htxroles.Read, "GET /v1/cropcycle/seasons/:ref (kế hoạch)", http.StatusOK, getPath(func(_ *fixture, d khmvData, mine bool) string {
			return "/v1/cropcycle/seasons/" + pick(mine, d.s1, d.sb)
		})},
		{"KHMV-FN-05", htxroles.Read, "GET /v1/cropcycle/farm-logs", http.StatusOK, getPath(func(f *fixture, _ khmvData, mine bool) string {
			return "/v1/cropcycle/farm-logs?cooperative_ref=" + coopQ(f, mine)
		})},
		{"KHMV-FN-05", htxroles.Read, "GET /v1/cropcycle/farm-logs/:id", http.StatusOK, getPath(func(_ *fixture, d khmvData, mine bool) string {
			return "/v1/cropcycle/farm-logs/" + pick(mine, d.l1, d.lb)
		})},
		{"KHMV-FN-06", htxroles.Read, "GET /v1/cropcycle/seasons/:ref (sản lượng)", http.StatusOK, getPath(func(_ *fixture, d khmvData, mine bool) string {
			return "/v1/cropcycle/seasons/" + pick(mine, d.s6, d.sb)
		})},
		{"KHMV-FN-03", htxroles.Update, "POST /v1/cropcycle/seasons/:ref/corrections", http.StatusCreated,
			func(_ *testing.T, f *fixture, c caller, d khmvData, mine bool) resp {
				s := pick(mine, d.s1, d.sb)
				return c.post("/v1/cropcycle/seasons/"+s+"/corrections", map[string]any{"supersedes_id": f.currentEvent(s),
					"correction_reason": "Sửa lại thông tin vụ theo biên bản"})
			}},
		{"KHMV-FN-05", htxroles.Update, "POST /v1/cropcycle/farm-logs/:id/corrections", http.StatusCreated,
			func(_ *testing.T, _ *fixture, c caller, d khmvData, mine bool) resp {
				return c.post("/v1/cropcycle/farm-logs/"+pick(mine, d.l1, d.lb)+"/corrections", map[string]any{"activity_code": "kiem_tra",
					"correction_reason": "Sửa ghi chú theo biên bản"})
			}},
		{"KHMV-FN-06", htxroles.Update, "POST /v1/cropcycle/harvests/:id/corrections", http.StatusCreated,
			func(_ *testing.T, _ *fixture, c caller, d khmvData, mine bool) resp {
				return c.post("/v1/cropcycle/harvests/"+pick(mine, d.h6, d.hb)+"/corrections", map[string]any{"mass_kg": "9100",
					"correction_reason": "Cân lại tại kho HTX"})
			}},
		{"KHMV-FN-03", htxroles.Delete, "POST /v1/cropcycle/seasons/:ref/cancel", http.StatusCreated,
			func(_ *testing.T, f *fixture, c caller, d khmvData, mine bool) resp {
				s := pick(mine, d.s2, d.sb)
				return c.post("/v1/cropcycle/seasons/"+s+"/cancel", map[string]any{"supersedes_id": f.currentEvent(s), "reason": "Mở nhầm thửa khi gieo"})
			}},
		{"KHMV-FN-03", htxroles.Create, "POST /v1/cropcycle/seasons", http.StatusCreated,
			func(_ *testing.T, f *fixture, c caller, _ khmvData, mine bool) resp {
				if mine {
					return c.open(f.p3, nil)
				}
				return c.open(f.p4, nil)
			}},
		{"KHMV-FN-03", htxroles.Create, "POST /v1/cropcycle/seasons/bulk-open", http.StatusOK,
			func(_ *testing.T, f *fixture, c caller, _ khmvData, mine bool) resp {
				return c.post("/v1/cropcycle/seasons/bulk-open", map[string]any{"plot_refs": []string{pick(mine, f.p3.String(), f.p4.String())},
					"season_code": "DX", "season_year": 2026, "variety_code": "OM5451", "sowing_date": f.clk.Now().Format("2006-01-02")})
			}},
		{"KHMV-FN-05", htxroles.Create, "POST /v1/cropcycle/farm-logs", http.StatusCreated,
			func(_ *testing.T, f *fixture, c caller, _ khmvData, mine bool) resp {
				plot := f.p4
				if mine {
					plot = f.p1
				}
				return c.log(plot, map[string]any{"activity_code": "kiem_tra", "sop_step_seq": nil})
			}},
		{"KHMV-FN-05", htxroles.Create, "POST /v1/cropcycle/farm-logs/batch", http.StatusCreated,
			func(_ *testing.T, f *fixture, c caller, _ khmvData, mine bool) resp {
				return c.post("/v1/cropcycle/farm-logs/batch", map[string]any{"plot_refs": []string{pick(mine, f.p1.String(), f.p4.String())},
					"activity_code": "tuoi_nuoc"})
			}},
		{"KHMV-FN-06", htxroles.Create, "POST /v1/cropcycle/seasons/:ref/harvest", http.StatusCreated,
			func(_ *testing.T, _ *fixture, c caller, d khmvData, mine bool) resp {
				return c.post("/v1/cropcycle/seasons/"+pick(mine, d.s5, d.sb)+"/harvest", map[string]any{"mass_kg": "9750"})
			}},
		{"KHMV-FN-06", htxroles.Create, "POST /v1/cropcycle/harvests/bulk", http.StatusOK,
			func(_ *testing.T, f *fixture, c caller, _ khmvData, mine bool) resp {
				return c.post("/v1/cropcycle/harvests/bulk", map[string]any{"cooperative_ref": coopQ(f, mine),
					"rows": []map[string]any{{"plot_code": "TH0000001", "mass_kg": "9750"}}})
			}},
		{"KHMV-FN-06", htxroles.Create, "POST /v1/cropcycle/harvests/import", http.StatusOK,
			func(t *testing.T, f *fixture, c caller, _ khmvData, mine bool) resp {
				body := harvestXLSX(t, [][]any{{"TH0000001", "2027-01-03", 9750, 17}})
				return c.do(f.app.URL, http.MethodPost, "/v1/cropcycle/harvests/import?cooperative_ref="+coopQ(f, mine), key(), tabular.ContentTypeXLSX, body)
			}},
	}
}

// Ô ma trận không có API vụ mùa trong ticket này — kiểm ở nơi khác.
var khmvNoAPI = map[string]string{
	"KHMV-FN-01/C": "Admin API soạn SOP (sopAuthorRoles có platform_admin) — sop_integration_test; ticket này không đổi",
	"KHMV-FN-01/U": "Admin API soạn SOP — sop_integration_test",
	"KHMV-FN-01/D": "Admin API soạn SOP — sop_integration_test",
	"KHMV-FN-02/R": "thửa: plot/handler TestKHMV_FN02_MemberReadsOwnCoopPlots; nông hộ: AH-273",
	"KHMV-FN-04/C": "kế hoạch mùa vụ chưa có API riêng; grants = KHMV-FN-03 (TestAllows_KHMV)",
	"KHMV-FN-04/U": "kế hoạch mùa vụ chưa có API riêng; grants = KHMV-FN-03 (TestAllows_KHMV)",
	"KHMV-FN-04/D": "kế hoạch mùa vụ chưa có API riêng; grants = KHMV-FN-03 (TestAllows_KHMV)",
	"KHMV-NTF/R":   "thông báo AH-22: TestAH22_AC4_NotifyMembers, notify/handler",
	"KHMV-FN-05/D": "chưa có API xóa nhật ký (sổ chỉ ghi thêm; sửa là bản ghi bù) — ngoài phạm vi AH-39",
	"KHMV-FN-06/D": "chưa có API hủy khai báo sản lượng — ngoài phạm vi AH-39",
	// Lịch thời vụ chưa có màn và API (AH-51): chỉ ghi dòng quyền vào ma trận.
	"KHMV-FN-03-LICH/C": "AH-51", "KHMV-FN-03-LICH/R": "AH-51", "KHMV-FN-03-LICH/U": "AH-51", "KHMV-FN-03-LICH/D": "AH-51",
}

// TestKHMV_NFR05_RoleMatrixTable: ba vai × mọi ô có API. Được phép (theo htxroles.Can) → 2xx trên dữ liệu HTX A;
// không được phép → 403 (cropcycle.read_only khi vai có quyền xem dòng đó). Mỗi ô có ca bị từ chối: Đại diện HTX,
// Thành viên HTX dùng dữ liệu HTX B; Admin (toàn hệ thống, AC3) dùng tài khoản Admin của tenant khác.
func TestKHMV_NFR05_RoleMatrixTable(t *testing.T) {
	calls := khmvCalls()
	covered := map[string]bool{}
	for _, c := range calls {
		covered[c.fn+"/"+string(c.act)] = true
	}
	for _, fn := range htxroles.Load().Functions {
		if !strings.HasPrefix(fn.Key, "KHMV-") {
			continue
		}
		for _, a := range "CRUD" {
			cell := fn.Key + "/" + string(a)
			has := false
			for _, g := range fn.Grants {
				has = has || strings.ContainsRune(g, a)
			}
			if has && !covered[cell] && khmvNoAPI[cell] == "" {
				t.Errorf("ô %s chưa có ca kiểm (thêm vào khmvCalls hoặc khmvNoAPI kèm lý do)", cell)
			}
		}
	}

	for _, role := range []string{htxroles.Admin, htxroles.Representative, htxroles.Member} {
		t.Run(role, func(t *testing.T) {
			f := newFixture(t)
			d := khmvSeed(t, f)
			own, other, otherMine := f.as(f.tenant, f.coopA, role), f.as(f.tenant, f.coopA, role), false
			if role == htxroles.Admin {
				own, other, otherMine = f.platformAdmin(), f.as(ids.New(), uuid.Nil, htxroles.Admin), true
			}
			for _, c := range calls {
				what := fmt.Sprintf("%s %s/%c %s", role, c.fn, c.act, c.api)
				r := c.call(t, f, own, d, true)
				switch {
				case htxroles.Can(role, c.fn, c.act):
					if r.status != c.ok {
						t.Fatalf("%s: được phép, HTTP %d, muốn %d: %s", what, r.status, c.ok, r.raw)
					}
				case htxroles.Can(role, c.fn, htxroles.Read):
					expectCode(t, r, http.StatusForbidden, "cropcycle.read_only", what)
				default:
					expectCode(t, r, http.StatusForbidden, "forbidden", what)
				}
				if rd := c.call(t, f, other, d, otherMine); !noData(rd) {
					t.Fatalf("%s: dữ liệu ngoài phạm vi phải bị từ chối, HTTP %d: %s", what, rd.status, rd.raw)
				}
			}
		})
	}
}

// khmvSnapshot đếm dữ liệu HTX A để chứng minh không đổi.
func (f *fixture) khmvSnapshot(d khmvData) string {
	f.t.Helper()
	var out []string
	err := tenant.WithTenantTx(context.Background(), f.db, f.tenant, func(tx pgx.Tx) error {
		for _, q := range []string{
			`SELECT count(*)::text FROM pf_coop.season_event`,
			`SELECT count(*)::text FROM pf_coop.farm_log`,
			`SELECT count(*)::text FROM pf_coop.harvest_declaration`,
			`SELECT count(*)::text FROM shared.outbox`,
		} {
			var s string
			if err := tx.QueryRow(context.Background(), q).Scan(&s); err != nil {
				return err
			}
			out = append(out, s)
		}
		return nil
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return strings.Join(out, "|") + "|" + f.currentEvent(d.s1) + "|" + f.currentEvent(d.s5)
}

// AH-39 AC4: Đại diện HTX và Thành viên HTX của HTX B gọi API với dữ liệu HTX A → bị từ chối, phản hồi không mang dữ
// liệu, dữ liệu không đổi.
func TestKHMV_AC4_OtherCoopDenied(t *testing.T) {
	f := newFixture(t)
	d := khmvSeed(t, f)
	before := f.khmvSnapshot(d)
	for _, c := range []caller{f.managerB(), f.memberB()} {
		for _, path := range []string{"/v1/cropcycle/seasons/" + d.s1, "/v1/cropcycle/farm-logs/" + d.l1, "/v1/cropcycle/seasons/" + d.s6} {
			r := c.get(path)
			if r.status != http.StatusNotFound || get(r.body, "season") != nil || get(r.body, "log") != nil || get(r.body, "harvest") != nil {
				t.Fatalf("HTX B đọc %s: %d %s", path, r.status, r.raw)
			}
		}
		for _, path := range []string{"/v1/cropcycle/seasons?cooperative_ref=" + f.coopA.String(), "/v1/cropcycle/farm-logs?cooperative_ref=" + f.coopA.String()} {
			if r := c.get(path); !noData(r) {
				t.Fatalf("HTX B liệt kê HTX A %s: %d %s", path, r.status, r.raw)
			}
		}
		writes := []resp{
			c.open(f.p3, nil),
			c.log(f.p1, map[string]any{"activity_code": "kiem_tra", "sop_step_seq": nil}),
			c.post("/v1/cropcycle/seasons/"+d.s1+"/corrections", map[string]any{"supersedes_id": f.currentEvent(d.s1), "correction_reason": "Sửa chéo HTX khác"}),
			c.post("/v1/cropcycle/seasons/"+d.s1+"/cancel", map[string]any{"supersedes_id": f.currentEvent(d.s1), "reason": "Hủy chéo HTX khác"}),
			c.post("/v1/cropcycle/seasons/"+d.s5+"/harvest", map[string]any{"mass_kg": "9750"}),
			c.post("/v1/cropcycle/farm-logs/"+d.l1+"/corrections", map[string]any{"activity_code": "kiem_tra", "correction_reason": "Sửa chéo HTX khác"}),
			c.post("/v1/cropcycle/harvests/"+d.h6+"/corrections", map[string]any{"mass_kg": "1", "correction_reason": "Sửa chéo HTX khác"}),
			c.post("/v1/cropcycle/harvests/bulk", map[string]any{"cooperative_ref": f.coopA.String(), "rows": []map[string]any{{"plot_code": "TH0000005", "mass_kg": "1"}}}),
		}
		for i, r := range writes {
			if r.status != http.StatusForbidden && r.status != http.StatusNotFound || !noData(r) {
				t.Fatalf("HTX B ghi dữ liệu HTX A (#%d): %d %s", i, r.status, r.raw)
			}
		}
	}
	if after := f.khmvSnapshot(d); after != before {
		t.Fatalf("dữ liệu HTX A bị đổi:\n%s\n%s", before, after)
	}
}

// AH-39 AC4: RLS chặn ở tầng cơ sở dữ liệu — giao dịch của Thành viên HTX B (app.node_id = HTX B) không thấy, không sửa
// được bản ghi HTX A; đối chứng với Thành viên HTX A.
func TestKHMV_AC4_RLSDirect(t *testing.T) {
	f := newFixture(t)
	d := khmvSeed(t, f)
	count := func(coop uuid.UUID) (n [3]int, updated int64) {
		ctx := authz.WithPrincipal(context.Background(), authz.Principal{ActorRef: ids.New(), TenantID: f.tenant, OrgRef: coop, Roles: []string{port.RoleHTXMember}})
		err := tenant.WithTenantTx(ctx, f.db, f.tenant, func(tx pgx.Tx) error {
			for i, q := range []string{
				`SELECT count(*) FROM pf_coop.season_current WHERE season_ref = $1::uuid`,
				`SELECT count(*) FROM pf_coop.farm_log_current WHERE id = $1::uuid`,
				`SELECT count(*) FROM pf_coop.harvest_declaration WHERE id = $1::uuid`,
			} {
				arg := []string{d.s1, d.l1, d.h6}[i]
				if err := tx.QueryRow(ctx, q, arg).Scan(&n[i]); err != nil {
					return err
				}
			}
			tag, err := tx.Exec(ctx, `UPDATE pf_coop.season_current SET updated_at = updated_at WHERE season_ref = $1::uuid`, d.s1)
			updated = tag.RowsAffected()
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		return n, updated
	}
	if n, u := count(f.coopB); n != [3]int{} || u != 0 {
		t.Fatalf("RLS phải chặn HTX B: thấy %v, sửa %d dòng", n, u)
	}
	if n, _ := count(f.coopA); n != [3]int{1, 1, 1} {
		t.Fatalf("đối chứng HTX A phải thấy bản ghi: %v", n)
	}
}

// AH-39 (FN-06/EX-02, fail-closed): Đại diện HTX, Thành viên HTX chưa gắn HTX không đọc, không ghi được gì.
func TestKHMV_NoCoopFailClosed(t *testing.T) {
	f := newFixture(t)
	d := khmvSeed(t, f)
	before := f.khmvSnapshot(d)
	for _, c := range []caller{f.memberNoCoop(), f.managerNoCoop()} {
		for _, path := range []string{"/v1/cropcycle/seasons", "/v1/cropcycle/seasons/" + d.s1, "/v1/cropcycle/farm-logs",
			"/v1/cropcycle/farm-logs/" + d.l1, "/v1/cropcycle/dashboard", "/v1/cropcycle/stage-map"} {
			// Mã thực tế: 403 node_scope_required (giao dịch cần Nút) — không lộ dữ liệu.
			if r := c.get(path); r.status != http.StatusForbidden || code(r) != "node_scope_required" || !noData(r) {
				t.Fatalf("chưa gắn HTX đọc %s: %d %s", path, r.status, r.raw)
			}
		}
		for i, r := range []resp{
			c.open(f.p3, nil),
			c.log(f.p1, map[string]any{"activity_code": "kiem_tra", "sop_step_seq": nil}),
			c.post("/v1/cropcycle/seasons/"+d.s5+"/harvest", map[string]any{"mass_kg": "9750"}),
			c.post("/v1/cropcycle/harvests/bulk", map[string]any{"rows": []map[string]any{{"plot_code": "TH0000005", "mass_kg": "1"}}}),
		} {
			if r.status != http.StatusForbidden || !noData(r) {
				t.Fatalf("chưa gắn HTX ghi (#%d): %d %s", i, r.status, r.raw)
			}
		}
	}
	if after := f.khmvSnapshot(d); after != before {
		t.Fatalf("dữ liệu bị đổi:\n%s\n%s", before, after)
	}
}

// AH-39, BRD mục V: tài khoản kiêm Đại diện HTX và Thành viên HTX dùng quyền Đại diện HTX tới AH-270 — mọi thao tác
// ghi của ma trận (kể cả qua đồng bộ) bị từ chối, đọc được.
func TestKHMV_DualRoleWritesDenied(t *testing.T) {
	f := newFixture(t)
	d := khmvSeed(t, f)
	both := f.bothA()
	for _, c := range khmvCalls() {
		r := c.call(t, f, both, d, true)
		if c.act == htxroles.Read {
			if r.status != c.ok {
				t.Fatalf("kiêm hai vai đọc %s: %d %s", c.api, r.status, r.raw)
			}
			continue
		}
		expectCode(t, r, http.StatusForbidden, "cropcycle.read_only", "kiêm hai vai ghi "+c.api)
	}
	ctx := f.ctxFor(both, port.RoleCoopManager, port.RoleHTXMember)
	for _, e := range []string{port.EntitySeasonOpen, port.EntityFarmLog, port.EntityLogBatch, port.EntityHarvest} {
		res := f.mod.SyncSink().Accept(ctx, authz.Scope{}, rec(e, syncport.OpCreate, map[string]any{"plot_ref": f.p3.String()}, nil, "", f.clk.Now()))
		if res.Status != syncport.StatusRejected || res.Code != "cropcycle.read_only" {
			t.Fatalf("kiêm hai vai đồng bộ %s: %+v", e, res)
		}
	}
}

// AH-39: đồng bộ từ app cho Thành viên HTX chưa mở (app di động thuộc AH-303) — chiều xuống rỗng, chiều lên bị từ chối.
func TestKHMV_SyncNotOpenedForMember(t *testing.T) {
	f := newFixture(t)
	khmvSeed(t, f)
	ctx := f.ctxFor(f.memberA(), port.RoleHTXMember)
	src := f.mod.SyncSource()
	for _, e := range src.Entities() {
		ch, _, err := src.Changes(ctx, authz.Scope{}, e, 0, 100)
		if err != nil || len(ch) != 0 {
			t.Fatalf("chiều xuống %s cho Thành viên HTX: %v %v", e, ch, err)
		}
	}
	for _, e := range []string{port.EntitySeasonOpen, port.EntityFarmLog, port.EntityLogBatch, port.EntityHarvest} {
		res := f.mod.SyncSink().Accept(ctx, authz.Scope{}, rec(e, syncport.OpCreate, map[string]any{"plot_ref": f.p3.String()}, nil, "", f.clk.Now()))
		if res.Status != syncport.StatusRejected || res.Code != "forbidden" {
			t.Fatalf("chiều lên %s của Thành viên HTX: %+v", e, res)
		}
	}
	// Đối chứng: lối ghi qua Application API vẫn mở cho Thành viên HTX.
	if r := f.memberA().open(f.p3, nil); r.status != http.StatusCreated {
		t.Fatalf("Thành viên HTX mở vụ qua API: %d %s", r.status, r.raw)
	}
}
