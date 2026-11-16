package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/domain"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/cooperative/season/store"
	mdport "github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/authz"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/problem"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tabular"
)

// MaxSOPImportBytes là dung lượng tối đa của tệp nhập SOP (như nhập danh mục và nhập sản lượng).
const MaxSOPImportBytes = 2 << 20

var sopImportLimits = tabular.Limits{MaxBytes: MaxSOPImportBytes, MaxRows: domain.MaxSteps * domain.MaxMaterials}

// templateListRows là số dòng dữ liệu của mỗi sheet mà tệp mẫu gắn danh sách chọn.
const templateListRows = 1000

// Tên sheet và cột của tệp nhập SOP. Một đặc tả duy nhất (sopImportSheets) dùng cho cả tệp mẫu và bộ đọc,
// nên cột của hai bên luôn khớp nhau.
const (
	sheetInfo   = "Thông tin chung"
	sheetStages = "Giai đoạn"
	sheetSteps  = "Việc"
	sheetMats   = "Vật tư"
	sheetLists  = "Danh sách"
	sheetGuide  = "Hướng dẫn"

	colCrop      = "Loại cây"
	colVariety   = "Giống"
	colSOPName   = "Tên SOP"
	colAnchorNm  = "Tên mốc gốc"
	colSeq       = "Thứ tự"
	colStageName = "Tên giai đoạn"
	colFrom      = "Từ"
	colTo        = "Đến"
	colStage     = "Giai đoạn"
	colStepName  = "Nội dung việc"
	colActivity  = "Loại việc"
	colWindow    = "Loại cửa sổ"
	colAnchor    = "Mốc"
	colStepSeq   = "Thứ tự việc"
	colMaterial  = "Vật tư"
	colDose      = "Liều/ha"
	colUnit      = "Đơn vị"
)

// importCol là một cột của sheet. List là tên cột của sheet "Danh sách" cung cấp giá trị chọn ("" = nhập tự do).
type importCol struct {
	Label string
	Key   string // tabular.NormalizeHeader(Label)
	List  string
}

type importSheet struct {
	Name string
	Cols []importCol
}

func icol(label, list string) importCol {
	return importCol{Label: label, Key: tabular.NormalizeHeader(label), List: list}
}

var sopImportSheets = []importSheet{
	{sheetInfo, []importCol{icol(colCrop, colCrop), icol(colVariety, colVariety), icol(colSOPName, ""), icol(colAnchorNm, "")}},
	{sheetStages, []importCol{icol(colSeq, ""), icol(colStageName, ""), icol(colFrom, ""), icol(colTo, "")}},
	{sheetSteps, []importCol{icol(colStage, ""), icol(colSeq, ""), icol(colStepName, ""), icol(colActivity, colActivity),
		icol(colWindow, colWindow), icol(colAnchor, colAnchor), icol(colFrom, ""), icol(colTo, "")}},
	{sheetMats, []importCol{icol(colStage, ""), icol(colStepSeq, ""), icol(colMaterial, colMaterial), icol(colDose, ""), icol(colUnit, "")}},
}

// sopLists là thứ tự các cột của sheet "Danh sách".
var sopLists = []string{colWindow, colAnchor, colActivity, colCrop, colVariety, colMaterial}

func importSheetOf(name string) importSheet {
	for _, sh := range sopImportSheets {
		if sh.Name == name {
			return sh
		}
	}
	return importSheet{}
}

type labelCode struct{ Label, Code string }

// Nhãn người dùng điền → mã của bộ luật soạn SOP.
var (
	windowKindLabels = []labelCode{{"Theo ngày", domain.WindowDateRange}, {"Mở", domain.WindowOpen},
		{"Theo giai đoạn", domain.WindowStage}, {"Không ngày", domain.WindowNone}}
	anchorLabels = []labelCode{{"NXG", domain.AnchorNXG}, {"NTH", domain.AnchorNTH}, {"NTT", domain.AnchorNTT}}
)

func codeOfLabel(list []labelCode, label string) (string, bool) {
	key := domain.NameKey(label)
	for _, lc := range list {
		if domain.NameKey(lc.Label) == key {
			return lc.Code, true
		}
	}
	return "", false
}

