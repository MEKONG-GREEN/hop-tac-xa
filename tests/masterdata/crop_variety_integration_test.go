package masterdata_test

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/masterdata"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/masterdata/port"
	syncport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ledger"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
	"github.com/MEKONG-GREEN/hop-tac-xa/migrations"
)

// Kiểm thử AH-2 (Quản lý loại cây và giống, Admin) trên PostgreSQL thật. Mỗi AC của Story có test mang mã AC.

const (
	varietiesPath = "/v1/masterdata/catalogs/rice_variety/items"
	cropsPath     = "/v1/masterdata/catalogs/crop/items"
)

// fakeUsage giả nguồn đếm tham chiếu của module khác (vụ, SOP, sản lượng, thẻ tài liệu).
type fakeUsage struct {
	used map[string]bool // "catalog/code"
	err  error
}

func (u fakeUsage) CatalogItemInUse(_ context.Context, catalog, code string) (bool, error) {
	return u.used[catalog+"/"+code], u.err
}

// withUsage nối các nguồn đếm giả; không nối thì xóa luôn chỉ ẩn (fail-closed).
func (f *fixture) withUsage(u fakeUsage) {
	f.mod.UseFormUsage(fakeFormUsage{})
	f.mod.UseCatalogUsage(u)
}

// roleCaller là người gọi có vai nghiệp vụ và đơn vị công tác (Nút), như luồng thật.
func roleCaller(t *testing.T, f *fixture, tid uuid.UUID, role string) caller {
	t.Helper()
	c := f.caller(tid, role)
	c.token = f.iss.Sign(t, authztest.Token{Subject: c.actor, TenantID: tid, Roles: []string{role}, MFA: true, OrgRef: ids.New()})
	return c
}

func variety(cropCode, days string) map[string]string {
	return map[string]string{"crop_code": cropCode, "growth_days": days}
}

func firstError(t *testing.T, body map[string]any) (field, reason string) {
	t.Helper()
	errs, _ := body["errors"].([]any)
	if len(errs) == 0 {
		t.Fatalf("thiếu lỗi theo trường: %v", body)
	}
	m := errs[0].(map[string]any)
	return m["field"].(string), m["reason"].(string)
}

func (c caller) newVariety(t *testing.T, cropCode, name, days string) string {
	t.Helper()
	st, b := c.adm(http.MethodPost, varietiesPath, map[string]any{"name": name, "attrs": variety(cropCode, days)})
	if st != http.StatusCreated {
		t.Fatalf("tạo giống %q: %d %v", name, st, b)
	}
	return b["code"].(string)
}

func (c caller) newCrop(t *testing.T, name, isRice string) string {
	t.Helper()
	st, b := c.adm(http.MethodPost, cropsPath, map[string]any{"name": name, "attrs": map[string]string{"is_rice": isRice}})
	if st != http.StatusCreated {
		t.Fatalf("tạo loại cây %q: %d %v", name, st, b)
	}
	return b["code"].(string)
}

func listCodes(t *testing.T, c caller, path string) []string {
	t.Helper()
	st, b := c.do(c.f.app, http.MethodGet, path, "", nil)
	if st != http.StatusOK {
		t.Fatalf("GET %s: %d %v", path, st, b)
	}
	var out []string
	for _, it := range b["items"].([]any) {
		out = append(out, it.(map[string]any)["code"].(string))
	}
	return out
}

func adminListCodes(t *testing.T, c caller, path string) []string {
	t.Helper()
	st, b := c.adm(http.MethodGet, path, nil)
	if st != http.StatusOK {
		t.Fatalf("GET %s: %d %v", path, st, b)
	}
	var out []string
	for _, it := range b["items"].([]any) {
		out = append(out, it.(map[string]any)["code"].(string))
	}
	return out
}

