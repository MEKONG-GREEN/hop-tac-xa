package handler_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	port "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ledger"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tenant"
)

const (
	sopsPath     = "/v1/cropcycle/sops"
	versionsPath = "/v1/cropcycle/sop-versions/"
)

// hasField cho biết phản hồi lỗi có báo đúng trường field.
func hasField(r resp, field string) bool {
	for _, e := range list(r.body, "errors") {
		if str(e, "field") == field {
			return true
		}
	}
	return false
}

func expectField(t *testing.T, r resp, status int, c, field, what string) {
	t.Helper()
	expectCode(t, r, status, c, what)
	if !hasField(r, field) {
		t.Fatalf("%s: muốn lỗi ở trường %s: %s", what, field, r.raw)
	}
}

// createDraft tạo SOP nháp (tên name) và trả (ref mẫu, id phiên bản).
func (f *fixture) createDraft(name string, extra map[string]any) (tmpl, ver string) {
	f.t.Helper()
	r := f.sopAdmin.adminPost(sopsPath, sopBody(name, extra))
	expect(f.t, r, http.StatusCreated, "tạo SOP nháp "+name)
	return str(r.body, "ref"), str(list(r.body, "versions")[0], "id")
}

// HTX US-CAT-01 AC-1/AC-6: tạo SOP; thứ tự không liên tục bị từ chối; tên trùng bị chặn;
// phát hành một bước bởi vai soạn (AH-8, thay R-56) và phải có việc thu hoạch;
// phiên bản đã phát hành bất biến; phát pf.cropcycle.sop.published.
func TestSOPAuthoringAndPublish_HTX_US_CAT_01(t *testing.T) {
	f := newFixture(t)
	bad := sopSteps()
	bad[2]["seq"] = 4
	expectCode(t, f.sopAdmin.adminPost(sopsPath, sopBody("SOP lỗi", map[string]any{"code": "SOP-X", "steps": bad})),
		http.StatusUnprocessableEntity, "cropcycle.sop_step_order", "việc không liên tục")

	r := f.sopAdmin.adminPost(sopsPath, sopBody("SOP lúa OM5451", map[string]any{"code": "SOP-OM", "standard": "vietgap"}))
	expect(t, r, http.StatusCreated, "tạo SOP")
	tmpl, ver := str(r.body, "ref"), str(list(r.body, "versions")[0], "id")
	if str(r.body, "status") != "draft" || num(list(r.body, "versions")[0], "harvest_day_offset") != 95 {
		t.Fatalf("SOP mới: %s", r.raw)
	}
	expectCode(t, f.sopAdmin.adminPost(sopsPath, sopBody("  sop lúa   OM5451 ", map[string]any{"code": "SOP-OM2"})),
		http.StatusConflict, "cropcycle.sop_name_taken", "tên trùng (chuẩn hóa)")
	expectCode(t, f.sopAdmin.adminPost(sopsPath, sopBody("Tên khác", map[string]any{"code": "SOP-OM"})),
		http.StatusConflict, "cropcycle.sop_code_taken", "mã trùng")

	// Ứng dụng chưa thấy mẫu nháp.
	if n := len(list(f.managerA().get(sopsPath).body, "items")); n != 0 {
		t.Fatalf("mẫu nháp không được hiện ở App API: %d", n)
	}
	// Vai param_approver không soạn, không phát hành SOP.
	expectCode(t, f.approver.adminPost(versionsPath+ver+"/publish", nil), http.StatusForbidden, "forbidden", "param_approver không phát hành SOP")

	// Thiếu việc thu hoạch → không phát hành được.
	noHarvest := map[string]any{"stages": sopStages()[:2], "steps": sopSteps()[:4]}
	expect(t, f.sopAdmin.adminPost(versionsPath+ver+"/steps", noHarvest), http.StatusOK, "sửa nháp")
	expectCode(t, f.sopAdmin.adminPost(versionsPath+ver+"/publish", nil), http.StatusUnprocessableEntity,
		"cropcycle.sop_harvest_step_missing", "thiếu việc thu hoạch")
	full := map[string]any{"stages": sopStages(), "steps": sopSteps()}
	expect(t, f.sopAdmin.adminPost(versionsPath+ver+"/steps", full), http.StatusOK, "sửa nháp lại")

	r = f.sopAdmin.adminPost(versionsPath+ver+"/publish", nil)
	expect(t, r, http.StatusOK, "phát hành bởi chính người soạn")
	if str(r.body, "status") != "published" || num(r.body, "published_version") != 1 {
		t.Fatalf("sau phát hành: %s", r.raw)
	}
	expectCode(t, f.sopAdmin.adminPost(versionsPath+ver+"/steps", full), http.StatusConflict, "cropcycle.sop_state", "sửa bản đã phát hành")
	// BR-07: trigger chặn sửa bản đã phát hành ngay cả khi đi thẳng vào DB bằng vai ứng dụng.
	err := tenant.WithTenantTx(context.Background(), f.db, f.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), "UPDATE pf_coop.sop_version SET step_count = 0 WHERE id = $1::uuid", ver)
		return err
	})
	if !errors.Is(ledger.Classify(err), ledger.ErrImmutable) {
		t.Fatalf("UPDATE bản đã phát hành phải bị trigger chặn: %v", err)
	}
	ev := f.outbox(port.EventSOPPublished)
	if len(ev) != 1 || str(ev[0], "template_ref") != tmpl || str(ev[0], "authored_by") != str(ev[0], "published_by") {
		t.Fatalf("sự kiện sop.published (người soạn tự phát hành): %v", ev)
	}
	// Phiên bản mới chép giai đoạn và việc; chỉ một bản nháp mỗi lúc.
	r = f.sopAdmin.adminPost(sopsPath+"/"+tmpl+"/versions", map[string]any{"change_note": "Điều chỉnh cửa sổ bón phân"})
	expect(t, r, http.StatusCreated, "phiên bản 2")
	v2 := list(r.body, "versions")[1]
	if len(list(r.body, "versions")) != 2 || len(list(v2, "steps")) != 5 || len(list(v2, "stages")) != 3 {
		t.Fatalf("phiên bản 2 phải chép 3 giai đoạn, 5 việc: %s", r.raw)
	}
	expectCode(t, f.sopAdmin.adminPost(sopsPath+"/"+tmpl+"/versions", nil), http.StatusConflict, "cropcycle.sop_state", "hai bản nháp")
	// App API thấy mẫu đã ban hành và chỉ phiên bản đã phát hành.
	r = f.managerA().get(sopsPath + "/" + tmpl)
	expect(t, r, http.StatusOK, "đọc SOP App API")
	if len(list(r.body, "versions")) != 1 {
		t.Fatalf("App API chỉ thấy phiên bản đã phát hành: %s", r.raw)
	}
	// Ngừng dùng (không xóa — HTX US-CAT-01 AC-2).
	expectCode(t, f.sopAdmin.adminPost(sopsPath+"/"+tmpl+"/retire", map[string]any{"reason": "ngắn"}), http.StatusUnprocessableEntity,
		"validation_failed", "lý do quá ngắn")
	r = f.sopAdmin.adminPost(sopsPath+"/"+tmpl+"/retire", map[string]any{"reason": "Thay bằng quy trình SRP mới"})
	expect(t, r, http.StatusOK, "ngừng dùng")
	if str(r.body, "status") != "retired" {
		t.Fatalf("trạng thái: %s", r.raw)
	}
}