func labelsOf(list []labelCode) []string {
	out := make([]string, len(list))
	for i, lc := range list {
		out[i] = lc.Label
	}
	return out
}

// SOPImportError là một lỗi của tệp nhập: sheet, số dòng trong tệp, cột và lý do.
type SOPImportError struct {
	Sheet  string `json:"sheet"`
	Row    int    `json:"row"`
	Column string `json:"column"`
	Reason string `json:"reason"`
}

// ---------------------------------------------------------------------------
// Nhập
// ---------------------------------------------------------------------------

// ImportSOP tạo một SOP Nháp từ tệp Excel mẫu (AH-38, FN-01.4). Đọc và kiểm toàn bộ tệp
// (kể cả tra danh mục) rồi mới mở transaction ghi trong CreateSOP (AGENTS.md §9 #1). Có lỗi:
// không ghi gì, trả đủ danh sách lỗi theo sheet/dòng/cột.
func (s *Service) ImportSOP(ctx context.Context, src io.Reader, idemKey string) (SOPView, error) {
	p, err := authz.Require(ctx, sopAuthorRoles...)
	if err != nil {
		return SOPView{}, err
	}
	if idemKey == "" {
		return SOPView{}, problem.IdempotencyRequired
	}
	if s.Catalogs == nil {
		return SOPView{}, ErrDependency.WithDetail("không tra được danh mục")
	}
	tables, err := readSOPImport(src)
	if err != nil {
		return SOPView{}, err
	}
	if err := checkImportShape(tables); err != nil {
		return SOPView{}, err
	}
	ix, err := s.importCatalogs(ctx)
	if err != nil {
		return SOPView{}, err
	}
	im := newSOPImport(tables, ix)
	in := im.build()

	// Thông tin chung và bộ luật nội dung dùng chung với soạn SOP.
	im.addProblem(cleanTemplateInfo(&in.Name, &in.Crop, &in.VarietyCode, &in.AnchorName))
	if in.Crop != "" {
		if err := s.checkCatalogScope(ctx, in.Crop, in.VarietyCode); err != nil {
			if !im.addProblem(err) {
				return SOPView{}, err
			}
		}
	}
	if _, fes := domain.CheckContent(domain.Content{Stages: in.Stages, Steps: in.Steps}); len(fes) > 0 {
		for _, fe := range fes {
			im.addField(fe.Field, fe.Reason)
		}
	}
	if in.Name != "" {
		taken := false
		err := s.inTx(ctx, p.TenantID, func(tx pgx.Tx) error {
			_, nameTaken, err := store.TemplateTaken(ctx, tx, "", domain.NameKey(in.Name))
			taken = nameTaken
			return err
		})
		if err != nil {
			return SOPView{}, err
		}
		if taken {
			im.addField("name", "Tên SOP đã tồn tại, hãy chọn tên khác")
		}
	}
	if errs := im.result(); len(errs) > 0 {
		return SOPView{}, ErrSOPImportRows.WithDetail(strconv.Itoa(len(errs))+" lỗi — sửa file rồi nhập lại").With("rows", errs)
	}
	in.Scope, in.ChangeNote = domain.ScopeSystem, "Nhập từ Excel"
	return s.CreateSOP(ctx, in)
}

// readSOPImport đọc cả tệp (một lần) thành các bảng theo tên sheet; mọi lỗi định dạng → ErrSOPImportInvalid.
func readSOPImport(src io.Reader) (map[string]*tabular.Table, error) {
	lim := sopImportLimits
	raw, err := io.ReadAll(io.LimitReader(src, lim.MaxBytes+1))
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return nil, ErrSOPImportInvalid.WithDetail(tabular.Message(tabular.ErrTooLarge, lim))
		}
		return nil, err
	}
	names := make([]string, len(sopImportSheets))
	for i, sh := range sopImportSheets {
		names[i] = sh.Name
	}
	tables, err := tabular.ReadXLSXSheets(raw, lim, names...)
	if err != nil {
		detail := tabular.Message(err, lim)
		if errors.Is(err, tabular.ErrFormat) {
			detail = "Chỉ nhận tệp Excel .xlsx — hãy tải file mẫu và thử lại"
		}
		return nil, ErrSOPImportInvalid.WithDetail(detail)
	}
	return tables, nil
}