func historyActions(t *testing.T, c caller, catalog, code string) []string {
	t.Helper()
	st, h := c.adm(http.MethodGet, "/v1/masterdata/catalogs/"+catalog+"/items/"+code+"/history", nil)
	if st != http.StatusOK {
		t.Fatalf("lịch sử: %d %v", st, h)
	}
	var out []string
	for _, it := range h["items"].([]any) {
		out = append(out, it.(map[string]any)["action"].(string))
	}
	return out
}

// AC1 — Thêm giống: thấy ngay trong danh sách chọn của mọi HTX (mọi vai đọc, port, đồng bộ).
func TestAH2_AC1_AddVarietyVisibleToAllCoops(t *testing.T) {
	f := newFixture(t)
	tid := f.seeded()
	admin := f.caller(tid, port.RoleMasterdataAdmin)

	key := "k-" + ids.New().String()
	st, b := admin.do(f.admin, http.MethodPost, varietiesPath, key, map[string]any{"name": "Giống thử AC1", "attrs": variety("LUA", "95")})
	if st != http.StatusCreated || b["active"] != true {
		t.Fatalf("thêm giống: %d %v", st, b)
	}
	code := b["code"].(string)
	if _, err := uuid.Parse(code); err != nil || len(code) != 36 {
		t.Fatalf("mã phải do hệ thống sinh (UUID): %q %v", code, err)
	}
	attrs := b["attrs"].(map[string]any)
	if _, has := attrs["expected_yield_t_per_ha"]; has || attrs["growth_days"] != "95" || attrs["crop_code"] != "LUA" {
		t.Fatalf("giống mới không có năng suất sẵn: %v", attrs)
	}

	// Thành viên (farmer) và đại diện (coop_manager) cùng thấy ngay.
	for _, role := range []string{"coop_manager", "farmer"} {
		reader := roleCaller(t, f, tid, role)
		if !slices.Contains(listCodes(t, reader, "/v1/masterdata/catalogs/rice_variety?limit=200"), code) {
			t.Fatalf("%s không thấy giống vừa thêm", role)
		}
		if st, it := reader.get("/v1/masterdata/catalogs/rice_variety/" + code); st != http.StatusOK || it["name"] != "Giống thử AC1" {
			t.Fatalf("%s đọc theo mã: %d %v", role, st, it)
		}
	}
	items, err := f.mod.Catalogs().List(admin.ctx(), port.CatalogRiceVariety)
	if err != nil || !slices.ContainsFunc(items, func(it port.CatalogItem) bool { return it.Code == code }) {
		t.Fatalf("port Catalogs.List thiếu giống: %v", err)
	}
	// App HTX nhận ở lần đồng bộ kế tiếp, kèm crop_code.
	chs, _ := pullAll(t, admin, f.mod.SyncSource(), authz.Scope{All: true}, port.SyncEntityCatalogItem, 0, 500)
	if got := byCode(t, chs)["rice_variety/"+code]; got == nil || got["attrs"].(map[string]any)["crop_code"] != "LUA" {
		t.Fatalf("đồng bộ thiếu giống: %v", got)
	}

	// Gửi lặp cùng Idempotency-Key → cùng mã, lịch sử một dòng.
	st, again := admin.do(f.admin, http.MethodPost, varietiesPath, key, map[string]any{"name": "Giống thử AC1", "attrs": variety("LUA", "95")})
	if (st != http.StatusOK && st != http.StatusCreated) || again["code"] != code { // middleware phát lại phản hồi gốc
		t.Fatalf("gửi lặp: %d %v", st, again)
	}
	if acts := historyActions(t, admin, "rice_variety", code); !slices.Equal(acts, []string{"created"}) {
		t.Fatalf("lịch sử: %v", acts)
	}

	// Máy chủ sinh mã: gửi code → 422.
	st, b = admin.adm(http.MethodPost, varietiesPath, map[string]any{"code": "OM_TU_DAT", "name": "Tự đặt mã", "attrs": variety("LUA", "95")})
	if field, _ := firstError(t, b); st != http.StatusUnprocessableEntity || field != "code" {
		t.Fatalf("gửi mã: %d %v", st, b)
	}
	// Loại cây phải tồn tại, đang hiện, và là loại cây (không phải mục cũ có nhóm cha).
	hidden := admin.newCrop(t, "Loại cây sẽ ẩn", "false")
	if st, b := admin.adm(http.MethodPost, cropsPath+"/"+hidden+"/deactivate", map[string]any{}); st != http.StatusOK {
		t.Fatalf("ẩn loại cây: %d %v", st, b)
	}
	for name, crop := range map[string]string{"mục cũ có cha": "LUA_CAC_LOAI", "đang ẩn": hidden, "không tồn tại": "KHONG_CO"} {
		st, b := admin.adm(http.MethodPost, varietiesPath, map[string]any{"name": "Giống " + name, "attrs": variety(crop, "90")})
		if field, _ := firstError(t, b); st != http.StatusUnprocessableEntity || field != "attrs.crop_code" {
			t.Fatalf("loại cây %s: %d %v", name, st, b)
		}
	}
}