// HTX US-CAT-03: nhân bản giữ nguyên giai đoạn, việc và thứ tự; sửa bản sao không đổi nguồn;
// tên bản sao trùng → "Tên SOP này đã tồn tại".
func TestSOPClone_HTX_US_CAT_03(t *testing.T) {
	f := newFixture(t)
	src, srcVer := f.publishedSOP("SOP chuẩn OM5451", "")
	expectCode(t, f.sopAdmin.adminPost(sopsPath+"/"+src+"/clone", map[string]any{"name": "SOP chuẩn OM5451"}),
		http.StatusConflict, "cropcycle.sop_name_taken", "tên bản sao trùng nguồn")
	r := f.sopAdmin.adminPost(sopsPath+"/"+src+"/clone", map[string]any{"name": "SOP ST25 (bản sao)"})
	expect(t, r, http.StatusCreated, "nhân bản")
	clone := list(r.body, "versions")[0]
	if str(r.body, "source_template_ref") != src || str(clone, "source_version_ref") != srcVer || str(r.body, "status") != "draft" ||
		str(r.body, "crop") != "LUA" || str(r.body, "anchor_name") != "Ngày xuống giống" {
		t.Fatalf("nguồn của bản sao: %s", r.raw)
	}
	cs := list(clone, "steps")
	if len(cs) != 5 || str(cs[0], "name") != "Làm đất" || str(cs[4], "activity_code") != "thu_hoach" || len(list(clone, "stages")) != 3 {
		t.Fatalf("bản sao phải giữ giai đoạn, việc và thứ tự: %v", cs)
	}
	edited := sopSteps()
	edited[0]["name"] = "Cày ải"
	expect(t, f.sopAdmin.adminPost(versionsPath+str(clone, "id")+"/steps", map[string]any{"stages": sopStages(), "steps": edited}), http.StatusOK, "sửa bản sao")
	r = f.sopAdmin.adminGet(versionsPath + srcVer)
	expect(t, r, http.StatusOK, "đọc nguồn")
	if str(list(get(r.body, "version").(map[string]any), "steps")[0], "name") != "Làm đất" {
		t.Fatalf("sửa bản sao làm đổi nguồn: %s", r.raw)
	}
}