// checkImportShape từ chối tệp sai bố cục: thiếu cột, sai số dòng, vượt giới hạn. Không trả danh sách dòng.
func checkImportShape(tables map[string]*tabular.Table) error {
	var parts []string
	for _, sh := range sopImportSheets {
		col := tables[sh.Name].Columns(keyAliases(sh))
		var missing []string
		for _, c := range sh.Cols {
			if _, ok := col[c.Key]; !ok {
				missing = append(missing, c.Label)
			}
		}
		if len(missing) > 0 {
			parts = append(parts, `Sheet "`+sh.Name+`" thiếu cột: `+strings.Join(missing, ", "))
		}
	}
	if len(parts) > 0 {
		return ErrSOPImportInvalid.WithDetail(strings.Join(parts, "; "))
	}
	stages, steps := len(tables[sheetStages].Rows), len(tables[sheetSteps].Rows)
	switch n := len(tables[sheetInfo].Rows); {
	case n != 1:
		return ErrSOPImportInvalid.WithDetail(`Sheet "` + sheetInfo + `" phải có đúng một dòng dữ liệu (tệp có ` + strconv.Itoa(n) + ")")
	case stages == 0:
		return ErrSOPImportInvalid.WithDetail(`Sheet "` + sheetStages + `" chưa có dòng dữ liệu`)
	case steps == 0:
		return ErrSOPImportInvalid.WithDetail(`Sheet "` + sheetSteps + `" chưa có dòng dữ liệu`)
	case stages > domain.MaxStages:
		return ErrSOPImportInvalid.WithDetail("Tối đa " + strconv.Itoa(domain.MaxStages) + " giai đoạn mỗi SOP")
	case steps > domain.MaxSteps:
		return ErrSOPImportInvalid.WithDetail("Tối đa " + strconv.Itoa(domain.MaxSteps) + " việc mỗi SOP")
	}
	return nil
}

func keyAliases(sh importSheet) map[string]string {
	m := make(map[string]string, len(sh.Cols))
	for _, c := range sh.Cols {
		m[c.Key] = c.Key
	}
	return m
}

// catalogIndex tra mục danh mục đang dùng theo tên (domain.NameKey).
type catalogIndex map[string][]mdport.CatalogItem

type importIndexes struct{ crop, variety, material catalogIndex }

// importCatalogs tra mỗi danh mục một lần (ngoài transaction).
func (s *Service) importCatalogs(ctx context.Context) (importIndexes, error) {
	load := func(catalog string) (catalogIndex, error) {
		items, err := s.Catalogs.List(ctx, catalog)
		if err != nil {
			return nil, ErrDependency.WithDetail("không tra được danh mục").WithCause(err)
		}
		ix := catalogIndex{}
		for _, it := range items {
			if it.Active {
				k := domain.NameKey(it.Name)
				ix[k] = append(ix[k], it)
			}
		}
		return ix, nil
	}
	var out importIndexes
	var err error
	if out.crop, err = load(mdport.CatalogCrop); err != nil {
		return out, err
	}
	if out.variety, err = load(mdport.CatalogRiceVariety); err != nil {
		return out, err
	}
	out.material, err = load(mdport.CatalogMaterial)
	return out, err
}

const (
	pickFound = iota
	pickMissing
	pickAmbiguous
)

// pick chọn mục theo tên; trùng nhiều mục thì ưu tiên các mục thỏa prefer, vẫn nhiều thì báo trùng (không đoán).
func (ix catalogIndex) pick(name string, prefer func(mdport.CatalogItem) bool) (mdport.CatalogItem, int) {
	items := ix[domain.NameKey(name)]
	if len(items) > 1 && prefer != nil {
		if pref := slices.DeleteFunc(slices.Clone(items), func(it mdport.CatalogItem) bool { return !prefer(it) }); len(pref) > 0 {
			items = pref
		}
	}
	switch len(items) {
	case 0:
		return mdport.CatalogItem{}, pickMissing
	case 1:
		return items[0], pickFound
	}
	return mdport.CatalogItem{}, pickAmbiguous
}

