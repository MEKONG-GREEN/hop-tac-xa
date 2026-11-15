package masterdata_test

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/masterdata/port"
	syncport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
)

// Kiểm thử AH-7 (Quản lý danh mục mùa vụ, Admin) trên PostgreSQL thật. Mỗi AC của Story có test mang mã AC.

const seasonsPath = "/v1/masterdata/catalogs/season/items"

var seasonCodeRe = regexp.MustCompile(`^[0-9A-F]{32}$`)

// fakeSeasonUsage giả nguồn đếm số vụ đang dùng mỗi mùa vụ (cột "Đang dùng").
type fakeSeasonUsage struct{ counts map[string]int }

func (u fakeSeasonUsage) SeasonsUsing(_ context.Context, codes []string) (map[string]int, error) {
	out := map[string]int{}
	for _, c := range codes {
		if n, ok := u.counts[c]; ok {
			out[c] = n
		}
	}
	return out, nil
}

func (c caller) newSeason(t *testing.T, name string, attrs map[string]string) string {
	t.Helper()
	st, b := c.adm(http.MethodPost, seasonsPath, map[string]any{"name": name, "attrs": attrs})
	if st != http.StatusCreated {
		t.Fatalf("tạo mùa vụ %q: %d %v", name, st, b)
	}
	return b["code"].(string)
}

// AC1 — Thêm mùa vụ: thấy ngay trong danh sách chọn Vụ/Năm của mọi vai đọc.
func TestAH7_AC1_AddSeasonVisibleInPicker(t *testing.T) {
	f := newFixture(t)
	tid := f.seeded()
	admin := f.caller(tid, port.RoleMasterdataAdmin)

	key := "k-" + ids.New().String()
	body := map[string]any{"name": "Vụ thử AC1", "attrs": map[string]string{"description": "Mùa vụ thử", "start_month": "11", "end_month": "3"}}
	st, b := admin.do(f.admin, http.MethodPost, seasonsPath, key, body)
	if st != http.StatusCreated || b["active"] != true {
		t.Fatalf("thêm mùa vụ: %d %v", st, b)
	}
	code := b["code"].(string)
	if !seasonCodeRe.MatchString(code) {
		t.Fatalf("mã phải do hệ thống sinh (32 HEX HOA): %q", code)
	}
	if attrs := b["attrs"].(map[string]any); attrs["start_month"] != "11" || attrs["end_month"] != "3" || attrs["description"] != "Mùa vụ thử" {
		t.Fatalf("thuộc tính: %v", attrs)
	}
	for _, role := range []string{"coop_manager", "farmer"} {
		reader := roleCaller(t, f, tid, role)
		if !slices.Contains(listCodes(t, reader, "/v1/masterdata/catalogs/season?limit=200"), code) {
			t.Fatalf("%s không thấy mùa vụ vừa thêm", role)
		}
	}
	pi, err := f.mod.Catalogs().Get(admin.ctx(), port.CatalogSeason, code)
	if err != nil || pi.Name != "Vụ thử AC1" || !pi.Active {
		t.Fatalf("port Get: %+v %v", pi, err)
	}
	// Gửi lặp cùng Idempotency-Key → cùng mã, không có mục thứ hai.
	if st, again := admin.do(f.admin, http.MethodPost, seasonsPath, key, body); (st != http.StatusOK && st != http.StatusCreated) || again["code"] != code {
		t.Fatalf("gửi lặp: %d %v", st, again)
	}
	if acts := historyActions(t, admin, "season", code); !slices.Equal(acts, []string{"created"}) {
		t.Fatalf("lịch sử: %v", acts)
	}
	// Tháng: chỉ một tháng, hoặc ngoài 1–12 → 422 đúng trường.
	for name, attrs := range map[string]map[string]string{
		"một tháng":     {"start_month": "11"},
		"tháng 13":      {"start_month": "13", "end_month": "3"},
		"tháng chữ":     {"start_month": "a", "end_month": "3"},
		"mô tả quá dài": {"description": strings.Repeat("x", 501)},
	} {
		st, b := admin.adm(http.MethodPost, seasonsPath, map[string]any{"name": "Sai " + name, "attrs": attrs})
		if st != http.StatusUnprocessableEntity {
			t.Fatalf("%s: %d %v", name, st, b)
		}
	}
	// Thiếu tên → 422.
	if st, b := admin.adm(http.MethodPost, seasonsPath, map[string]any{"name": " ", "attrs": map[string]string{}}); st != http.StatusUnprocessableEntity {
		t.Fatalf("thiếu tên: %d %v", st, b)
	}
	// Máy chủ sinh mã: gửi code → 422.
	st, b = admin.adm(http.MethodPost, seasonsPath, map[string]any{"code": "VU_TU_DAT", "name": "Tự đặt mã", "attrs": map[string]string{}})
	if field, _ := firstError(t, b); st != http.StatusUnprocessableEntity || field != "code" {
		t.Fatalf("gửi mã: %d %v", st, b)
	}
}