// AH-8 AC1: soạn nháp — SOP ở trạng thái Nháp, chưa dùng cho vụ.
func TestSOPDraft_AH8_AC1(t *testing.T) {
	f := newFixture(t)
	idemKey := key()
	body := sopBody("SOP lúa OM5451 mùa khô", map[string]any{"variety_code": "OM5451"}) // không gửi code → mã tự sinh
	r := f.sopAdmin.adminPostKey(sopsPath, body, idemKey)
	expect(t, r, http.StatusCreated, "lưu nháp")
	tmpl, ver := str(r.body, "ref"), str(list(r.body, "versions")[0], "id")
	if str(r.body, "status") != "draft" || str(r.body, "crop") != "LUA" || str(r.body, "variety_code") != "OM5451" ||
		str(r.body, "anchor_name") != "Ngày xuống giống" || !strings.HasPrefix(str(r.body, "code"), "SOP-") ||
		len(list(list(r.body, "versions")[0], "stages")) != 3 {
		t.Fatalf("SOP nháp: %s", r.raw)
	}
	mat := list(list(list(r.body, "versions")[0], "steps")[2], "materials")
	if len(mat) != 1 || str(mat[0], "name") != "URE" || str(mat[0], "unit") != "kg" || str(mat[0], "dose_per_ha") != "50" {
		t.Fatalf("vật tư của việc bón: %s", r.raw)
	}
	// Gọi lặp cùng Idempotency-Key → cùng SOP, không tạo bản ghi thứ hai.
	again := f.sopAdmin.adminPostKey(sopsPath, body, idemKey)
	if again.status != http.StatusCreated || str(again.body, "ref") != tmpl {
		t.Fatalf("gọi lặp cùng khóa: %d %s", again.status, again.raw)
	}
	if n := len(list(f.sopAdmin.adminGet(sopsPath).body, "items")); n != 1 {
		t.Fatalf("chỉ một SOP sau khi gọi lặp: %d", n)
	}
	// Chưa dùng cho vụ: App API không thấy; mở vụ với bản nháp bị từ chối.
	if n := len(list(f.managerA().get(sopsPath).body, "items")); n != 0 {
		t.Fatalf("mẫu nháp không hiện ở App API: %d", n)
	}
	expectCode(t, f.managerA().get(sopsPath+"/"+tmpl), http.StatusNotFound, "cropcycle.sop_not_found", "App API đọc SOP nháp")
	expectCode(t, f.farmer(f.h1).open(f.p1, map[string]any{"sop_version_id": ver}), http.StatusUnprocessableEntity,
		"cropcycle.sop_not_published", "mở vụ với SOP nháp")
	// Còn nháp: sửa thông tin chung được.
	r = f.sopAdmin.adminPost(sopsPath+"/"+tmpl+"/info", map[string]any{"name": "SOP lúa OM5451 (sửa)", "crop": "LUA", "variety_code": "ST25",
		"anchor_name": "Ngày sạ"})
	expect(t, r, http.StatusOK, "sửa thông tin chung")
	if str(r.body, "name") != "SOP lúa OM5451 (sửa)" || str(r.body, "variety_code") != "ST25" || str(r.body, "anchor_name") != "Ngày sạ" {
		t.Fatalf("thông tin sau sửa: %s", r.raw)
	}
	// Đã phát hành: thông tin chung bị khóa.
	expect(t, f.sopAdmin.adminPost(versionsPath+ver+"/publish", nil), http.StatusOK, "phát hành")
	expectCode(t, f.sopAdmin.adminPost(sopsPath+"/"+tmpl+"/info", map[string]any{"name": "Đổi tên", "crop": "LUA", "anchor_name": "Ngày sạ"}),
		http.StatusConflict, "cropcycle.sop_state", "sửa thông tin sau phát hành")
}