// AC2 — Trùng tên (server): trong cùng loại cây; ẩn vẫn giữ tên, xóa thì không.
func TestAH2_AC2_DuplicateNameRejected(t *testing.T) {
	f := newFixture(t)
	f.withUsage(fakeUsage{})
	tid := f.seeded()
	admin := f.caller(tid, port.RoleMasterdataAdmin)

	// Ca chính: tạo "Giống Alpha", rồi trùng sau chuẩn hóa → 409 kèm lỗi tại trường name.
	alpha := admin.newVariety(t, "LUA", "Giống Alpha", "95")
	for _, dup := range []string{"giống alpha", " GIỐNG   ALPHA "} {
		st, b := admin.adm(http.MethodPost, varietiesPath, map[string]any{"name": dup, "attrs": variety("LUA", "100")})
		field, reason := firstError(t, b)
		if st != http.StatusConflict || b["code"] != "masterdata.catalog_item_name_taken" || field != "name" || !strings.Contains(reason, "đã tồn tại") {
			t.Fatalf("trùng %q: %d %v", dup, st, b)
		}
	}
	// Khác khoảng trắng bên trong thì là tên khác; cùng tên ở loại cây khác thì hợp lệ.
	admin.newVariety(t, "LUA", "Giống Al pha", "95")
	admin.newVariety(t, "RAU_MAU", "Giống Alpha", "40")
	// Đổi tên trùng qua PUT → 409.
	other := admin.newVariety(t, "LUA", "Giống Beta", "95")
	st, b := admin.adm(http.MethodPut, varietiesPath+"/"+other, map[string]any{"name": "GIỐNG ALPHA", "attrs": variety("LUA", "95")})
	if st != http.StatusConflict || b["code"] != "masterdata.catalog_item_name_taken" {
		t.Fatalf("PUT trùng tên: %d %v", st, b)
	}
	// Đổi sang loại cây khác đã có tên đó → 409; giữ nguyên tên và đổi số ngày → 200.
	if st, b := admin.adm(http.MethodPut, varietiesPath+"/"+other, map[string]any{"name": "Giống Beta", "attrs": variety("LUA", "99")}); st != http.StatusOK {
		t.Fatalf("PUT cùng tên của chính nó: %d %v", st, b)
	}
	// Giống đã ẩn vẫn giữ tên.
	if st, b := admin.adm(http.MethodPost, varietiesPath+"/"+alpha+"/deactivate", map[string]any{}); st != http.StatusOK {
		t.Fatalf("ẩn: %d %v", st, b)
	}
	if st, b := admin.adm(http.MethodPost, varietiesPath, map[string]any{"name": "Giống Alpha", "attrs": variety("LUA", "95")}); st != http.StatusConflict {
		t.Fatalf("giống ẩn vẫn chặn tên: %d %v", st, b)
	}
	// Giống đã xóa không chặn tên.
	gone := admin.newVariety(t, "LUA", "Giống sẽ xóa", "95")
	if st, b := admin.adm(http.MethodPost, varietiesPath+"/"+gone+"/delete", nil); st != http.StatusOK || b["outcome"] != "deleted" {
		t.Fatalf("xóa: %d %v", st, b)
	}
	admin.newVariety(t, "LUA", "Giống sẽ xóa", "95")

	// Loại cây: trùng trong các loại cây (kể cả đã ẩn); mục cũ không còn là loại cây thì không tính.
	if st, b := admin.adm(http.MethodPost, cropsPath, map[string]any{"name": "rau  MÀU", "attrs": map[string]string{"is_rice": "false"}}); st != http.StatusConflict || b["code"] != "masterdata.catalog_item_name_taken" {
		t.Fatalf("loại cây trùng tên: %d %v", st, b)
	}
	admin.newCrop(t, "Xoài", "false")
}