// sopImport dựng CreateSOPInput từ các bảng đã đọc và ghi nhận lỗi theo vị trí trong tệp.
type sopImport struct {
	tables map[string]*tabular.Table
	cols   map[string]map[string]int
	ix     importIndexes
	errs   []SOPImportError

	infoLine   int
	stageLines []int
	stepLines  []int
	matLines   [][]int // theo việc, theo dòng vật tư đã đưa vào nội dung
}

func newSOPImport(tables map[string]*tabular.Table, ix importIndexes) *sopImport {
	im := &sopImport{tables: tables, ix: ix, cols: map[string]map[string]int{}}
	for _, sh := range sopImportSheets {
		im.cols[sh.Name] = tables[sh.Name].Columns(keyAliases(sh))
	}
	return im
}

// rowReader đọc ô theo nhãn cột của một dòng.
type rowReader struct {
	im    *sopImport
	sheet string
	row   tabular.Row
}

func (im *sopImport) reader(sheet string, row tabular.Row) rowReader {
	return rowReader{im: im, sheet: sheet, row: row}
}

func (r rowReader) text(label string) string {
	return r.row.Get(r.im.cols[r.sheet], tabular.NormalizeHeader(label))
}

func (r rowReader) fail(label, reason string) { r.im.fail(r.sheet, r.row.Line, label, reason) }

// whole đọc số nguyên; ô trống hoặc sai dạng ghi lỗi (required) và trả ok=false.
func (r rowReader) whole(label string, required bool) (int, bool) {
	cell := r.text(label)
	if cell == "" {
		if required {
			r.fail(label, "bắt buộc")
		}
		return 0, false
	}
	d, err := decimal.NewFromString(cell)
	if err != nil || !d.IsInteger() || d.Abs().GreaterThan(decimal.NewFromInt(1_000_000)) {
		r.fail(label, "phải là số nguyên")
		return 0, false
	}
	return int(d.IntPart()), true
}

func (r rowReader) optWhole(label string) *int {
	if n, ok := r.whole(label, false); ok {
		return &n
	}
	return nil
}

func (im *sopImport) fail(sheet string, row int, col, reason string) {
	im.errs = append(im.errs, SOPImportError{Sheet: sheet, Row: row, Column: col, Reason: reason})
}

// build đọc mọi dòng, giữ nguyên thứ tự trong tệp, và đổi chữ tiếng Việt thành mã.
func (im *sopImport) build() CreateSOPInput {
	var in CreateSOPInput
	info := im.tables[sheetInfo].Rows[0]
	im.infoLine = info.Line
	ir := im.reader(sheetInfo, info)
	in.Name, in.AnchorName = ir.text(colSOPName), ir.text(colAnchorNm)
	if name := ir.text(colCrop); name != "" {
		in.Crop = im.resolve(ir, colCrop, "loại cây", im.ix.crop, func(it mdport.CatalogItem) bool { return it.Attrs[mdport.AttrParentCode] == "" }).Code
	}
	if name := ir.text(colVariety); name != "" {
		in.VarietyCode = im.resolve(ir, colVariety, "giống", im.ix.variety, func(it mdport.CatalogItem) bool { return it.Attrs[mdport.AttrCropCode] == in.Crop }).Code
	}

	for _, row := range im.tables[sheetStages].Rows {
		r := im.reader(sheetStages, row)
		seq, _ := r.whole(colSeq, true)
		in.Stages = append(in.Stages, domain.Stage{Seq: seq, Name: r.text(colStageName), FromDay: r.optWhole(colFrom), ToDay: r.optWhole(colTo)})
		im.stageLines = append(im.stageLines, row.Line)
	}

	byPos := map[[2]int]int{}
	for j, row := range im.tables[sheetSteps].Rows {
		r := im.reader(sheetSteps, row)
		st := domain.Step{Seq: j + 1, Name: r.text(colStepName), Window: &domain.Window{}}
		st.StageSeq, _ = r.whole(colStage, true)
		st.TaskSeq, _ = r.whole(colSeq, true)
		st.ActivityCode = im.activity(r)
		w := st.Window
		w.Kind, w.Anchor = im.windowKind(r), im.anchor(r)
		w.From, w.To = r.optWhole(colFrom), r.optWhole(colTo)
		in.Steps = append(in.Steps, st)
		im.stepLines = append(im.stepLines, row.Line)
		if _, dup := byPos[[2]int{st.StageSeq, st.TaskSeq}]; !dup {
			byPos[[2]int{st.StageSeq, st.TaskSeq}] = j
		}
	}

	im.matLines = make([][]int, len(in.Steps))
	for _, row := range im.tables[sheetMats].Rows {
		r := im.reader(sheetMats, row)
		stage, ok1 := r.whole(colStage, true)
		task, ok2 := r.whole(colStepSeq, true)
		if !ok1 || !ok2 {
			continue
		}
		j, found := byPos[[2]int{stage, task}]
		if !found {
			r.fail(colStepSeq, "Không có việc giai đoạn "+strconv.Itoa(stage)+", thứ tự "+strconv.Itoa(task)+" trong sheet "+sheetSteps)
			continue
		}
		if m, ok := im.material(r); ok {
			in.Steps[j].Materials = append(in.Steps[j].Materials, m)
			im.matLines[j] = append(im.matLines[j], row.Line)
		}
	}
	return in
}