// AC2 — Trùng tên (server): sau khi bỏ khoảng trắng thừa, không phân biệt hoa thường; ẩn vẫn giữ tên, xóa thì không.
func TestAH7_AC2_DuplicateNameRejectedByServer(t *testing.T) {
	f := newFixture(t)
	f.withUsage(fakeUsage{})
	tid := f.seeded()
	admin := f.caller(tid, port.RoleMasterdataAdmin)

	for _, dup := range []string{"đông xuân", "  ĐÔNG   XUÂN "} {
		st, b := admin.adm(http.MethodPost, seasonsPath, map[string]any{"name": dup, "attrs": map[string]string{}})
		field, _ := firstError(t, b)
		if st != http.StatusConflict || b["code"] != "masterdata.catalog_item_name_taken" || field != "name" {
			t.Fatalf("trùng %q: %d %v", dup, st, b)
		}
	}
	// Mục đã ẩn vẫn giữ tên.
	hidden := admin.newSeason(t, "Vụ sẽ ẩn", nil)
	if st, b := admin.adm(http.MethodPost, seasonsPath+"/"+hidden+"/deactivate", map[string]any{}); st != http.StatusOK {
		t.Fatalf("ẩn: %d %v", st, b)
	}
	if st, b := admin.adm(http.MethodPost, seasonsPath, map[string]any{"name": "Vụ sẽ ẩn", "attrs": map[string]string{}}); st != http.StatusConflict {
		t.Fatalf("mùa vụ ẩn vẫn chặn tên: %d %v", st, b)
	}
	// Mục đã xóa thì không chặn tên.
	gone := admin.newSeason(t, "Vụ sẽ xóa", nil)
	if st, b := admin.adm(http.MethodPost, seasonsPath+"/"+gone+"/delete", nil); st != http.StatusOK || b["outcome"] != "deleted" {
		t.Fatalf("xóa: %d %v", st, b)
	}
	admin.newSeason(t, "Vụ sẽ xóa", nil)
	// Sửa tên mục khác thành tên trùng → 409; giữ nguyên tên của chính nó → 200.
	other := admin.newSeason(t, "Vụ khác", nil)
	if st, b := admin.adm(http.MethodPut, seasonsPath+"/"+other, map[string]any{"name": "HÈ THU", "attrs": map[string]string{}}); st != http.StatusConflict || b["code"] != "masterdata.catalog_item_name_taken" {
		t.Fatalf("PUT trùng tên: %d %v", st, b)
	}
	if st, b := admin.adm(http.MethodPut, seasonsPath+"/"+other, map[string]any{"name": "Vụ khác", "attrs": map[string]string{"description": "mô tả mới"}}); st != http.StatusOK {
		t.Fatalf("PUT cùng tên: %d %v", st, b)
	}
}

