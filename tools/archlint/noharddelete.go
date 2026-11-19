package main

import (
	"go/ast"
	"go/token"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// NoHardDelete cấm câu SQL xóa cứng trong mã nghiệp vụ (RB-1, ADR-0011): mọi yêu cầu
// "xóa" hay "ghi đè" trong BRD đổi thành phiên bản mới hoặc bản ghi bù. Ngoại lệ:
// dựng lại projection (R-22), và đúng câu xóa Nút HTX CHƯA phát sinh dữ liệu
// của FN-03/DATA-03 (AH-283, comment PO 15/11/2026), có guard DB độc lập.
var NoHardDelete = &analysis.Analyzer{
	Name: "agnoharddelete",
	Doc: "cấm DELETE FROM / TRUNCATE trong internal/platform và internal/mkg (RB-1, ADR-0011) — " +
		"dùng trạng thái ngừng dùng, phiên bản mới hoặc bản ghi bù; chỉ hàm Rebuild*/Clear* (dựng lại bảng chiếu) được xóa",
	Run: runNoHardDelete,
}

var hardDeleteSQL = regexp.MustCompile(`(?is)\b(delete\s+from|truncate(\s+table)?\s+[a-z_"])`)

func businessPkg(pkg string) bool {
	return strings.Contains(pkg, "/internal/platform/") || strings.Contains(pkg, "/internal/kn/") || strings.Contains(pkg, "/internal/mkg/")
}

func rebuildFunc(name string) bool {
	return strings.HasPrefix(name, "Rebuild") || strings.HasPrefix(name, "Clear") ||
		strings.HasPrefix(name, "rebuild") || strings.HasPrefix(name, "clear")
}

func runNoHardDelete(pass *analysis.Pass) (any, error) {
	if !businessPkg(pass.Pkg.Path()) {
		return nil, nil
	}
	for _, f := range pass.Files {
		if strings.HasSuffix(pass.Fset.Position(f.Pos()).Filename, "_test.go") {
			continue
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if ok && rebuildFunc(fn.Name.Name) {
				continue
			}
			ast.Inspect(decl, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				s, err := strconv.Unquote(lit.Value)
				if err != nil {
					s = lit.Value
				}
				if hardDeleteSQL.MatchString(s) {
					if fn != nil && fn.Recv == nil && unusedCooperativeDelete(pass.Pkg.Path(), fn.Name.Name, s) {
						return true
					}
					pass.Reportf(lit.Pos(), "RB-1/ADR-0011: không xóa cứng dữ liệu nghiệp vụ — dùng ngừng dùng, phiên bản mới hoặc bản ghi bù (chỉ Rebuild*/Clear* dựng lại bảng chiếu được xóa)")
				}
				return true
			})
		}
	}
	return nil, nil
}

// Không miễn cả function/package hay mọi DELETE của Party. Nếu đổi đích,
// bỏ predicate kind hoặc thêm câu SQL thứ hai thì vẫn phải báo lỗi.
func unusedCooperativeDelete(pkg, name, sql string) bool {
	return strings.HasSuffix(pkg, "/internal/platform/party/store") && name == "RemoveCooperativeProjection" &&
		strings.ToLower(strings.Join(strings.Fields(sql), " ")) == "delete from pf_party.org where ref=$1 and kind='cooperative'"
}