// resolve tra mục danh mục theo tên người dùng điền; không thấy hoặc trùng thì ghi lỗi của ô và trả mục rỗng.
func (im *sopImport) resolve(r rowReader, label, noun string, ix catalogIndex, prefer func(mdport.CatalogItem) bool) mdport.CatalogItem {
	name := r.text(label)
	it, st := ix.pick(name, prefer)
	switch st {
	case pickMissing:
		r.fail(label, "Không có "+noun+` "`+name+`" trong danh mục`)
	case pickAmbiguous:
		r.fail(label, "Tên "+noun+` "`+name+`" trùng nhiều mục trong danh mục — chưa chọn được`)
	}
	return it
}

func (im *sopImport) activity(r rowReader) string {
	cell := r.text(colActivity)
	if cell == "" {
		r.fail(colActivity, "bắt buộc: chọn một loại việc ("+strings.Join(domain.ActivityLabels(), ", ")+")")
		return ""
	}
	code, ok := domain.ActivityByLabel(cell)
	if !ok {
		r.fail(colActivity, `Loại việc "`+cell+`" không có trong danh mục: `+strings.Join(domain.ActivityLabels(), ", "))
	}
	return code
}

func (im *sopImport) windowKind(r rowReader) string {
	cell := r.text(colWindow)
	if cell == "" {
		r.fail(colWindow, msgWindowKindLabels())
		return ""
	}
	code, ok := codeOfLabel(windowKindLabels, cell)
	if !ok {
		r.fail(colWindow, `Loại cửa sổ "`+cell+`" không hợp lệ — chọn `+strings.Join(labelsOf(windowKindLabels), ", "))
	}
	return code
}

func (im *sopImport) anchor(r rowReader) string {
	cell := r.text(colAnchor)
	if cell == "" {
		return ""
	}
	code, ok := codeOfLabel(anchorLabels, cell)
	if !ok {
		r.fail(colAnchor, `Mốc "`+cell+`" không hợp lệ — chọn NXG, NTH hoặc NTT`)
	}
	return code
}

// material đọc một dòng vật tư; vật tư không có trong danh mục hoặc trùng tên thì ghi lỗi và bỏ dòng.
func (im *sopImport) material(r rowReader) (domain.Material, bool) {
	var m domain.Material
	cell := r.text(colDose)
	dose, err := decimal.NewFromString(strings.ReplaceAll(cell, ",", "."))
	switch {
	case cell == "":
		r.fail(colDose, "bắt buộc")
	case err != nil:
		r.fail(colDose, "Sai định dạng số liều")
	default:
		m.DosePerHa = dose
	}
	if r.text(colMaterial) == "" {
		r.fail(colMaterial, "bắt buộc: chọn vật tư trong danh mục")
		return m, false
	}
	it := im.resolve(r, colMaterial, "vật tư", im.ix.material, nil)
	if it.Code == "" {
		return m, false
	}
	m.MaterialCode, m.Name = it.Code, it.Name
	// Đơn vị để trống thì lấy của vật tư trong danh mục; có điền thì giữ nguyên chữ người dùng điền.
	if m.Unit = r.text(colUnit); m.Unit == "" {
		m.Unit = it.Attrs["unit"]
	}
	return m, true
}