// AC3 — Giá trị sai (server): thời gian sinh trưởng ≤ 0 hoặc không phải số nguyên.
func TestAH2_AC3_GrowthDaysMustBePositive(t *testing.T) {
	f := newFixture(t)
	tid := f.seeded()
	admin := f.caller(tid, port.RoleMasterdataAdmin)
	existing := admin.newVariety(t, "LUA", "Giống kiểm nhập", "95")

	for _, bad := range []string{"0", "-5", "1.5", "abc", ""} {
		st, b := admin.adm(http.MethodPost, varietiesPath, map[string]any{"name": "Sai " + bad, "attrs": variety("LUA", bad)})
		field, reason := firstError(t, b)
		if st != http.StatusUnprocessableEntity || b["code"] != "validation_failed" || field != "attrs.growth_days" || reason == "" {
			t.Fatalf("POST growth_days=%q: %d %v", bad, st, b)
		}
		st, b = admin.adm(http.MethodPut, varietiesPath+"/"+existing, map[string]any{"name": "Giống kiểm nhập", "attrs": variety("LUA", bad)})
		if field, _ := firstError(t, b); st != http.StatusUnprocessableEntity || field != "attrs.growth_days" {
			t.Fatalf("PUT growth_days=%q: %d %v", bad, st, b)
		}
	}
	// Không có giới hạn trên.
	if st, b := admin.adm(http.MethodPost, varietiesPath, map[string]any{"name": "Giống dài ngày", "attrs": variety("LUA", "400")}); st != http.StatusCreated {
		t.Fatalf("400 ngày: %d %v", st, b)
	}
	// Thiếu tên → lỗi tại trường name.
	if st, b := admin.adm(http.MethodPost, varietiesPath, map[string]any{"name": " ", "attrs": variety("LUA", "90")}); st != http.StatusUnprocessableEntity {
		t.Fatalf("thiếu tên: %d %v", st, b)
	}
}

