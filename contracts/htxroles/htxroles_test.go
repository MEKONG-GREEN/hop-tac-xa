package htxroles_test

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
)

// Ma trận Mục V (FN-05, ADM-01) chép đúng bản đã chốt 17/11/2026: 3 vai, 14 dòng, ký hiệu ⊆ CRUDLPX.
func TestMatrix(t *testing.T) {
	m := htxroles.Load()
	if m.Version != 1 {
		t.Fatalf("version = %d", m.Version)
	}
	var roles []string
	for _, r := range m.Roles {
		roles = append(roles, r.Key)
	}
	if !slices.Equal(roles, []string{htxroles.Admin, htxroles.Representative, htxroles.Member}) {
		t.Fatalf("vai của ma trận = %v", roles)
	}
	for _, a := range "CRUDLPX" {
		if m.Actions[string(a)] == "" {
			t.Errorf("thiếu nhãn ký hiệu %c", a)
		}
	}
	want := map[string][3]string{
		"FN-01": {"R", "R", "R"}, "FN-02": {"CRU", "", ""}, "FN-03": {"CRUD", "RU", "R"}, "FN-04": {"CRUD", "CRUD", "R"},
		"FN-07": {"CRUL", "", ""}, "FN-08": {"", "CRUL", ""}, "FN-09": {"", "CRUD", ""}, "FN-10-OTP": {"P", "P", ""},
		"FN-10-SELF": {"U", "U", "U"}, "FN-11": {"CRU", "", ""}, "FN-12": {"R", "R", "R"}, "FN-13": {"R", "R", ""},
		"FN-14": {"R", "", ""}, "FN-15": {"RX", "RX", "R"},
	}
	if len(m.Functions) != len(want) {
		t.Fatalf("ma trận có %d dòng, cần %d", len(m.Functions), len(want))
	}
	for _, f := range m.Functions {
		w, ok := want[f.Key]
		if !ok {
			t.Errorf("dòng lạ %s", f.Key)
			continue
		}
		if len(f.Grants) != len(roles) {
			t.Errorf("%s: grants phải có đúng ba vai: %v", f.Key, f.Grants)
		}
		for i, r := range roles {
			g := f.Grants[r]
			if g != w[i] {
				t.Errorf("%s/%s = %q, cần %q", f.Key, r, g, w[i])
			}
			if strings.Trim(g, "CRUDLPX") != "" {
				t.Errorf("%s/%s: ký hiệu ngoài CRUDLPX: %q", f.Key, r, g)
			}
		}
	}
}

func TestMatrixHelpers(t *testing.T) {
	if got := htxroles.With("FN-04", htxroles.Read); !slices.Equal(got, []string{htxroles.Admin, htxroles.Representative, htxroles.Member}) {
		t.Fatalf("With(FN-04, R) = %v", got)
	}
	if got := htxroles.With("FN-02", htxroles.Read); !slices.Equal(got, []string{htxroles.Admin}) {
		t.Fatalf("FN-05/AC-02: chỉ Admin xem Đơn vị hành chính: %v", got)
	}
	if got := htxroles.With("FN-07", htxroles.Read); !slices.Equal(got, []string{htxroles.Admin}) {
		t.Fatalf("FN-05/AC-01: Thành viên HTX không xem Tài khoản: %v", got)
	}
	if htxroles.Can(htxroles.Member, "FN-04", htxroles.Create) || !htxroles.Can(htxroles.Representative, "FN-04", htxroles.Delete) {
		t.Fatal("FN-04: Thành viên HTX chỉ xem, Đại diện HTX CRUD")
	}
	if htxroles.Grants(htxroles.Admin, "FN-99") != "" || htxroles.Can(htxroles.Admin, "FN-99", htxroles.Read) {
		t.Fatal("chức năng không có trong ma trận → không có quyền")
	}
	// Thành viên HTX chỉ xem (FN-05/AC-01), trừ tự đổi mật khẩu của mình (FN-10).
	for _, f := range htxroles.Load().Functions {
		g := f.Grants[htxroles.Member]
		if f.Key == "FN-10-SELF" {
			if g != "U" {
				t.Errorf("FN-10-SELF/htx_member = %q", g)
			}
			continue
		}
		if g != "" && g != "R" {
			t.Errorf("%s: Thành viên HTX chỉ được xem, có %q", f.Key, g)
		}
	}
}

func TestMemberWriteRoutesShape(t *testing.T) {
	re := regexp.MustCompile(`^(POST|PUT|PATCH|DELETE) /v1/\S+$`)
	routes := htxroles.MemberWriteRoutes()
	for _, r := range routes {
		if !re.MatchString(r) {
			t.Errorf("member_write_routes %q phải dạng \"<POST|PUT|PATCH|DELETE> /v1/…\"", r)
		}
	}
	if len(routes) > 0 {
		routes[0] = "mutated"
		if htxroles.MemberWriteRoutes()[0] == "mutated" {
			t.Fatal("MemberWriteRoutes phải trả bản sao")
		}
	}
}

// FN-05/BR-02: tài khoản kiêm Đại diện HTX và Thành viên HTX dùng quyền Đại diện HTX; Admin không kiêm.
func TestIsMemberOnly(t *testing.T) {
	for _, c := range []struct {
		roles []string
		want  bool
	}{
		{[]string{htxroles.Member}, true},
		{[]string{htxroles.Member, "plot_viewer"}, true},
		{[]string{htxroles.Member, htxroles.Representative}, false},
		{[]string{htxroles.Member, htxroles.Admin}, false},
		{[]string{htxroles.Representative}, false},
		{[]string{htxroles.Admin}, false},
		{nil, false},
	} {
		if got := htxroles.IsMemberOnly(func(r string) bool { return slices.Contains(c.roles, r) }); got != c.want {
			t.Errorf("IsMemberOnly(%v) = %v, cần %v", c.roles, got, c.want)
		}
	}
}

// FN-06/BR-02, BR-03: Đại diện HTX và Thành viên HTX chỉ thấy HTX gắn với tài khoản; vai có phạm vi rộng hơn mà API
// nhận (wider) hoặc Admin thì không bị thu hẹp — kể cả khi kèm Thành viên HTX.
func TestOwnCoopOnly(t *testing.T) {
	wider := []string{"kn_officer", "registry_admin"}
	for _, c := range []struct {
		roles []string
		want  bool
	}{
		{[]string{htxroles.Member}, true},
		{[]string{htxroles.Representative}, true},
		{[]string{htxroles.Representative, htxroles.Member}, true},
		{[]string{htxroles.Representative, "kn_officer"}, false},
		{[]string{htxroles.Representative, "plot_viewer"}, true},
		{[]string{htxroles.Representative, htxroles.Admin}, false},
		{[]string{htxroles.Member, htxroles.Admin}, false},
		{[]string{htxroles.Member, "kn_officer"}, false},
		{[]string{htxroles.Member, "registry_admin"}, false},
		{[]string{htxroles.Member, "plot_viewer"}, true},
		{[]string{"kn_officer"}, false},
		{nil, false},
	} {
		if got := htxroles.OwnCoopOnly(func(r string) bool { return slices.Contains(c.roles, r) }, wider...); got != c.want {
			t.Errorf("OwnCoopOnly(%v) = %v, cần %v", c.roles, got, c.want)
		}
	}
}