func msgWindowKindLabels() string {
	return "bắt buộc: " + strings.Join(labelsOf(windowKindLabels), ", ")
}

// ---------------------------------------------------------------------------
// Đổi lỗi của bộ luật dùng chung thành vị trí trong tệp
// ---------------------------------------------------------------------------

var fieldPosRe = regexp.MustCompile(`^(stages|steps)\[(\d+)\](?:\.materials\[(\d+)\])?\.?(.*)$`)

// stageCols, stepCols, matCols, infoCols: trường của bộ luật → cột của tệp.
var (
	stageCols = map[string]string{"seq": colSeq, "name": colStageName, "from_day": colFrom, "to_day": colTo}
	stepCols  = map[string]string{"stage_seq": colStage, "task_seq": colSeq, "seq": colSeq, "name": colStepName,
		"activity_code": colActivity, "window.kind": colWindow, "window.anchor": colAnchor, "window.from": colFrom, "window.to": colTo}
	matCols  = map[string]string{"material_code": colMaterial, "name": colMaterial, "dose_per_ha": colDose, "unit": colUnit}
	infoCols = map[string]string{"name": colSOPName, "crop": colCrop, "variety_code": colVariety, "anchor_name": colAnchorNm}
)

// addField ghi lỗi của một trường trong bộ luật (vd steps[3].window.to) tại đúng sheet, dòng, cột.
func (im *sopImport) addField(field, reason string) {
	reason = importReason(reason)
	if m := fieldPosRe.FindStringSubmatch(field); m != nil {
		i, _ := strconv.Atoi(m[2])
		rest := m[4]
		switch {
		case m[1] == "stages" && i < len(im.stageLines):
			im.fail(sheetStages, im.stageLines[i], pickCol(stageCols, rest, colStageName), reason)
			return
		case m[1] == "steps" && i < len(im.stepLines) && m[3] != "":
			k, _ := strconv.Atoi(m[3])
			if k < len(im.matLines[i]) {
				im.fail(sheetMats, im.matLines[i][k], pickCol(matCols, rest, colMaterial), reason)
				return
			}
		case m[1] == "steps" && i < len(im.stepLines) && rest == "materials" && len(im.matLines[i]) > 0:
			im.fail(sheetMats, im.matLines[i][0], colMaterial, reason)
			return
		case m[1] == "steps" && i < len(im.stepLines):
			im.fail(sheetSteps, im.stepLines[i], pickCol(stepCols, rest, colStepName), reason)
			return
		}
	}
	im.fail(sheetInfo, im.infoLine, pickCol(infoCols, field, colSOPName), reason)
}

func pickCol(m map[string]string, field, def string) string {
	if c, ok := m[field]; ok {
		return c
	}
	return def
}

// importReason đổi lý do dùng mã (date_range, nxg…) của bộ luật sang cách gọi trên tệp mẫu.
func importReason(reason string) string {
	switch reason {
	case domain.MsgActivityChoice:
		return "bắt buộc: chọn một loại việc (" + strings.Join(domain.ActivityLabels(), ", ") + ")"
	case domain.MsgWindowKindNeeded, domain.MsgWindowKindChoice:
		return msgWindowKindLabels()
	case domain.MsgAnchorChoice:
		return "bắt buộc: NXG, NTH hoặc NTT"
	}
	return reason
}

// addProblem ghi các lỗi theo trường của một problem 422; false nếu err không phải lỗi dữ liệu (để trả nguyên).
func (im *sopImport) addProblem(err error) bool {
	if err == nil {
		return true
	}
	p := problem.From(err)
	if p.Status != http.StatusUnprocessableEntity || len(p.Errors) == 0 {
		return false
	}
	for _, fe := range p.Errors {
		im.addField(fe.Field, fe.Reason)
	}
	return true
}