// AH-8 AC2: server từ chối và chỉ rõ trường lỗi.
func TestSOPValidation_AH8_AC2(t *testing.T) {
	f := newFixture(t)
	f.createDraft("SOP có sẵn", nil)
	u := http.StatusUnprocessableEntity
	cases := []struct {
		name   string
		status int
		code   string
		field  string
		body   func() map[string]any
	}{
		{"thứ tự giai đoạn không liên tục", u, "cropcycle.sop_step_order", "stages[1].seq", func() map[string]any {
			s := sopStages()
			s[1]["seq"] = 3
			return sopBody("A1", map[string]any{"stages": s})
		}},
		{"giai đoạn Từ > Đến", u, "validation_failed", "stages[1].to_day", func() map[string]any {
			s := sopStages()
			s[1]["from_day"], s[1]["to_day"] = 50, 10
			return sopBody("A2", map[string]any{"stages": s})
		}},
		{"cửa sổ Từ > Đến", u, "validation_failed", "steps[2].window.to", func() map[string]any {
			st := sopSteps()
			st[2]["window"] = dateRange(20, 5)
			return sopBody("A3", map[string]any{"steps": st})
		}},
		{"liều bằng 0", u, "validation_failed", "steps[2].materials[0].dose_per_ha", func() map[string]any {
			return sopBody("A4", map[string]any{"steps": withMaterials(map[string]any{"material_code": "URE", "dose_per_ha": "0", "unit": "kg"})})
		}},
		{"liều âm", u, "validation_failed", "steps[2].materials[0].dose_per_ha", func() map[string]any {
			return sopBody("A5", map[string]any{"steps": withMaterials(map[string]any{"material_code": "URE", "dose_per_ha": "-1", "unit": "kg"})})
		}},
		{"thiếu đơn vị của vật tư tự do", u, "validation_failed", "steps[2].materials[0].unit", func() map[string]any {
			return sopBody("A6", map[string]any{"steps": withMaterials(map[string]any{"name": "Phân tự pha", "dose_per_ha": "1", "unit": ""})})
		}},
		{"vật tư lặp trong một việc", u, "validation_failed", "steps[2].materials[1].material_code", func() map[string]any {
			return sopBody("A7", map[string]any{"steps": withMaterials(
				map[string]any{"material_code": "URE", "dose_per_ha": "1", "unit": "kg"}, map[string]any{"material_code": "URE", "dose_per_ha": "2", "unit": "kg"})})
		}},
		{"vật tư ngừng dùng", u, "cropcycle.catalog_invalid", "steps[2].materials[0].material_code", func() map[string]any {
			return sopBody("A8", map[string]any{"steps": withMaterials(map[string]any{"material_code": "OLD", "dose_per_ha": "1", "unit": "kg"})})
		}},
		{"tên giai đoạn trùng", u, "validation_failed", "stages[1].name", func() map[string]any {
			s := sopStages()
			s[1]["name"] = " chuẩn   BỊ "
			return sopBody("A9", map[string]any{"stages": s})
		}},
		{"tên việc trùng trong giai đoạn", u, "validation_failed", "steps[1].name", func() map[string]any {
			st := sopSteps()
			st[1]["name"] = "làm đất"
			return sopBody("A10", map[string]any{"steps": st})
		}},
		{"giống không thuộc loại cây", u, "validation_failed", "variety_code", func() map[string]any {
			return sopBody("A11", map[string]any{"variety_code": "ARA"})
		}},
		{"loại cây là mục con", u, "cropcycle.catalog_invalid", "crop", func() map[string]any {
			return sopBody("A12", map[string]any{"crop": "CON"})
		}},
		{"thiếu loại cây", u, "validation_failed", "crop", func() map[string]any { return sopBody("A13", map[string]any{"crop": ""}) }},
		{"thiếu tên mốc gốc", u, "validation_failed", "anchor_name", func() map[string]any {
			return sopBody("A14", map[string]any{"anchor_name": "  "})
		}},
		{"thiếu cửa sổ", u, "validation_failed", "steps[0].window.kind", func() map[string]any {
			st := sopSteps()
			delete(st[0], "window")
			return sopBody("A15", map[string]any{"steps": st})
		}},
		{"tên SOP trùng (chuẩn hóa)", http.StatusConflict, "cropcycle.sop_name_taken", "", func() map[string]any {
			return sopBody("  sop CÓ   sẵn ", nil)
		}},
	}
	for _, tc := range cases {
		r := f.sopAdmin.adminPost(sopsPath, tc.body())
		if tc.field == "" {
			expectCode(t, r, tc.status, tc.code, tc.name)
		} else {
			expectField(t, r, tc.status, tc.code, tc.field, tc.name)
		}
	}
	// Nhiều lỗi cùng lúc: báo đủ mọi trường sai, không dừng ở lỗi đầu.
	s := sopStages()
	s[1]["from_day"], s[1]["to_day"] = 50, 10
	r := f.sopAdmin.adminPost(sopsPath, sopBody("B1", map[string]any{"stages": s,
		"steps": withMaterials(map[string]any{"material_code": "URE", "dose_per_ha": "0", "unit": "kg"})}))
	expectField(t, r, u, "validation_failed", "stages[1].to_day", "nhiều lỗi")
	if !hasField(r, "steps[2].materials[0].dose_per_ha") {
		t.Fatalf("phải báo cả lỗi liều: %s", r.raw)
	}
	// Sửa nháp cũng kiểm như khi tạo.
	_, ver := f.createDraft("SOP sửa nháp", nil)
	st := sopSteps()
	st[1]["window"] = dateRange(9, 1)
	expectField(t, f.sopAdmin.adminPost(versionsPath+ver+"/steps", map[string]any{"stages": sopStages(), "steps": st}), u,
		"validation_failed", "steps[1].window.to", "sửa nháp")
	expectField(t, f.sopAdmin.adminPost(versionsPath+ver+"/steps", map[string]any{"steps": st}), u, "validation_failed", "stages", "thiếu stages")
}