// AC3 — Đổi tên: mã không đổi, danh sách chọn hiện tên mới.
func TestAH7_AC3_RenameKeepsCode(t *testing.T) {
	f := newFixture(t)
	f.withUsage(fakeUsage{})
	tid := f.seeded()
	admin := f.caller(tid, port.RoleMasterdataAdmin)
	reader := roleCaller(t, f, tid, "coop_manager")

	code := admin.newSeason(t, "Vụ cũ tên", map[string]string{"start_month": "4", "end_month": "8", "description": "mô tả"})
	f.mod.UseCatalogUsage(fakeUsage{used: map[string]bool{"season/" + code: true}}) // đang được vụ dùng
	st, b := admin.adm(http.MethodPut, seasonsPath+"/"+code, map[string]any{"name": "Vụ tên mới", "attrs": map[string]string{"start_month": "4", "end_month": "8", "description": "mô tả"}})
	if st != http.StatusOK || b["name"] != "Vụ tên mới" || b["code"] != code {
		t.Fatalf("đổi tên: %d %v", st, b)
	}
	if st, it := reader.get("/v1/masterdata/catalogs/season/" + code); st != http.StatusOK || it["name"] != "Vụ tên mới" {
		t.Fatalf("danh sách chọn phải có tên mới: %d %v", st, it)
	}
	pi, err := f.mod.Catalogs().Get(reader.ctx(), port.CatalogSeason, code)
	if err != nil || pi.Name != "Vụ tên mới" || pi.Code != code {
		t.Fatalf("port Get: %+v %v", pi, err)
	}
	if acts := historyActions(t, admin, "season", code); !slices.Equal(acts, []string{"created", "updated"}) {
		t.Fatalf("lịch sử: %v", acts)
	}
	// Bỏ tháng và mô tả → bị xóa khỏi attrs (mùa vụ không có thuộc tính cũ phải giữ).
	st, b = admin.adm(http.MethodPut, seasonsPath+"/"+code, map[string]any{"name": "Vụ tên mới", "attrs": map[string]string{}})
	if attrs, _ := b["attrs"].(map[string]any); st != http.StatusOK || len(attrs) != 0 {
		t.Fatalf("bỏ tháng: %d %v", st, b)
	}
}