// AC4 — Đang được tham chiếu thì chỉ ẩn; chưa dùng thì xóa; vụ cũ vẫn đọc đúng tên.
func TestAH2_AC4_ReferencedItemOnlyHidden(t *testing.T) {
	f := newFixture(t)
	tid := f.seeded()
	admin := f.caller(tid, port.RoleMasterdataAdmin)
	reader := roleCaller(t, f, tid, "coop_manager")
	usage := fakeUsage{used: map[string]bool{}}
	f.withUsage(usage)
	listPath := "/v1/masterdata/catalogs/rice_variety?limit=200"

	// (a) Giống đang dùng → chỉ ẩn, đọc theo mã vẫn đúng tên.
	used := admin.newVariety(t, "LUA", "Giống đang gắn vụ", "95")
	usage.used["rice_variety/"+used] = true
	st, b := admin.adm(http.MethodPost, varietiesPath+"/"+used+"/delete", nil)
	if st != http.StatusOK || b["outcome"] != "hidden" || b["item"].(map[string]any)["active"] != false {
		t.Fatalf("xóa giống đang dùng: %d %v", st, b)
	}
	if st, it := reader.get("/v1/masterdata/catalogs/rice_variety/" + used); st != http.StatusOK || it["name"] != "Giống đang gắn vụ" || it["active"] != false {
		t.Fatalf("vụ cũ phải đọc đúng tên giống: %d %v", st, it)
	}
	pi, err := f.mod.Catalogs().Get(reader.ctx(), port.CatalogRiceVariety, used)
	if err != nil || pi.Name != "Giống đang gắn vụ" || pi.Active {
		t.Fatalf("port Get: %+v %v", pi, err)
	}
	if slices.Contains(listCodes(t, reader, listPath), used) || !slices.Contains(listCodes(t, reader, listPath+"&include_inactive=true"), used) {
		t.Fatal("giống đã ẩn không có trong danh sách chọn nhưng có khi include_inactive")
	}
	if acts := historyActions(t, admin, "rice_variety", used); !slices.Equal(acts, []string{"created", "deactivated"}) {
		t.Fatalf("lịch sử: %v", acts)
	}
	pool := f.env.Pool(t, migrations.PF, masterdata.DBRole)
	err = tenant.WithTenantTx(context.Background(), pool, tid, func(tx pgx.Tx) error {
		r, e := ledger.Verify(context.Background(), tx, "pf_masterdata.catalog_change", tid, "catalog:rice_variety:"+used)
		if e == nil && (!r.OK || r.RowsChecked != 2) {
			t.Errorf("chuỗi băm: %+v", r)
		}
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	// (a2) Xóa lại mục đã ẩn mà vẫn dùng: trả hidden, không ghi sổ thêm.
	if st, b := admin.adm(http.MethodPost, varietiesPath+"/"+used+"/delete", nil); st != http.StatusOK || b["outcome"] != "hidden" {
		t.Fatalf("xóa lại: %d %v", st, b)
	}
	if acts := historyActions(t, admin, "rice_variety", used); len(acts) != 2 {
		t.Fatalf("không ghi sổ thêm: %v", acts)
	}

	// (b) Giống chưa dùng → xóa: biến khỏi mọi danh sách, đồng bộ phát tombstone, lịch sử giữ.
	free := admin.newVariety(t, "LUA", "Giống chưa dùng", "95")
	_, cursor := pullAll(t, admin, f.mod.SyncSource(), authz.Scope{All: true}, port.SyncEntityCatalogItem, 0, 500)
	key := "k-" + ids.New().String()
	st, b = admin.do(f.admin, http.MethodPost, varietiesPath+"/"+free+"/delete", key, nil)
	if st != http.StatusOK || b["outcome"] != "deleted" {
		t.Fatalf("xóa giống chưa dùng: %d %v", st, b)
	}
	if st, again := admin.do(f.admin, http.MethodPost, varietiesPath+"/"+free+"/delete", key, nil); st != http.StatusOK || again["outcome"] != "deleted" {
		t.Fatalf("gửi lặp cùng khóa: %d %v", st, again)
	}
	for _, p := range []string{listPath, listPath + "&include_inactive=true"} {
		if slices.Contains(listCodes(t, reader, p), free) {
			t.Fatalf("mục đã xóa còn trong %s", p)
		}
	}
	if slices.Contains(adminListCodes(t, admin, varietiesPath+"?limit=200&include_inactive=true"), free) {
		t.Fatal("mục đã xóa còn trong danh sách Admin")
	}
	chs, _ := pullAll(t, admin, f.mod.SyncSource(), authz.Scope{All: true}, port.SyncEntityCatalogItem, cursor, 500)
	if !slices.ContainsFunc(chs, func(c syncport.Change) bool {
		return c.ID == port.CatalogItemSyncID("rice_variety", free) && c.Op == syncport.OpTombstone
	}) {
		t.Fatalf("đồng bộ phải phát tombstone: %v", chs)
	}
	if acts := historyActions(t, admin, "rice_variety", free); !slices.Equal(acts, []string{"created", "deleted"}) {
		t.Fatalf("lịch sử giữ nguyên: %v", acts)
	}
	for _, path := range []string{"/delete", "/reactivate"} {
		if st, b := admin.adm(http.MethodPost, varietiesPath+"/"+free+path, map[string]any{}); st != http.StatusNotFound {
			t.Fatalf("%s mục đã xóa: %d %v", path, st, b)
		}
	}

	// (c) Nguồn đếm lỗi → 503, không đổi gì.
	flaky := admin.newVariety(t, "LUA", "Giống khi nguồn đếm lỗi", "95")
	f.mod.UseCatalogUsage(fakeUsage{err: errors.New("hết thời gian chờ")})
	if st, b := admin.adm(http.MethodPost, varietiesPath+"/"+flaky+"/delete", nil); st != http.StatusServiceUnavailable || b["code"] != "masterdata.dependency_unavailable" {
		t.Fatalf("nguồn đếm lỗi: %d %v", st, b)
	}
	if st, it := reader.get("/v1/masterdata/catalogs/rice_variety/" + flaky); st != http.StatusOK || it["active"] != true {
		t.Fatalf("không đổi gì: %d %v", st, it)
	}

	// (d) Không nối nguồn đếm → coi như đang dùng, chỉ ẩn.
	bare := newFixture(t)
	bareTid := bare.seeded()
	bareAdmin := bare.caller(bareTid, port.RoleMasterdataAdmin)
	v := bareAdmin.newVariety(t, "LUA", "Giống không nguồn đếm", "95")
	if st, b := bareAdmin.adm(http.MethodPost, varietiesPath+"/"+v+"/delete", nil); st != http.StatusOK || b["outcome"] != "hidden" {
		t.Fatalf("thiếu nguồn đếm: %d %v", st, b)
	}
}

// AC4 (loại cây) — còn giống đang hiển thị thì chặn; có giống hoặc sâu bệnh trỏ tới thì chỉ ẩn; ẩn không cần lý do.
func TestAH2_AC4_CropTypeRules(t *testing.T) {
	f := newFixture(t)
	f.withUsage(fakeUsage{})
	tid := f.seeded()
	admin := f.caller(tid, port.RoleMasterdataAdmin)

	crop := admin.newCrop(t, "Cây thử AC4", "false")
	v1 := admin.newVariety(t, crop, "Giống 1", "60")
	v2 := admin.newVariety(t, crop, "Giống 2", "70")
	for _, path := range []string{cropsPath + "/" + crop + "/deactivate", cropsPath + "/" + crop + "/delete"} {
		st, b := admin.adm(http.MethodPost, path, map[string]any{})
		if st != http.StatusConflict || b["code"] != "masterdata.crop_type_has_visible_varieties" || !strings.Contains(b["detail"].(string), "2") {
			t.Fatalf("%s: %d %v", path, st, b)
		}
	}
	for _, v := range []string{v1, v2} {
		if st, b := admin.adm(http.MethodPost, varietiesPath+"/"+v+"/deactivate", map[string]any{}); st != http.StatusOK {
			t.Fatalf("ẩn giống: %d %v", st, b)
		}
	}
	// Ẩn không lý do → 200, lý do hệ thống; có giống (đã ẩn) nên xóa chỉ ẩn.
	st, b := admin.adm(http.MethodPost, cropsPath+"/"+crop+"/delete", nil)
	if st != http.StatusOK || b["outcome"] != "hidden" || b["item"].(map[string]any)["deactivation_reason"] == nil {
		t.Fatalf("ẩn loại cây còn giống đã ẩn: %d %v", st, b)
	}
	// Hiện lại giống khi loại cây đang ẩn → 409; hiện loại cây trước thì được.
	if st, b := admin.adm(http.MethodPost, varietiesPath+"/"+v1+"/reactivate", map[string]any{}); st != http.StatusConflict || b["code"] != "masterdata.crop_type_hidden" {
		t.Fatalf("hiện giống khi loại cây ẩn: %d %v", st, b)
	}
	if st, b := admin.adm(http.MethodPost, cropsPath+"/"+crop+"/reactivate", map[string]any{}); st != http.StatusOK {
		t.Fatalf("hiện loại cây: %d %v", st, b)
	}
	if st, b := admin.adm(http.MethodPost, varietiesPath+"/"+v1+"/reactivate", map[string]any{}); st != http.StatusOK {
		t.Fatalf("hiện giống: %d %v", st, b)
	}
	// Loại cây có sâu bệnh trỏ tới → chỉ ẩn; loại cây trống → xóa được.
	pestCrop := admin.newCrop(t, "Cây có sâu bệnh", "false")
	if st, b := admin.adm(http.MethodPost, "/v1/masterdata/catalogs/pest/items", map[string]any{"code": "sau_ah2", "name": "Sâu AH2",
		"attrs": map[string]string{"kind": "sau_hai", "affected_crops": pestCrop}}); st != http.StatusCreated {
		t.Fatalf("tạo sâu bệnh: %d %v", st, b)
	}
	if st, b := admin.adm(http.MethodPost, cropsPath+"/"+pestCrop+"/delete", nil); st != http.StatusOK || b["outcome"] != "hidden" {
		t.Fatalf("loại cây có sâu bệnh: %d %v", st, b)
	}
	empty := admin.newCrop(t, "Cây trống", "false")
	if st, b := admin.adm(http.MethodPost, cropsPath+"/"+empty+"/delete", nil); st != http.StatusOK || b["outcome"] != "deleted" {
		t.Fatalf("loại cây trống: %d %v", st, b)
	}
	// Ẩn có gửi lý do thì vẫn kiểm độ dài.
	other := admin.newCrop(t, "Cây kiểm lý do", "true")
	if st, b := admin.adm(http.MethodPost, cropsPath+"/"+other+"/deactivate", map[string]any{"reason": "ngắn"}); st != http.StatusUnprocessableEntity {
		t.Fatalf("lý do ngắn: %d %v", st, b)
	}
	// Mục cũ có nhóm cha chỉ xem; chỉ loại cây và giống có thao tác xóa.
	for _, path := range []string{"/deactivate", "/delete"} {
		if st, b := admin.adm(http.MethodPost, cropsPath+"/XOAI"+path, map[string]any{}); st != http.StatusConflict || b["code"] != "masterdata.catalog_item_read_only" {
			t.Fatalf("mục cũ %s: %d %v", path, st, b)
		}
	}
	if st, b := admin.adm(http.MethodPut, cropsPath+"/XOAI", map[string]any{"name": "Xoài mới", "attrs": map[string]string{"parent_code": "CAY_AN_QUA", "is_rice": "false"}}); st != http.StatusConflict {
		t.Fatalf("sửa mục cũ: %d %v", st, b)
	}
	if st, b := admin.adm(http.MethodPost, "/v1/masterdata/catalogs/material/items/PHAN_URE/delete", nil); st != http.StatusUnprocessableEntity || b["code"] != "masterdata.catalog_delete_unsupported" {
		t.Fatalf("danh mục không có xóa: %d %v", st, b)
	}
}

// AC5 — Quyền: chỉ vai quản trị danh mục ghi; TV, ĐD chỉ xem.
func TestAH2_AC5_OnlyCatalogAdminWrites(t *testing.T) {
	f := newFixture(t)
	f.withUsage(fakeUsage{})
	tid := f.seeded()
	admin := f.caller(tid, port.RoleMasterdataAdmin)
	v := admin.newVariety(t, "LUA", "Giống để thử quyền", "95")

	for name, c := range map[string]caller{"ĐD": roleCaller(t, f, tid, "coop_manager"), "TV": roleCaller(t, f, tid, "farmer"), "không vai": f.caller(tid)} {
		for what, call := range map[string]func() (int, map[string]any){
			"tạo": func() (int, map[string]any) {
				return c.adm(http.MethodPost, varietiesPath, map[string]any{"name": "Trái phép", "attrs": variety("LUA", "90")})
			},
			"sửa": func() (int, map[string]any) {
				return c.adm(http.MethodPut, varietiesPath+"/"+v, map[string]any{"name": "Đổi tên", "attrs": variety("LUA", "90")})
			},
			"ẩn": func() (int, map[string]any) {
				return c.adm(http.MethodPost, varietiesPath+"/"+v+"/deactivate", map[string]any{})
			},
			"hiện": func() (int, map[string]any) {
				return c.adm(http.MethodPost, varietiesPath+"/"+v+"/reactivate", map[string]any{})
			},
			"xóa": func() (int, map[string]any) { return c.adm(http.MethodPost, varietiesPath+"/"+v+"/delete", nil) },
			"tạo cây": func() (int, map[string]any) {
				return c.adm(http.MethodPost, cropsPath, map[string]any{"name": "Cây trái phép", "attrs": map[string]string{"is_rice": "false"}})
			},
		} {
			if st, b := call(); st != http.StatusForbidden || b["code"] != "forbidden" {
				t.Fatalf("%s %s: %d %v", name, what, st, b)
			}
		}
		if !slices.Contains(listCodes(t, c, "/v1/masterdata/catalogs/rice_variety?limit=200"), v) {
			t.Fatalf("%s phải xem được danh sách", name)
		}
		if st, _ := c.get("/v1/masterdata/catalogs/rice_variety/" + v); st != http.StatusOK {
			t.Fatalf("%s phải đọc được theo mã: %d", name, st)
		}
	}
}

// Dữ liệu khởi tạo theo mô hình mới và các luật giữ dữ liệu cũ.
func TestAH2_SeedAndLegacyData(t *testing.T) {
	f := newFixture(t)
	tid := f.seeded()
	admin := f.caller(tid, port.RoleMasterdataAdmin)

	crops, err := f.mod.Catalogs().List(admin.ctx(), port.CatalogCrop)
	if err != nil {
		t.Fatal(err)
	}
	isRice := map[string]string{}
	for _, c := range crops {
		if c.Attrs[port.AttrParentCode] == "" {
			isRice[c.Code] = c.Attrs[port.AttrIsRice]
		}
	}
	if len(isRice) != 3 || isRice["LUA"] != "true" || isRice["RAU_MAU"] != "false" || isRice["CAY_AN_QUA"] != "false" {
		t.Fatalf("loại cây khởi tạo: %v", isRice)
	}
	varieties, err := f.mod.Catalogs().List(admin.ctx(), port.CatalogRiceVariety)
	if err != nil || len(varieties) != 6 {
		t.Fatalf("giống khởi tạo: %d %v", len(varieties), err)
	}
	for _, v := range varieties {
		if v.Attrs[port.AttrCropCode] != "LUA" {
			t.Fatalf("giống %s phải thuộc loại cây LUA: %v", v.Code, v.Attrs)
		}
	}
	// Sửa giống cũ từ màn mới (không gửi năng suất) → năng suất cũ giữ nguyên.
	st, b := admin.adm(http.MethodPut, varietiesPath+"/OM5451", map[string]any{"name": "OM5451", "attrs": variety("LUA", "96")})
	if st != http.StatusOK || b["attrs"].(map[string]any)["expected_yield_t_per_ha"] != "6.75" || b["attrs"].(map[string]any)["growth_days"] != "96" {
		t.Fatalf("giữ năng suất cũ: %d %v", st, b)
	}
	// Loại cây: "Lúa các loại" là mục cũ, không phải loại cây nên không chặn tên mới.
	if st, b := admin.adm(http.MethodPut, cropsPath+"/LUA", map[string]any{"name": "Lúa", "attrs": map[string]string{"is_rice": "true"}}); st != http.StatusOK || b["attrs"].(map[string]any)["is_rice"] != "true" {
		t.Fatalf("sửa loại cây LUA: %d %v", st, b)
	}
	admin.newCrop(t, "Lúa các loại", "true")
}
