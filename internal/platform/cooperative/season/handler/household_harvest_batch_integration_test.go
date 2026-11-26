package handler_test

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/ids"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/tabular"
)

func batchRow(line int, ref, event, mass, day string, zero bool) map[string]any {
	r := map[string]any{"line": line, "season_ref": ref, "mass_t": mass, "harvest_date": day, "zero_confirmed": zero}
	if event != "" {
		r["expected_season_event_id"] = event
	}
	return r
}

func rowByLine(t *testing.T, body any, line int) map[string]any {
	t.Helper()
	for _, r := range list(body, "rows") {
		if int(num(r, "line")) == line {
			m, _ := r.(map[string]any)
			return m
		}
	}
	t.Fatalf("không có dòng %d: %v", line, body)
	return nil
}

// AH-177 AC1–AC4: khai báo sản lượng nhiều vụ nông hộ — dòng lỗi bỏ qua kèm lý do, dòng hợp lệ lưu theo quy tắc AH-100;
// phát lại cùng khóa; tệp Excel mẫu (KH-20), ô ngày Excel, tệp sai mẫu; quyền và phạm vi HTX.
func TestAH177_HouseholdHarvestBatchAndImport(t *testing.T) {
	f := newFixture(t)
	f.publishedSOP("AH177 OM5451", "OM5451")
	f.householdLand("3", true)
	c := f.memberA()
	open := func(area, anchor string) (path, ref, event string) {
		t.Helper()
		r := c.post(f.householdPath(), householdOpenBody(map[string]any{"area_ha": area, "anchor_date": anchor}))
		expect(t, r, http.StatusCreated, "mở vụ "+anchor)
		ref = str(r.body, "season", "season_ref")
		return f.householdPath() + "/" + ref, ref, str(r.body, "season", "current_event_id")
	}
	pathA, refA, eventA := open("0.50", "2026-06-01")
	_, refB, eventB := open("0.50", "2026-06-10")
	pathC, refC, _ := open("0.50", "2026-06-15")
	pathD, refD, _ := open("0.25", "2026-06-20")
	pathE, refE, eventE := open("0.25", "2026-06-25")
	_, refF, eventF := open("0.25", "2026-06-26")
	_, refG, _ := open("0.25", "2026-06-27")
	expect(t, c.post(pathE+"/stop", map[string]any{"expected_season_event_id": eventE, "reason": "Ruộng bị ngập nặng, dừng vụ"}), http.StatusOK, "dừng E")
	// A đã ghi việc Thu hoạch (KH-20).
	expect(t, c.postKey(pathA+"/task-completions", key(), tickBody(5, eventA)), http.StatusCreated, "tích thu hoạch A")
	coopBase := "/v1/cooperatives/" + f.coopA.String()

	// Tệp mẫu: mặc định chỉ vụ đã ghi thu hoạch; scope=all mọi vụ đang canh tác (không có vụ đã dừng).
	templateRefs := func(q string) []string {
		t.Helper()
		r := c.get(coopBase + "/harvest-template" + q)
		if r.status != http.StatusOK {
			t.Fatalf("tệp mẫu %s: %d %s", q, r.status, r.raw)
		}
		tb, err := tabular.ReadBytes([]byte(r.raw), tabular.Limits{})
		if err != nil {
			t.Fatal(err)
		}
		out := []string{}
		for _, row := range tb.Rows {
			out = append(out, row.Cells[0])
		}
		return out
	}
	if got := templateRefs(""); len(got) != 1 || got[0] != refA {
		t.Fatalf("tệp mẫu mặc định: %v", got)
	}
	if got := templateRefs("?scope=all"); len(got) != 6 || strings.Contains(strings.Join(got, ","), refE) {
		t.Fatalf("tệp mẫu all: %v", got)
	}
	// Danh sách chờ khai báo lọc vụ đã ghi thu hoạch.
	if items := list(c.get(coopBase+"/household-yields?state=pending&harvest_recorded_only=true").body, "items"); len(items) != 1 ||
		str(items[0], "season_ref") != refA || items[0].(map[string]any)["harvest_recorded"] != true || str(items[0], "current_event_id") != eventA {
		t.Fatalf("lọc đã ghi thu hoạch: %v", items)
	}

	// AC2: lô lẫn dòng hợp lệ và dòng lỗi.
	k := key()
	body := map[string]any{"rows": []map[string]any{
		batchRow(1, refA, eventA, "3", "2026-09-20", false),
		batchRow(2, refB, eventB, "-1", "2026-09-20", false),
		batchRow(3, refC, "", "0", "2026-09-20", false),
		batchRow(4, refD, "", "1,5", "20/09/2026", false),
		batchRow(5, refE, "", "1", "2026-09-20", false),
		batchRow(6, ids.New().String(), "", "1", "2026-09-20", false),
		batchRow(7, refA, "", "2", "2026-09-20", false),
		batchRow(8, "abc", "", "1", "2026-09-20", false),
		batchRow(9, refF, eventF, "1", "", false),
		batchRow(10, refG, "", "1", "2099-01-01", false),
	}}
	expect(t, f.managerA().postKey(coopBase+"/harvest-batches", key(), body), http.StatusForbidden, "Đại diện")
	if r := f.memberB().postKey(coopBase+"/harvest-batches", key(), body); r.status != http.StatusNotFound {
		t.Fatalf("HTX khác: %d %v", r.status, r.body)
	}
	r := c.postKey(coopBase+"/harvest-batches", k, body)
	expect(t, r, http.StatusOK, "lưu tất cả")
	if num(r.body, "accepted") != 2 || num(r.body, "rejected") != 8 {
		t.Fatalf("kết quả: %v", r.body)
	}
	for line, want := range map[int]string{1: "accepted", 4: "accepted"} {
		if s := str(rowByLine(t, r.body, line), "status"); s != want {
			t.Fatalf("dòng %d: %s", line, s)
		}
	}
	if row := rowByLine(t, r.body, 3); str(row, "code") != "cropcycle.harvest_zero_confirmation_required" || row["needs_confirm"] != true {
		t.Fatalf("dòng 0 chưa xác nhận: %v", row)
	}
	for line, code := range map[int]string{2: "validation_failed", 5: "cropcycle.season_state", 6: "cropcycle.season_not_found",
		7: "validation_failed", 8: "validation_failed", 9: "validation_failed", 10: "validation_failed"} {
		row := rowByLine(t, r.body, line)
		if str(row, "status") != "rejected" || str(row, "code") != code || str(row, "reason") == "" {
			t.Fatalf("dòng %d: %v", line, row)
		}
	}
	if !strings.Contains(str(rowByLine(t, r.body, 7), "reason"), "trùng") {
		t.Fatalf("dòng trùng: %v", rowByLine(t, r.body, 7))
	}
	// Dòng hợp lệ áp quy tắc AH-100: vụ "Đã thu hoạch", việc Thu hoạch tự Xong.
	if g := c.get(pathD); str(g.body, "status") != "completed" || str(g.body, "harvest", "mass_t") != "1.50" {
		t.Fatalf("D sau lưu: %v", g.body)
	}
	if task := checklistTask(c.get(pathD+"/checklist").body, 5); task["done_on"] != "2026-09-20" {
		t.Fatalf("việc Thu hoạch D: %v", task)
	}
	// Gửi lại cùng khóa: trả kết quả đã ghi, không ghi lần hai.
	first := str(rowByLine(t, r.body, 1), "harvest_id")
	r = c.postKey(coopBase+"/harvest-batches", k, body)
	if num(r.body, "accepted") != 2 || str(rowByLine(t, r.body, 1), "harvest_id") != first {
		t.Fatalf("gửi lại: %v", r.body)
	}
	if n := len(list(c.get(pathA+"/harvest/history").body, "items")); n != 1 {
		t.Fatalf("ghi hai lần: %d", n)
	}
	// Phiên bản vụ đã đổi.
	r = c.postKey(coopBase+"/harvest-batches", key(), map[string]any{"rows": []map[string]any{batchRow(1, refB, "01a00000-0000-7000-8000-000000000001", "1", "2026-09-20", false)}})
	if str(rowByLine(t, r.body, 1), "code") != "cropcycle.task_season_changed" {
		t.Fatalf("phiên bản cũ: %v", r.body)
	}
	expect(t, c.postKey(coopBase+"/harvest-batches", key(), map[string]any{"rows": []map[string]any{}}), http.StatusUnprocessableEntity, "lô rỗng")

	// AC3: tệp Excel mẫu — ô ngày kiểu Excel, xác nhận 0, dòng chưa nhập bỏ qua, vụ đã khai báo là dòng lỗi.
	x := excelize.NewFile()
	sheet := x.GetSheetName(0)
	for i, h := range []string{"Mã vụ", "Mã hộ", "Sản lượng (tấn)", "Ngày thu hoạch cuối", "Xác nhận 0"} {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		_ = x.SetCellStr(sheet, cell, h)
	}
	_ = x.SetCellStr(sheet, "A2", refC)
	_ = x.SetCellStr(sheet, "C2", "0")
	_ = x.SetCellValue(sheet, "D2", time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC))
	_ = x.SetCellStr(sheet, "E2", "x")
	_ = x.SetCellStr(sheet, "A3", refA)
	_ = x.SetCellStr(sheet, "C3", "4")
	_ = x.SetCellStr(sheet, "D3", "21/09/2026")
	_ = x.SetCellStr(sheet, "A4", refB) // dòng chưa nhập
	var buf bytes.Buffer
	if err := x.Write(&buf); err != nil {
		t.Fatal(err)
	}
	r = c.do(f.app.URL, http.MethodPost, coopBase+"/harvest-imports", key(), tabular.ContentTypeXLSX, buf.Bytes())
	expect(t, r, http.StatusOK, "nhập Excel")
	if num(r.body, "accepted") != 1 || num(r.body, "rejected") != 1 || len(list(r.body, "rows")) != 2 ||
		str(rowByLine(t, r.body, 2), "status") != "accepted" || str(rowByLine(t, r.body, 3), "code") != "cropcycle.season_state" {
		t.Fatalf("nhập Excel: %v", r.body)
	}
	if g := c.get(pathC); str(g.body, "status") != "completed" || str(g.body, "harvest", "mass_t") != "0.00" || str(g.body, "harvest", "harvest_date") != "2026-09-21" ||
		g.body["harvest"].(map[string]any)["zero_confirmed"] != true {
		t.Fatalf("C sau nhập: %v", g.body)
	}
	// Tệp sai mẫu bị từ chối toàn bộ.
	bad := c.do(f.app.URL, http.MethodPost, coopBase+"/harvest-imports", key(), "text/csv", []byte("Mã vụ,Ghi chú\n"+refB+",x\n"))
	expectCode(t, bad, http.StatusUnprocessableEntity, "cropcycle.import_invalid", "tệp sai mẫu")
	expect(t, f.managerA().get(coopBase+"/harvest-template"), http.StatusForbidden, "Đại diện tải mẫu")
	_ = pathA
}