// result bỏ lỗi trùng vị trí (giữ lỗi đọc ô vì nó đứng trước, cụ thể hơn) và sắp theo sheet → dòng → cột.
func (im *sopImport) result() []SOPImportError {
	seen := map[SOPImportError]bool{}
	var out []SOPImportError
	for _, e := range im.errs {
		k := SOPImportError{Sheet: e.Sheet, Row: e.Row, Column: e.Column}
		if !seen[k] {
			seen[k] = true
			out = append(out, e)
		}
	}
	sheetIdx := func(name string) int {
		for i, sh := range sopImportSheets {
			if sh.Name == name {
				return i
			}
		}
		return len(sopImportSheets)
	}
	colIdx := func(sheet, label string) int {
		for i, c := range importSheetOf(sheet).Cols {
			if c.Label == label {
				return i
			}
		}
		return len(importSheetOf(sheet).Cols)
	}
	sort.SliceStable(out, func(a, b int) bool {
		x, y := out[a], out[b]
		if sx, sy := sheetIdx(x.Sheet), sheetIdx(y.Sheet); sx != sy {
			return sx < sy
		}
		if x.Row != y.Row {
			return x.Row < y.Row
		}
		return colIdx(x.Sheet, x.Column) < colIdx(y.Sheet, y.Column)
	})
	return out
}

// ---------------------------------------------------------------------------
// Tệp mẫu
// ---------------------------------------------------------------------------

