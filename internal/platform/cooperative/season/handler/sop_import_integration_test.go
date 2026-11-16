package handler_test

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"

	port "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/port"
	mdport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/masterdata/port"
)

const (
	sopImportsPath  = "/v1/cropcycle/sop-imports"
	sopTemplatePath = "/v1/cropcycle/sop-imports/template.xlsx"
	xlsxType        = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
)

// seedImportCatalogs thêm vật tư có tên tiếng Việt (tên danh mục ≠ mã) cho các test nhập SOP.
func (f *fixture) seedImportCatalogs() {
	f.catalogs.put(mdport.CatalogMaterial, "URE2", "Phân Urê", map[string]string{"unit": "kg"}, true)
	f.catalogs.put(mdport.CatalogMaterial, "TR", "Thuốc trừ rầy", map[string]string{"unit": "lít"}, true)
	f.catalogs.put(mdport.CatalogMaterial, "DAP2", "Phân DAP test", map[string]string{"unit": "kg"}, true)
}

// importTemplate tải tệp mẫu bằng c.
func (f *fixture) importTemplate(c caller) []byte {
	f.t.Helper()
	r := c.adminGet(sopTemplatePath)
	expect(f.t, r, http.StatusOK, "tải tệp mẫu")
	return r.raw
}

// sopWorkbook mở tệp mẫu đã tải, cho fill điền dữ liệu rồi xuất lại thành byte .xlsx.
func sopWorkbook(t *testing.T, tmpl []byte, fill func(*excelize.File)) []byte {
	t.Helper()
	x, err := excelize.OpenReader(bytes.NewReader(tmpl))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = x.Close() }()
	fill(x)
	var buf bytes.Buffer
	if _, err := x.WriteTo(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// setRow ghi các ô của một dòng bắt đầu từ cột A; nil và "" để trống.
func setRow(x *excelize.File, sheet string, row int, vals ...any) {
	for i, v := range vals {
		if v == nil || v == "" {
			continue
		}
		cell, _ := excelize.CoordinatesToCellName(i+1, row)
		if err := x.SetCellValue(sheet, cell, v); err != nil {
			panic(err)
		}
	}
}

// minimalSOP điền một SOP hợp lệ nhỏ nhất: một giai đoạn, một việc Thu hoạch.
func minimalSOP(name string) func(*excelize.File) {
	return func(x *excelize.File) {
		setRow(x, "Thông tin chung", 2, "LUA", "OM5451", name, "Ngày xuống giống")
		setRow(x, "Giai đoạn", 2, 1, "Giai đoạn test", 0, 120)
		setRow(x, "Việc", 2, 1, 1, "Thu hoạch lúa", "Thu hoạch", "Theo ngày", "NXG", 100, 110)
	}
}

func (f *fixture) importSOP(c caller, body []byte, idemKey string) resp {
	f.t.Helper()
	return c.adminPostRaw(sopImportsPath, xlsxType, body, idemKey)
}

// rowErrs trả "sheet|dòng|cột" của từng lỗi trong cropcycle.sop_import_rows, theo thứ tự trả về.
func rowErrs(r resp) []string {
	var out []string
	for _, e := range list(r.body, "rows") {
		out = append(out, fmt.Sprintf("%s|%v|%s", str(e, "sheet"), get(e, "row"), str(e, "column")))
	}
	return out
}

func expectRows(t *testing.T, r resp, want []string, what string) {
	t.Helper()
	expectCode(t, r, http.StatusUnprocessableEntity, "cropcycle.sop_import_rows", what)
	if got := rowErrs(r); !slices.Equal(got, want) {
		t.Fatalf("%s: danh sách lỗi\n có  %q\n muốn %q\n%s", what, got, want, r.raw)
	}
}

func sopCount(f *fixture) int { return len(list(f.sopAdmin.adminGet(sopsPath).body, "items")) }

// [AH-38 FN-01.4/AC-04] AC1 — nhập đúng. Kiêm vòng "tải mẫu → điền → nhập lại thành công".
// Dữ liệu là DỮ LIỆU TEST tự dựng (7 giai đoạn, 34 việc), không phải quy trình chuẩn của khách.
func TestSOPImport_AH38_AC1(t *testing.T) {
	f := newFixture(t)
	f.seedImportCatalogs()
	const name = "Dữ liệu test AH-38 — SOP lúa 7 giai đoạn"
	stageFrom := []int{-10, 1, 11, 31, 61, 81, 96}
	tasks := []int{5, 5, 5, 5, 5, 5, 4}
	acts := []string{"Làm đất", "Gieo trồng", "Quản lý nước", "Bón phân", "Phun thuốc - dưỡng", "Theo dõi", "Cắt tỉa", "Khác"}

	wb := sopWorkbook(t, f.importTemplate(f.sopAdmin), func(x *excelize.File) {
		setRow(x, "Thông tin chung", 2, "LUA", "OM5451", name, "Ngày xuống giống")
		line, idx := 2, 0
		for s := 1; s <= 7; s++ {
			setRow(x, "Giai đoạn", s+1, s, fmt.Sprintf("Giai đoạn test %d", s), stageFrom[s-1], stageFrom[s-1]+9)
			for k := 1; k <= tasks[s-1]; k++ {
				act := acts[idx%len(acts)]
				kind, anchor, from, to := "Không ngày", "", any(nil), any(nil)
				switch k {
				case 1:
					kind = "Theo giai đoạn"
				case 2:
					kind, anchor, from, to = "Theo ngày", "NXG", stageFrom[s-1], stageFrom[s-1]+3
				case 3:
					kind, anchor, from = "Mở", "NXG", stageFrom[s-1]
				case 5:
					kind, anchor, from, to = "Theo ngày", "NTH", -7, -3
				}
				if s == 7 {
					switch k {
					case 1:
						act = "Thu hoạch"
					case 3:
						kind, anchor, from, to = "Mở", "NTT", 0, nil
					}
				}
				setRow(x, "Việc", line, s, k, fmt.Sprintf("Việc test %d.%d", s, k), act, kind, anchor, from, to)
				line++
				idx++
			}
		}
		// Việc 3.1 (thứ 11) có 3 vật tư: Đơn vị trống lấy của danh mục, "bao" giữ nguyên; liều nhận dấu phẩy.
		setRow(x, "Vật tư", 2, 3, 1, "Phân Urê", 12.5, "")
		setRow(x, "Vật tư", 3, 3, 1, "Thuốc trừ rầy", "0,75", "bao")
		setRow(x, "Vật tư", 4, 3, 1, " phân  dap TEST ", 3, "lít")
		setRow(x, "Vật tư", 5, 4, 2, "Phân Urê", 20, "")
	})

	key := key()
	r := f.importSOP(f.sopAdmin, wb, key)
	expect(t, r, http.StatusCreated, "nhập SOP")
	if str(r.body, "status") != "draft" || str(r.body, "name") != name || str(r.body, "crop") != "LUA" || str(r.body, "variety_code") != "OM5451" || str(r.body, "scope") != "system" {
		t.Fatalf("SOP nhập: %s", r.raw)
	}
	v := list(r.body, "versions")[0]
	steps, stages := list(v, "steps"), list(v, "stages")
	if str(v, "status") != "draft" || len(stages) != 7 || len(steps) != 34 || str(v, "change_note") != "Nhập từ Excel" {
		t.Fatalf("phiên bản: %d giai đoạn, %d việc: %s", len(stages), len(steps), r.raw)
	}
	win := func(i int) string {
		w := get(steps[i], "window")
		return fmt.Sprintf("%s|%s|%v|%v", str(w, "kind"), str(w, "anchor"), get(w, "from"), get(w, "to"))
	}
	// from/to vắng mặt trong JSON → <nil>
	for i, want := range map[int]string{0: "stage||<nil>|<nil>", 1: "date_range|nxg|-10|-7", 2: "open|nxg|-10|<nil>", 3: "none||<nil>|<nil>",
		4: "date_range|nth|-7|-3", 32: "open|ntt|0|<nil>"} {
		if got := win(i); got != want {
			t.Errorf("cửa sổ việc %d: có %q, muốn %q", i+1, got, want)
		}
	}
	if str(steps[0], "name") != "Việc test 1.1" || str(steps[33], "name") != "Việc test 7.4" {
		t.Errorf("việc đầu/cuối: %v / %v", steps[0], steps[33])
	}
	if got := str(steps[30], "activity_code"); got != "thu_hoach" {
		t.Errorf("việc 7.1 phải là thu_hoach, có %q", got)
	}
	mats := list(steps[10], "materials")
	if len(mats) != 3 {
		t.Fatalf("việc 3.1 phải có 3 vật tư: %v", steps[10])
	}
	for i, want := range []struct{ code, dose, unit string }{{"URE2", "12.5", "kg"}, {"TR", "0.75", "bao"}, {"DAP2", "3", "lít"}} {
		m := mats[i]
		if str(m, "material_code") != want.code || fmt.Sprint(get(m, "dose_per_ha")) != want.dose || str(m, "unit") != want.unit {
			t.Errorf("vật tư %d: %v, muốn %+v", i+1, m, want)
		}
	}

	// Gọi lặp cùng Idempotency-Key trả đúng phản hồi cũ; chỉ có một SOP.
	r2 := f.importSOP(f.sopAdmin, wb, key)
	expect(t, r2, http.StatusCreated, "nhập lặp cùng khóa")
	if str(r2.body, "ref") != str(r.body, "ref") || sopCount(f) != 1 {
		t.Fatalf("nhập lặp: %s; %d SOP", r2.raw, sopCount(f))
	}

	// Phát hành bản nháp vừa nhập như SOP soạn tay (AH-8).
	expect(t, f.sopAdmin.adminPost(versionsPath+str(v, "id")+"/publish", nil), http.StatusOK, "phát hành SOP đã nhập")
}

// [AH-38 FN-01.4/AC-04] AC2 — tệp lỗi: không tạo SOP, trả đủ mọi lỗi theo sheet/dòng/cột.
func TestSOPImport_AH38_AC2(t *testing.T) {
	f := newFixture(t)
	f.seedImportCatalogs()
	tmpl := f.importTemplate(f.sopAdmin)

	wb := sopWorkbook(t, tmpl, func(x *excelize.File) {
		setRow(x, "Thông tin chung", 2, "Cây lạ", "", "Test lỗi", "Ngày xuống giống")
		setRow(x, "Giai đoạn", 2, 1, "", 0, 10)
		setRow(x, "Giai đoạn", 3, 2, "Giai đoạn B", 1, 20)
		setRow(x, "Giai đoạn", 4, 4, "Giai đoạn C", 21, 30)
		setRow(x, "Việc", 2, 1, 1, "Cày", "Làm đất", "Theo ngày", "NXG", 10, 5)
		setRow(x, "Việc", 3, 1, 2, "Sạ", "Gieo trồng", "Hàng tuần", "NXG", 1, 3)
		setRow(x, "Việc", 4, 2, 1, "Tưới", "Quản lý nước", "Mở", "", 5, "")
		setRow(x, "Việc", 5, 2, 3, "Bón", "Bón phân", "Không ngày", "", "", "")
		setRow(x, "Việc", 6, 3, 1, "", "Khác", "Không ngày", "", "", "")
		setRow(x, "Vật tư", 2, 1, 1, "Phân lạ", 2, "")
		setRow(x, "Vật tư", 3, 1, 1, "Phân Urê", "abc", "")
		setRow(x, "Vật tư", 4, 9, 9, "Phân Urê", 1, "")
	})
	r := f.importSOP(f.sopAdmin, wb, key())
	expectRows(t, r, []string{
		"Thông tin chung|2|Loại cây",
		"Giai đoạn|2|Tên giai đoạn",
		"Giai đoạn|4|Thứ tự",
		"Việc|2|Đến",
		"Việc|3|Loại cửa sổ",
		"Việc|4|Mốc",
		"Việc|5|Thứ tự",
		"Việc|6|Nội dung việc",
		"Vật tư|2|Vật tư",
		"Vật tư|3|Liều/ha",
		"Vật tư|4|Thứ tự việc",
	}, "tệp nhiều lỗi")
	if !strings.Contains(string(r.raw), "Hàng tuần") {
		t.Errorf("lý do của loại cửa sổ sai phải nêu giá trị đã điền: %s", r.raw)
	}
	if sopCount(f) != 0 || len(f.outbox(port.EventSOPPublished)) != 0 {
		t.Fatalf("tệp lỗi không được tạo SOP")
	}

	t.Run("trường hợp biên", func(t *testing.T) {
		f.catalogs.put(mdport.CatalogMaterial, "UREB", "phân  urê", map[string]string{"unit": "kg"}, true) // trùng tên "Phân Urê" sau chuẩn hóa
		cases := []struct {
			name string
			fill func(*excelize.File)
			want []string
		}{
			{"giống thuộc cây khác", func(x *excelize.File) {
				minimalSOP("Giống sai cây")(x)
				setRow(x, "Thông tin chung", 2, "LUA", "ARA", "Giống sai cây", "Ngày xuống giống")
			}, []string{"Thông tin chung|2|Giống"}},
			{"chọn mục con thay cho loại cây", func(x *excelize.File) {
				minimalSOP("Cây con")(x)
				setRow(x, "Thông tin chung", 2, "CON", "", "Cây con", "Ngày xuống giống")
			}, []string{"Thông tin chung|2|Loại cây"}},
			{"vật tư trùng tên trong danh mục", func(x *excelize.File) {
				minimalSOP("Vật tư trùng")(x)
				setRow(x, "Vật tư", 2, 1, 1, "Phân Urê", 2, "")
			}, []string{"Vật tư|2|Vật tư"}},
			{"vật tư ngừng dùng", func(x *excelize.File) {
				minimalSOP("Vật tư ngừng")(x)
				setRow(x, "Vật tư", 2, 1, 1, "OLD", 2, "")
			}, []string{"Vật tư|2|Vật tư"}},
			{"thiếu tên SOP và mốc gốc", func(x *excelize.File) {
				minimalSOP("x")(x)
				_ = x.SetCellValue("Thông tin chung", "C2", "")
				_ = x.SetCellValue("Thông tin chung", "D2", "")
			}, []string{"Thông tin chung|2|Tên SOP", "Thông tin chung|2|Tên mốc gốc"}},
			{"số thập phân ở ô nguyên", func(x *excelize.File) {
				minimalSOP("Số lẻ")(x)
				setRow(x, "Việc", 2, 1, 1, "Thu hoạch lúa", "Thu hoạch", "Theo ngày", "NXG", 100.5, 110)
			}, []string{"Việc|2|Từ"}},
			{"mốc sai chữ", func(x *excelize.File) {
				minimalSOP("Mốc sai")(x)
				setRow(x, "Việc", 2, 1, 1, "Thu hoạch lúa", "Thu hoạch", "Theo ngày", "Mốc lạ", 100, 110)
			}, []string{"Việc|2|Mốc"}},
			{"loại việc sai chữ", func(x *excelize.File) {
				minimalSOP("Việc sai")(x)
				setRow(x, "Việc", 2, 1, 1, "Thu hoạch lúa", "Thu hái", "Theo ngày", "NXG", 100, 110)
			}, []string{"Việc|2|Loại việc"}},
			{"Đến cho cửa sổ Mở", func(x *excelize.File) {
				minimalSOP("Mở có Đến")(x)
				setRow(x, "Việc", 2, 1, 1, "Thu hoạch lúa", "Thu hoạch", "Mở", "NXG", 100, 110)
			}, []string{"Việc|2|Đến"}},
		}
		for _, tc := range cases {
			expectRows(t, f.importSOP(f.sopAdmin, sopWorkbook(t, tmpl, tc.fill), key()), tc.want, tc.name)
		}
		if sopCount(f) != 0 {
			t.Fatal("các tệp lỗi không được tạo SOP")
		}
	})

	t.Run("tên SOP trùng", func(t *testing.T) {
		ok := sopWorkbook(t, tmpl, minimalSOP("SOP nhập trùng tên"))
		expect(t, f.importSOP(f.sopAdmin, ok, key()), http.StatusCreated, "nhập lần đầu")
		expectRows(t, f.importSOP(f.sopAdmin, ok, key()), []string{"Thông tin chung|2|Tên SOP"}, "nhập lại cùng tên")
		if sopCount(f) != 1 {
			t.Fatalf("chỉ một SOP: %d", sopCount(f))
		}
	})

	t.Run("tệp sai định dạng", func(t *testing.T) {
		many := func(x *excelize.File) {
			minimalSOP("Quá nhiều việc")(x)
			for i := 0; i < 101; i++ {
				setRow(x, "Việc", 2+i, 1, i+1, fmt.Sprintf("Việc %d", i), "Khác", "Không ngày")
			}
		}
		cases := []struct {
			name   string
			body   []byte
			detail string
		}{
			{"CSV", []byte("a,b\n1,2\n"), ".xlsx"},
			{"xls cũ", []byte{0xD0, 0xCF, 0x11, 0xE0, 0, 0, 0, 0}, ".xls"},
			{"byte rác", []byte("không phải tệp"), ".xlsx"},
			{"thiếu sheet Việc", sopWorkbook(t, tmpl, func(x *excelize.File) { _ = x.DeleteSheet("Việc") }), `thiếu sheet "Việc"`},
			{"thiếu cột Loại cửa sổ", sopWorkbook(t, tmpl, func(x *excelize.File) { _ = x.SetCellValue("Việc", "E1", "Cột khác") }), "thiếu cột: Loại cửa sổ"},
			{"ô có công thức", sopWorkbook(t, tmpl, func(x *excelize.File) {
				minimalSOP("Có công thức")(x)
				_ = x.SetCellFormula("Việc", "G2", "1+1")
			}), `Sheet "Việc": Ô G2 chứa công thức`},
			{"sheet Việc rỗng", sopWorkbook(t, tmpl, func(x *excelize.File) {
				setRow(x, "Thông tin chung", 2, "LUA", "", "Không việc", "Ngày xuống giống")
				setRow(x, "Giai đoạn", 2, 1, "Giai đoạn test", 0, 10)
			}), `Sheet "Việc" chưa có dòng dữ liệu`},
			{"Thông tin chung hai dòng", sopWorkbook(t, tmpl, func(x *excelize.File) {
				minimalSOP("Hai dòng")(x)
				setRow(x, "Thông tin chung", 3, "LUA", "", "SOP khác", "Ngày xuống giống")
			}), "đúng một dòng"},
			{"quá 100 việc", sopWorkbook(t, tmpl, many), "Tối đa 100 việc"},
		}
		for _, tc := range cases {
			r := f.importSOP(f.sopAdmin, tc.body, key())
			expectCode(t, r, http.StatusUnprocessableEntity, "cropcycle.sop_import_invalid", tc.name)
			if !strings.Contains(str(r.body, "detail"), tc.detail) || len(list(r.body, "rows")) != 0 {
				t.Errorf("%s: detail %q, muốn chứa %q", tc.name, str(r.body, "detail"), tc.detail)
			}
			if tc.name == "xls cũ" && strings.Contains(str(r.body, "detail"), "CSV") {
				t.Errorf("xls cũ: detail không được gợi ý CSV: %q", str(r.body, "detail"))
			}
		}
		expectCode(t, f.importSOP(f.sopAdmin, sopWorkbook(t, tmpl, minimalSOP("Thiếu khóa")), ""),
			http.StatusBadRequest, "idempotency_key_required", "thiếu Idempotency-Key")
	})
}

// [AH-38 FN-01.4/AC-04] AC3 — tải tệp mẫu có cột loại cửa sổ, Từ, Đến, danh sách chọn và hướng dẫn.
func TestSOPImportTemplate_AH38_AC3(t *testing.T) {
	f := newFixture(t)
	f.seedImportCatalogs()

	req, _ := http.NewRequest(http.MethodGet, f.admin.URL+sopTemplatePath, nil)
	req.Header.Set("Authorization", "Bearer "+f.sopAdmin.token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != xlsxType || !strings.Contains(res.Header.Get("Content-Disposition"), "mau-nhap-sop.xlsx") {
		t.Fatalf("phản hồi: %d %v", res.StatusCode, res.Header)
	}

	x, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = x.Close() }()
	want := []string{"Thông tin chung", "Giai đoạn", "Việc", "Vật tư", "Danh sách", "Hướng dẫn"}
	if got := x.GetSheetList(); !slices.Equal(got, want) {
		t.Fatalf("sheet: %v", got)
	}
	header, _ := x.GetRows("Việc")
	for _, col := range []string{"Loại cửa sổ", "Từ", "Đến", "Mốc", "Loại việc"} {
		if len(header) == 0 || !slices.Contains(header[0], col) {
			t.Errorf("sheet Việc thiếu cột %q: %v", col, header)
		}
	}
	dvs, err := x.GetDataValidations("Việc")
	if err != nil || len(dvs) != 3 {
		t.Fatalf("danh sách chọn của sheet Việc: %d %v", len(dvs), err)
	}
	if !slices.ContainsFunc(dvs, func(dv *excelize.DataValidation) bool {
		return dv.Sqref == "E2:E1001" && dv.Formula1 == "'Danh sách'!$A$2:$A$5"
	}) {
		t.Errorf("cột Loại cửa sổ phải chọn từ 4 nhãn ở sheet Danh sách: %+v", dvs)
	}
	lists, _ := x.GetRows("Danh sách")
	var all []string
	for _, row := range lists {
		all = append(all, row...)
	}
	for _, v := range []string{"Theo ngày", "Mở", "Theo giai đoạn", "Không ngày", "NXG", "NTH", "NTT", "Bón phân", "LUA", "OM5451", "Phân Urê", "Thuốc trừ rầy"} {
		if !slices.Contains(all, v) {
			t.Errorf("sheet Danh sách thiếu %q", v)
		}
	}
	if slices.Contains(all, "OLD") || slices.Contains(all, "CON") {
		t.Errorf("sheet Danh sách không được có mục ngừng dùng hoặc mục con: %v", all)
	}
	guide, _ := x.GetRows("Hướng dẫn")
	var text strings.Builder
	for _, row := range guide {
		text.WriteString(strings.Join(row, " "))
	}
	if !strings.Contains(text.String(), "Đơn vị để trống thì lấy đơn vị của vật tư trong danh mục") {
		t.Errorf("sheet Hướng dẫn thiếu câu về Đơn vị: %s", text.String())
	}
}

// [AH-38 FN-01.4/AC-04] AC4 — chỉ vai soạn SOP nhập và tải mẫu được.
func TestSOPImportRoles_AH38_AC4(t *testing.T) {
	f := newFixture(t)
	f.seedImportCatalogs()
	tmpl := f.importTemplate(f.sopAdmin)
	wb := sopWorkbook(t, tmpl, minimalSOP("SOP của người không có quyền"))
	for name, c := range map[string]caller{"param_approver": f.approver, "coop_manager": f.managerA()} {
		expectCode(t, f.importSOP(c, wb, key()), http.StatusForbidden, "forbidden", name+" nhập SOP")
		expectCode(t, c.adminGet(sopTemplatePath), http.StatusForbidden, "forbidden", name+" tải mẫu")
	}
	if sopCount(f) != 0 {
		t.Fatal("vai không có quyền không được tạo SOP")
	}
	pa := f.platformAdmin()
	expect(t, pa.adminGet(sopTemplatePath), http.StatusOK, "platform_admin tải mẫu")
	expect(t, f.importSOP(pa, sopWorkbook(t, tmpl, minimalSOP("SOP do platform_admin nhập")), key()), http.StatusCreated, "platform_admin nhập SOP")
}