// withMaterials trả việc mẫu với việc bón (thứ 3) mang các dòng vật tư cho trước.
func withMaterials(ms ...map[string]any) []map[string]any {
	st := sopSteps()
	st[2]["materials"] = ms
	return st
}

// AH-8 AC3: bốn loại cửa sổ, ba mốc, số âm; hai trường cũ do server tính.
func TestSOPWindowKinds_AH8_AC3(t *testing.T) {
	f := newFixture(t)
	stages := []map[string]any{{"seq": 1, "name": "Giai đoạn chính", "from_day": 0, "to_day": 10}}
	task := func(seq int, name, act string, w map[string]any) map[string]any {
		return map[string]any{"seq": seq, "stage_seq": 1, "task_seq": seq, "name": name, "activity_code": act, "window": w}
	}
	steps := []map[string]any{
		task(1, "Theo ngày NXG âm", "lam_dat", map[string]any{"kind": "date_range", "anchor": "nxg", "from": -5, "to": -1}),
		task(2, "Mở", "kiem_tra", map[string]any{"kind": "open", "anchor": "nxg", "from": 7}),
		task(3, "Theo giai đoạn", "bon_phan", map[string]any{"kind": "stage"}),
		task(4, "Không ngày", "cat_tia", map[string]any{"kind": "none"}),
		task(5, "Theo ngày NTH", "kiem_tra", map[string]any{"kind": "date_range", "anchor": "nth", "from": -10, "to": -5}),
		task(6, "Theo ngày NTT", "khac", map[string]any{"kind": "date_range", "anchor": "ntt", "from": 0, "to": 3}),
		task(7, "Thu hoạch", "thu_hoach", map[string]any{"kind": "date_range", "anchor": "nxg", "from": 100, "to": 105}),
	}
	r := f.sopAdmin.adminPost(sopsPath, sopBody("SOP bốn loại cửa sổ", map[string]any{"stages": stages, "steps": steps}))
	expect(t, r, http.StatusCreated, "tạo SOP bốn loại cửa sổ")
	got := list(list(r.body, "versions")[0], "steps")
	want := []struct {
		kind    string
		off     float64
		undated bool
	}{{"date_range", -5, false}, {"open", 7, false}, {"stage", 0, false}, {"none", 0, true}, {"date_range", 90, false}, {"date_range", 0, true}, {"date_range", 100, false}}
	for i, w := range want {
		s := got[i]
		if str(s, "window", "kind") != w.kind || num(s, "day_offset") != w.off || (get(s, "undated") == true) != w.undated || num(s, "window_days") != 0 {
			t.Errorf("việc %d: %v", i+1, s)
		}
	}
	// Client gửi day_offset/window_days riêng thì bị server ghi đè.
	steps[0]["day_offset"], steps[0]["window_days"] = 33, 9
	r = f.sopAdmin.adminPost(sopsPath, sopBody("SOP ghi đè trường cũ", map[string]any{"stages": stages, "steps": steps}))
	expect(t, r, http.StatusCreated, "tạo SOP")
	if s := list(list(r.body, "versions")[0], "steps")[0]; num(s, "day_offset") != -5 || num(s, "window_days") != 0 {
		t.Fatalf("server phải tự tính day_offset/window_days: %v", s)
	}
	// Theo giai đoạn khi giai đoạn chưa có khoảng ngày → từ chối.
	noRange := []map[string]any{{"seq": 1, "name": "Chưa có ngày"}}
	expectField(t, f.sopAdmin.adminPost(sopsPath, sopBody("SOP giai đoạn chưa có ngày", map[string]any{"stages": noRange,
		"steps": []map[string]any{task(1, "Việc", "khac", map[string]any{"kind": "stage"})}})),
		http.StatusUnprocessableEntity, "validation_failed", "steps[0].window.kind", "cửa sổ theo giai đoạn")
}