// SOPImportTemplate sinh tệp .xlsx mẫu từ cùng đặc tả cột với bộ đọc, kèm danh sách giá trị hợp lệ và hướng dẫn.
func (s *Service) SOPImportTemplate(ctx context.Context) ([]byte, error) {
	if _, err := authz.Require(ctx, sopAuthorRoles...); err != nil {
		return nil, err
	}
	lists := map[string][]string{
		colWindow:   labelsOf(windowKindLabels),
		colAnchor:   labelsOf(anchorLabels),
		colActivity: domain.ActivityLabels(),
	}
	if s.Catalogs != nil {
		ix, err := s.importCatalogs(ctx)
		if err != nil {
			return nil, err
		}
		lists[colCrop] = indexNames(ix.crop, func(it mdport.CatalogItem) bool { return it.Attrs[mdport.AttrParentCode] == "" })
		lists[colVariety] = indexNames(ix.variety, nil)
		lists[colMaterial] = indexNames(ix.material, nil)
	}

	var sheets []tabular.Sheet
	for _, sh := range sopImportSheets {
		out := tabular.Sheet{Name: sh.Name, FreezeHeader: true}
		header := make([]tabular.Cell, len(sh.Cols))
		last := templateListRows + 1
		if sh.Name == sheetInfo {
			last = 2 // một SOP mỗi tệp
		}
		for i, c := range sh.Cols {
			header[i] = tabular.Cell{Text: c.Label, Bold: true}
			out.Widths = append(out.Widths, max(14, len([]rune(c.Label))+4))
			if c.List == "" || len(lists[c.List]) == 0 {
				continue
			}
			listCol := slices.Index(sopLists, c.List) + 1
			letter, err := columnName(listCol)
			if err != nil {
				return nil, err
			}
			out.Lists = append(out.Lists, tabular.DropList{Col: i + 1, FirstRow: 2, LastRow: last,
				Source: "'" + sheetLists + "'!$" + letter + "$2:$" + letter + "$" + strconv.Itoa(len(lists[c.List])+1)})
		}
		out.Rows = [][]tabular.Cell{header}
		switch sh.Name {
		case sheetSteps:
			out.Widths[2] = 40
		case sheetStages:
			out.Widths[1] = 30
		case sheetInfo:
			out.Widths[2], out.Widths[3] = 40, 24
		}
		sheets = append(sheets, out)
	}
	sheets = append(sheets, listsSheet(lists), guideSheet())

	var buf bytes.Buffer
	if err := tabular.WriteSheetsXLSX(&buf, sheets); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// indexNames liệt kê tên các mục đang dùng (thỏa keep), sắp theo tên.
func indexNames(ix catalogIndex, keep func(mdport.CatalogItem) bool) []string {
	var names []string
	for _, items := range ix {
		for _, it := range items {
			if keep == nil || keep(it) {
				names = append(names, it.Name)
			}
		}
	}
	slices.SortFunc(names, func(a, b string) int { return strings.Compare(domain.NameKey(a), domain.NameKey(b)) })
	return names
}

func columnName(n int) (string, error) {
	if n < 1 || n > 26 {
		return "", errors.New("cropcycle: cột danh sách ngoài A..Z")
	}
	return string(rune('A' + n - 1)), nil
}

// listsSheet là sheet "Danh sách": mỗi cột là các giá trị hợp lệ của một cột chọn.
func listsSheet(lists map[string][]string) tabular.Sheet {
	sh := tabular.Sheet{Name: sheetLists, FreezeHeader: true}
	height := 0
	header := make([]tabular.Cell, len(sopLists))
	for i, name := range sopLists {
		header[i] = tabular.Cell{Text: name, Bold: true}
		height = max(height, len(lists[name]))
		sh.Widths = append(sh.Widths, 28)
	}
	sh.Rows = append(sh.Rows, header)
	for r := range height {
		row := make([]tabular.Cell, len(sopLists))
		for i, name := range sopLists {
			if r < len(lists[name]) {
				row[i] = tabular.Cell{Text: lists[name][r]}
			}
		}
		sh.Rows = append(sh.Rows, row)
	}
	return sh
}

// guideSheet là sheet "Hướng dẫn" ngắn.
func guideSheet() tabular.Sheet {
	lines := []string{
		"Mỗi tệp là một SOP. Nhập xong hệ thống tạo SOP Nháp; kiểm tra rồi phát hành như SOP soạn tay.",
		"Điền bằng chữ tiếng Việt như trên màn soạn SOP, không điền mã. Loại cây, giống, vật tư phải có trong danh mục (xem sheet " + sheetLists + "); tên không có trong danh mục là lỗi của dòng đó, hệ thống không tự tạo mục mới.",
		`Sheet "` + sheetInfo + `": đúng một dòng dữ liệu (Loại cây, Giống nếu có, Tên SOP, Tên mốc gốc).`,
		`Sheet "` + sheetStages + `": Thứ tự giai đoạn 1, 2, 3… liên tục; Từ và Đến là số ngày so với mốc gốc (nhập cả hai hoặc để trống cả hai).`,
		`Sheet "` + sheetSteps + `": cột Giai đoạn là số thứ tự giai đoạn; cột Thứ tự là thứ tự việc trong giai đoạn (1, 2, 3…). Các dòng đi theo thứ tự giai đoạn.`,
		"Loại cửa sổ: Theo ngày = cần Mốc, Từ, Đến; Mở = cần Mốc và Từ, để trống Đến; Theo giai đoạn = lấy khoảng ngày của giai đoạn chứa việc (giai đoạn phải có Từ–Đến), để trống Mốc, Từ, Đến; Không ngày = để trống Mốc, Từ, Đến.",
		"Mốc: NXG (ngày xuống giống, mốc gốc), NTH (ngày thu hoạch dự kiến), NTT (ngày thu hoạch thực tế). Từ và Đến là số ngày so với mốc, được âm, trong khoảng -365 đến 1825.",
		`Sheet "` + sheetMats + `": Giai đoạn và Thứ tự việc trỏ tới việc ở sheet "` + sheetSteps + `". Một việc có nhiều vật tư thì ghi nhiều dòng. Liều tính theo ha, lớn hơn 0, tối đa 4 chữ số lẻ.`,
		"Đơn vị để trống thì lấy đơn vị của vật tư trong danh mục; có điền thì giữ nguyên chữ đã điền (không so với danh mục).",
		"Không dùng công thức; không xóa sheet hay đổi tên cột. Lỗi bất kỳ thì hệ thống không tạo SOP và báo từng dòng lỗi để sửa tệp rồi nhập lại.",
	}
	sh := tabular.Sheet{Name: sheetGuide, Widths: []int{140}}
	sh.Rows = append(sh.Rows, []tabular.Cell{{Text: "Hướng dẫn nhập SOP từ Excel", Title: true}})
	for _, l := range lines {
		sh.Rows = append(sh.Rows, []tabular.Cell{{Text: l}})
	}
	return sh
}
