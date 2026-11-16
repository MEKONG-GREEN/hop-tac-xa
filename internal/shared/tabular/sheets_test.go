package tabular

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

// [AH-38 FN-01.4/AC-04] đọc nhiều trang tính theo tên.
func TestReadXLSXSheets(t *testing.T) {
	build := func(formula bool) []byte {
		f := excelize.NewFile()
		_ = f.SetSheetName("Sheet1", "Một")
		_, _ = f.NewSheet(" Hai ")
		_ = f.SetSheetRow("Một", "A1", &[]any{"Mã", "Tên"})
		_ = f.SetSheetRow("Một", "A2", &[]any{"a", "A"})
		_ = f.SetSheetRow(" Hai ", "A1", &[]any{"Số"})
		_ = f.SetCellValue(" Hai ", "A3", 7)
		if formula {
			_ = f.SetCellFormula(" Hai ", "A4", "A3*2")
		}
		var buf bytes.Buffer
		if _, err := f.WriteTo(&buf); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}
	got, err := ReadXLSXSheets(build(false), Limits{}, "Một", "Hai")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got["Một"].Keys, []string{"ma", "ten"}) || got["Hai"].Rows[0].Line != 3 || got["Hai"].Rows[0].Cells[0] != "7" {
		t.Fatalf("nội dung: %+v %+v", got["Một"], got["Hai"])
	}

	_, err = ReadXLSXSheets(build(false), Limits{}, "Một", "Ba")
	var ms *MissingSheetError
	if !errors.As(err, &ms) || ms.Name != "Ba" || !strings.Contains(Message(err, Limits{}), `"Ba"`) {
		t.Fatalf("thiếu sheet: %v", err)
	}
	_, err = ReadXLSXSheets(build(true), Limits{}, "Một", "Hai")
	var fe *FormulaError
	if !errors.As(err, &fe) || fe.Cell != "A4" {
		t.Fatalf("công thức ở sheet thứ hai: %v", err)
	}
	if _, err := ReadXLSXSheets([]byte("a,b\n1,2\n"), Limits{}, "Một"); !errors.Is(err, ErrFormat) {
		t.Fatalf("CSV: %v", err)
	}
	if _, err := ReadXLSXSheets([]byte{0xD0, 0xCF, 0x11, 0xE0, 0, 0}, Limits{}, "Một"); !errors.Is(err, ErrLegacyExcel) {
		t.Fatalf(".xls: %v", err)
	}
	f := excelize.NewFile()
	_ = f.SetSheetRow("Sheet1", "A1", &[]any{"x"})
	_, _ = f.NewSheet("Rỗng")
	var buf bytes.Buffer
	if _, err := f.WriteTo(&buf); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadXLSXSheets(buf.Bytes(), Limits{}, "Sheet1", "Rỗng"); !errors.Is(err, ErrEmpty) {
		t.Fatalf("sheet rỗng: %v", err)
	}
}

// [AH-38 FN-01.4/AC-04] danh sách chọn và cố định dòng tiêu đề.
func TestWriteSheetsXLSX_DropList(t *testing.T) {
	sheets := []Sheet{
		{Name: "Việc", Rows: [][]Cell{{{Text: "Loại", Bold: true}}}, FreezeHeader: true,
			Lists: []DropList{{Col: 1, FirstRow: 2, LastRow: 50, Source: "'Danh sách'!$A$2:$A$3"}}},
		{Name: "Danh sách", Rows: [][]Cell{{{Text: "Loại"}}, {{Text: "Mở"}}, {{Text: "Không ngày"}}}},
	}
	var buf bytes.Buffer
	if err := WriteSheetsXLSX(&buf, sheets); err != nil {
		t.Fatal(err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	dvs, err := f.GetDataValidations("Việc")
	if err != nil || len(dvs) != 1 || dvs[0].Sqref != "A2:A50" || dvs[0].Formula1 != "'Danh sách'!$A$2:$A$3" {
		t.Fatalf("data validation: %+v %v", dvs, err)
	}
	if p, err := f.GetPanes("Việc"); err != nil || !p.Freeze || p.YSplit != 1 {
		t.Fatalf("pane: %+v %v", p, err)
	}
	if p, _ := f.GetPanes("Danh sách"); p.Freeze {
		t.Fatalf("sheet không yêu cầu không được cố định: %+v", p)
	}
}