// AC4 — Đang được tham chiếu thì chỉ ẩn; chưa dùng thì xóa; vụ cũ vẫn đọc đúng tên.
func TestAH7_AC4_ReferencedSeasonOnlyHidden(t *testing.T) {
	f := newFixture(t)
	tid := f.seeded()
	admin := f.caller(tid, port.RoleMasterdataAdmin)
	reader := roleCaller(t, f, tid, "coop_manager")
	usage := fakeUsage{used: map[string]bool{}}
	f.withUsage(usage)
	f.mod.UseSeasonUsage(fakeSeasonUsage{counts: map[string]int{}})
	listPath := "/v1/masterdata/catalogs/season?limit=200"

	// (a) Mùa vụ đang dùng → chỉ ẩn, đọc theo mã vẫn đúng tên.
	used := admin.newSeason(t, "Vụ đang dùng", nil)
	usage.used["season/"+used] = true
	st, b := admin.adm(http.MethodPost, seasonsPath+"/"+used+"/delete", nil)
	if st != http.StatusOK || b["outcome"] != "hidden" || b["item"].(map[string]any)["active"] != false {
		t.Fatalf("xóa mùa vụ đang dùng: %d %v", st, b)
	}
	if st, it := reader.get("/v1/masterdata/catalogs/season/" + used); st != http.StatusOK || it["name"] != "Vụ đang dùng" || it["active"] != false {
		t.Fatalf("vụ cũ phải đọc đúng tên: %d %v", st, it)
	}
	if slices.Contains(listCodes(t, reader, listPath), used) || !slices.Contains(listCodes(t, reader, listPath+"&include_inactive=true"), used) {
		t.Fatal("mùa vụ đã ẩn không có trong danh sách chọn nhưng có khi include_inactive")
	}
	if acts := historyActions(t, admin, "season", used); !slices.Equal(acts, []string{"created", "deactivated"}) {
		t.Fatalf("lịch sử: %v", acts)
	}

	// (b) Mùa vụ chưa dùng → xóa: biến khỏi mọi danh sách, đồng bộ phát tombstone.
	free := admin.newSeason(t, "Vụ chưa dùng", nil)
	_, cursor := pullAll(t, admin, f.mod.SyncSource(), authz.Scope{All: true}, port.SyncEntityCatalogItem, 0, 500)
	if st, b := admin.adm(http.MethodPost, seasonsPath+"/"+free+"/delete", nil); st != http.StatusOK || b["outcome"] != "deleted" {
		t.Fatalf("xóa mùa vụ chưa dùng: %d %v", st, b)
	}
	for _, p := range []string{listPath, listPath + "&include_inactive=true"} {
		if slices.Contains(listCodes(t, reader, p), free) {
			t.Fatalf("mục đã xóa còn trong %s", p)
		}
	}
	chs, _ := pullAll(t, admin, f.mod.SyncSource(), authz.Scope{All: true}, port.SyncEntityCatalogItem, cursor, 500)
	if !slices.ContainsFunc(chs, func(c syncport.Change) bool {
		return c.ID == port.CatalogItemSyncID("season", free) && c.Op == syncport.OpTombstone
	}) {
		t.Fatalf("đồng bộ phải phát tombstone: %v", chs)
	}

	// (c) Nguồn đếm lỗi → 503, không đổi gì.
	flaky := admin.newSeason(t, "Vụ khi nguồn đếm lỗi", nil)
	f.mod.UseCatalogUsage(fakeUsage{err: errors.New("hết thời gian chờ")})
	if st, b := admin.adm(http.MethodPost, seasonsPath+"/"+flaky+"/delete", nil); st != http.StatusServiceUnavailable || b["code"] != "masterdata.dependency_unavailable" {
		t.Fatalf("nguồn đếm lỗi: %d %v", st, b)
	}
	if st, it := reader.get("/v1/masterdata/catalogs/season/" + flaky); st != http.StatusOK || it["active"] != true {
		t.Fatalf("không đổi gì: %d %v", st, it)
	}

	// (d) Không nối nguồn đếm → coi như đang dùng, chỉ ẩn.
	bare := newFixture(t)
	bareAdmin := bare.caller(bare.seeded(), port.RoleMasterdataAdmin)
	v := bareAdmin.newSeason(t, "Vụ không nguồn đếm", nil)
	if st, b := bareAdmin.adm(http.MethodPost, seasonsPath+"/"+v+"/delete", nil); st != http.StatusOK || b["outcome"] != "hidden" {
		t.Fatalf("thiếu nguồn đếm: %d %v", st, b)
	}

	// (e) Ẩn không gửi lý do → 200, lý do hệ thống; hiện lại được.
	g := newFixture(t)
	g.withUsage(fakeUsage{})
	gAdmin := g.caller(g.seeded(), port.RoleMasterdataAdmin)
	s := gAdmin.newSeason(t, "Vụ ẩn không lý do", nil)
	st, b = gAdmin.adm(http.MethodPost, seasonsPath+"/"+s+"/deactivate", map[string]any{})
	if st != http.StatusOK || b["deactivation_reason"] != "Ẩn tại màn Danh mục mùa vụ" {
		t.Fatalf("ẩn không lý do: %d %v", st, b)
	}
	if st, b := gAdmin.adm(http.MethodPost, seasonsPath+"/"+s+"/reactivate", map[string]any{}); st != http.StatusOK || b["active"] != true {
		t.Fatalf("hiện lại: %d %v", st, b)
	}

	// (f) Danh sách Admin có seasons_in_use; danh sách Application không có trường này.
	h := newFixture(t)
	h.withUsage(fakeUsage{})
	h.mod.UseSeasonUsage(fakeSeasonUsage{counts: map[string]int{"DX": 4}})
	hTid := h.seeded()
	hAdmin := h.caller(hTid, port.RoleMasterdataAdmin)
	st, b = hAdmin.adm(http.MethodGet, seasonsPath+"?limit=200", nil)
	if st != http.StatusOK {
		t.Fatalf("danh sách Admin: %d %v", st, b)
	}
	got := map[string]any{}
	for _, it := range b["items"].([]any) {
		m := it.(map[string]any)
		got[m["code"].(string)] = m["seasons_in_use"]
	}
	if got["DX"] != float64(4) || got["HT"] != float64(0) {
		t.Fatalf("seasons_in_use: %v", got)
	}
	_, app := roleCaller(t, h, hTid, "coop_manager").get("/v1/masterdata/catalogs/season?limit=200")
	for _, it := range app["items"].([]any) {
		if _, has := it.(map[string]any)["seasons_in_use"]; has {
			t.Fatalf("Application API không có seasons_in_use: %v", it)
		}
	}
}