// AH-8 AC4: phát hành một bước bởi vai soạn; SOP dùng được cho vụ mở mới; nhiều SOP cùng loại cây và giống cùng đang áp dụng.
func TestSOPPublish_AH8_AC4(t *testing.T) {
	f := newFixture(t)
	t1, v1 := f.createDraft("SOP OM5451 bản 1", map[string]any{"variety_code": "OM5451"})
	t2, v2 := f.createDraft("SOP OM5451 bản 2", map[string]any{"variety_code": "OM5451"})
	r := f.sopAdmin.adminPost(versionsPath+v1+"/publish", nil)
	expect(t, r, http.StatusOK, "phát hành SOP 1")
	if str(r.body, "status") != "published" {
		t.Fatalf("trạng thái Đang áp dụng: %s", r.raw)
	}
	expect(t, f.sopAdmin.adminPost(versionsPath+v2+"/publish", nil), http.StatusOK, "phát hành SOP 2 (cùng loại cây, cùng giống — KH-11)")
	for _, ref := range []string{t1, t2} {
		if str(f.sopAdmin.adminGet(sopsPath+"/"+ref).body, "status") != "published" {
			t.Fatalf("cả hai SOP cùng đang áp dụng: %s", ref)
		}
	}
	ev := f.outbox(port.EventSOPPublished)
	if len(ev) != 2 || num(ev[0], "harvest_window_days") != 0 || num(ev[0], "harvest_day_offset") != 95 || str(ev[0], "authored_by") != str(ev[0], "published_by") {
		t.Fatalf("sự kiện sop.published: %v", ev)
	}
	// Dùng cho vụ mở mới: ngày thu hoạch dự kiến = mốc gốc + Từ của việc Thu hoạch cuối; ngày mở khai = đúng ngày đó.
	rs := f.farmer(f.h1).open(f.p1, map[string]any{"sop_version_id": v1, "sowing_date": "2026-09-20"})
	expect(t, rs, http.StatusCreated, "mở vụ với SOP vừa phát hành")
	s := get(rs.body, "season")
	if str(s, "sop_version_id") != v1 || str(s, "expected_harvest_date") != "2026-12-24" || str(s, "harvest_open_date") != "2026-12-24" {
		t.Fatalf("vụ mở bằng SOP mới: %s", rs.raw)
	}
}