// AC5 — Quyền: chỉ vai quản trị danh mục ghi; TV, ĐD chỉ xem.
func TestAH7_AC5_OnlyCatalogAdminWrites(t *testing.T) {
	f := newFixture(t)
	f.withUsage(fakeUsage{})
	tid := f.seeded()
	admin := f.caller(tid, port.RoleMasterdataAdmin)
	s := admin.newSeason(t, "Vụ để thử quyền", nil)

	for name, c := range map[string]caller{"ĐD": roleCaller(t, f, tid, "coop_manager"), "TV": roleCaller(t, f, tid, "farmer"), "không vai": f.caller(tid)} {
		for what, call := range map[string]func() (int, map[string]any){
			"tạo": func() (int, map[string]any) {
				return c.adm(http.MethodPost, seasonsPath, map[string]any{"name": "Trái phép", "attrs": map[string]string{}})
			},
			"sửa": func() (int, map[string]any) {
				return c.adm(http.MethodPut, seasonsPath+"/"+s, map[string]any{"name": "Đổi tên", "attrs": map[string]string{}})
			},
			"ẩn": func() (int, map[string]any) {
				return c.adm(http.MethodPost, seasonsPath+"/"+s+"/deactivate", map[string]any{})
			},
			"hiện": func() (int, map[string]any) {
				return c.adm(http.MethodPost, seasonsPath+"/"+s+"/reactivate", map[string]any{})
			},
			"xóa": func() (int, map[string]any) { return c.adm(http.MethodPost, seasonsPath+"/"+s+"/delete", nil) },
		} {
			if st, b := call(); st != http.StatusForbidden || b["code"] != "forbidden" {
				t.Fatalf("%s %s: %d %v", name, what, st, b)
			}
		}
		if !slices.Contains(listCodes(t, c, "/v1/masterdata/catalogs/season?limit=200"), s) {
			t.Fatalf("%s phải xem được danh sách", name)
		}
		if st, _ := c.get("/v1/masterdata/catalogs/season/" + s); st != http.StatusOK {
			t.Fatalf("%s phải đọc được theo mã: %d", name, st)
		}
	}
}

// Dữ liệu khởi tạo: ba mùa vụ Đông Xuân 11–3, Hè Thu 4–8, Thu Đông 8–12.
func TestAH7_SeedSeasons(t *testing.T) {
	f := newFixture(t)
	tid := f.seeded()
	admin := f.caller(tid, port.RoleMasterdataAdmin)

	items, err := f.mod.Catalogs().List(admin.ctx(), port.CatalogSeason)
	if err != nil {
		t.Fatal(err)
	}
	type season struct{ name, start, end string }
	got := map[string]season{}
	for _, it := range items {
		got[it.Code] = season{it.Name, it.Attrs["start_month"], it.Attrs["end_month"]}
	}
	want := map[string]season{"DX": {"Đông Xuân", "11", "3"}, "HT": {"Hè Thu", "4", "8"}, "TD": {"Thu Đông", "8", "12"}}
	for code, w := range want {
		if got[code] != w {
			t.Fatalf("mùa vụ %s: %+v, muốn %+v", code, got[code], w)
		}
	}
}