// AH-8 AC5: chặn phát hành (EX-05, Q-06) kèm trường cần sửa.
func TestSOPPublishBlocked_AH8_AC5(t *testing.T) {
	f := newFixture(t)
	u := http.StatusUnprocessableEntity
	withHarvest := func(w map[string]any) []map[string]any {
		st := sopSteps()
		st[4]["window"] = w
		return st
	}
	cases := []struct {
		name  string
		code  string
		field string
		extra map[string]any
	}{
		{"SOP rỗng", "cropcycle.sop_empty", "stages", map[string]any{"stages": []map[string]any{}, "steps": []map[string]any{}}},
		{"giai đoạn chưa có việc", "cropcycle.sop_empty", "stages[3]", map[string]any{
			"stages": append(sopStages(), map[string]any{"seq": 4, "name": "Giai đoạn trống"})}},
		{"không có việc Thu hoạch", "cropcycle.sop_harvest_step_missing", "steps", map[string]any{"stages": sopStages()[:2], "steps": sopSteps()[:4]}},
		{"Thu hoạch cuối cửa sổ Mở", "cropcycle.sop_harvest_window", "steps[4].window",
			map[string]any{"steps": withHarvest(map[string]any{"kind": "open", "anchor": "nxg", "from": 95})}},
		{"Thu hoạch cuối Không ngày", "cropcycle.sop_harvest_window", "steps[4].window",
			map[string]any{"steps": withHarvest(map[string]any{"kind": "none"})}},
		{"Thu hoạch cuối mốc NTH", "cropcycle.sop_harvest_window", "steps[4].window",
			map[string]any{"steps": withHarvest(map[string]any{"kind": "date_range", "anchor": "nth", "from": 0, "to": 1})}},
		{"Thu hoạch cuối mốc NTT", "cropcycle.sop_harvest_window", "steps[4].window",
			map[string]any{"steps": withHarvest(map[string]any{"kind": "date_range", "anchor": "ntt", "from": 0, "to": 1})}},
		{"việc dùng NTH nhưng Thu hoạch không có cửa sổ ngày", "cropcycle.sop_harvest_window", "steps[4].window", map[string]any{"steps": func() []map[string]any {
			st := withHarvest(map[string]any{"kind": "none"})
			st[3]["window"] = map[string]any{"kind": "date_range", "anchor": "nth", "from": -5, "to": -1}
			return st
		}()}},
	}
	for i, tc := range cases {
		extra := tc.extra
		extra["code"] = "SOP-BLK" + string(rune('A'+i))
		tmpl, ver := f.createDraft("SOP bị chặn "+string(rune('A'+i)), extra)
		expectField(t, f.sopAdmin.adminPost(versionsPath+ver+"/publish", nil), u, tc.code, tc.field, tc.name)
		if str(f.sopAdmin.adminGet(sopsPath+"/"+tmpl).body, "status") != "draft" {
			t.Fatalf("%s: SOP phải còn ở Nháp", tc.name)
		}
	}
	// Sửa theo thông báo thì phát hành được: Thu hoạch cuối theo giai đoạn.
	_, ver := f.createDraft("SOP thu hoạch theo giai đoạn", map[string]any{"steps": withHarvest(map[string]any{"kind": "stage"})})
	expect(t, f.sopAdmin.adminPost(versionsPath+ver+"/publish", nil), http.StatusOK, "thu hoạch theo giai đoạn")
}

// AH-8 AC6: Thành viên HTX chỉ xem SOP đã phát hành; chỉ vai Admin soạn và phát hành.
func TestSOPRoles_AH8_AC6(t *testing.T) {
	f := newFixture(t)
	tmpl, ver := f.createDraft("SOP quyền nháp", nil)
	pubTmpl, _ := f.publishedSOP("SOP quyền đã phát hành", "")
	writes := map[string]any{
		sopsPath:                            sopBody("SOP do TV tạo", nil),
		sopsPath + "/" + tmpl + "/info":     map[string]any{"name": "Đổi", "crop": "LUA", "anchor_name": "Ngày sạ"},
		sopsPath + "/" + tmpl + "/versions": nil,
		sopsPath + "/" + tmpl + "/clone":    map[string]any{"name": "Bản sao TV"},
		sopsPath + "/" + tmpl + "/retire":   map[string]any{"reason": "Lý do đủ dài để ngừng"},
		versionsPath + ver + "/steps":       map[string]any{"stages": sopStages(), "steps": sopSteps()},
		versionsPath + ver + "/publish":     nil,
		"/v1/cropcycle/variety-bindings":    map[string]any{"variety_code": "OM5451", "template_ref": pubTmpl},
	}
	for path, body := range writes {
		expectCode(t, f.managerA().adminPost(path, body), http.StatusForbidden, "forbidden", "coop_manager ghi "+path)
		expectCode(t, f.officerA().adminPost(path, body), http.StatusForbidden, "forbidden", "kn_officer ghi "+path)
	}
	expectCode(t, f.approver.adminPost(versionsPath+ver+"/publish", nil), http.StatusForbidden, "forbidden", "param_approver phát hành")
	expectCode(t, f.approver.adminPost(sopsPath, sopBody("SOP do approver tạo", nil)), http.StatusForbidden, "forbidden", "param_approver soạn")
	// Admin (platform_admin) vẫn soạn và phát hành được.
	r := f.platformAdmin().adminPost(sopsPath, sopBody("SOP do platform_admin", nil))
	expect(t, r, http.StatusCreated, "platform_admin soạn")
	expect(t, f.platformAdmin().adminPost(versionsPath+str(list(r.body, "versions")[0], "id")+"/publish", nil), http.StatusOK, "platform_admin phát hành")
	// App API: chỉ thấy mẫu đã phát hành.
	items := list(f.managerA().get(sopsPath).body, "items")
	seen := map[string]bool{}
	for _, it := range items {
		seen[str(it, "ref")] = true
		if str(it, "status") != "published" {
			t.Fatalf("App API chỉ được thấy SOP đã phát hành: %v", it)
		}
	}
	if seen[tmpl] || !seen[pubTmpl] {
		t.Fatalf("App API phải thấy SOP đã phát hành, không thấy nháp: %v", seen)
	}
	if f.approver.adminGet(sopsPath).status != http.StatusOK {
		t.Fatalf("%s xem danh sách quản trị được", authz.RoleParamApprover)
	}
}
