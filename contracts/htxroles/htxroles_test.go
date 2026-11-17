package htxroles_test

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/hop-tac-xa/contracts/htxroles"
)

// Ma trận Mục V (FN-05, ADM-01) chép đúng bản đã chốt 17/11/2026: 3 vai, 14 dòng, ký hiệu ⊆ CRUDLPX; cùng 9 dòng
// KHMV-* của Mục VI BRD Kế hoạch mùa vụ v3.9 (AH-39, KH-02, KH-28).
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
		"KHMV-FN-01": {"CRUD", "R", "R"}, "KHMV-FN-02": {"R", "R", "R"}, "KHMV-FN-03": {"CRUD", "R", "CRUD"},
		"KHMV-FN-03-LICH": {"CRUD", "CRUD", "R"}, "KHMV-FN-03-XEM": {"R", "R", "R"}, "KHMV-FN-04": {"CRUD", "R", "CRUD"},
		"KHMV-FN-05": {"CRUD", "R", "CRUD"}, "KHMV-FN-06": {"CRUD", "R", "CRUD"}, "KHMV-NTF": {"", "", "R"},
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
	// Thành viên HTX chỉ xem (FN-05/AC-01), trừ tự đổi mật khẩu của mình (FN-10). Các dòng KHMV-* theo BRD Kế hoạch
	// mùa vụ (Thành viên HTX mở vụ, ghi nhật ký, khai sản lượng) kiểm ở TestAllows_KHMV.
	for _, f := range htxroles.Load().Functions {
		if strings.HasPrefix(f.Key, "KHMV-") {
			continue
		}
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

// BRD mục V, FN-05/BR-02: vai có hiệu lực theo thứ tự Admin > Đại diện HTX > Thành viên HTX, không cộng gộp; vai
// ngoài ma trận không tính (AH-39).
func TestEffective_KHMV(t *testing.T) {
	for _, c := range []struct {
		roles []string
		want  string
	}{
		{[]string{htxroles.Member}, htxroles.Member},
		{[]string{htxroles.Representative, htxroles.Member}, htxroles.Representative},
		{[]string{htxroles.Member, htxroles.Admin}, htxroles.Admin},
		{[]string{htxroles.Member, "kn_officer"}, htxroles.Member},
		{[]string{"kn_officer"}, ""},
		{nil, ""},
	} {
		if got := htxroles.Effective(func(r string) bool { return slices.Contains(c.roles, r) }); got != c.want {
			t.Errorf("Effective(%v) = %q, cần %q", c.roles, got, c.want)
		}
	}
}

// Ma trận Mục VI BRD Kế hoạch mùa vụ: Thành viên HTX ghi vụ, nhật ký, sản lượng; Đại diện HTX chỉ xem nhưng lập lịch
// thời vụ; tài khoản kiêm hai vai dùng quyền Đại diện HTX; Admin đủ quyền (AH-39, KH-28).
func TestAllows_KHMV(t *testing.T) {
	has := func(roles ...string) func(string) bool {
		return func(r string) bool { return slices.Contains(roles, r) }
	}
	member, rep := has(htxroles.Member), has(htxroles.Representative)
	both, admin := has(htxroles.Representative, htxroles.Member), has(htxroles.Admin, htxroles.Member)
	for _, fn := range []string{"KHMV-FN-03", "KHMV-FN-04", "KHMV-FN-05", "KHMV-FN-06"} {
		for _, a := range []htxroles.Action{htxroles.Create, htxroles.Update, htxroles.Delete} {
			if !htxroles.Allows(member, fn, a) || !htxroles.Allows(admin, fn, a) {
				t.Errorf("%s/%c: Thành viên HTX và Admin phải được ghi", fn, a)
			}
			if htxroles.Allows(rep, fn, a) || htxroles.Allows(both, fn, a) {
				t.Errorf("%s/%c: Đại diện HTX (kể cả kiêm Thành viên HTX) chỉ xem", fn, a)
			}
		}
		for _, h := range []func(string) bool{member, rep, both, admin} {
			if !htxroles.Allows(h, fn, htxroles.Read) {
				t.Errorf("%s/R: cả ba vai được xem", fn)
			}
		}
	}
	for _, a := range []htxroles.Action{htxroles.Create, htxroles.Read, htxroles.Update, htxroles.Delete} {
		if !htxroles.Allows(rep, "KHMV-FN-03-LICH", a) {
			t.Errorf("KHMV-FN-03-LICH/%c: Đại diện HTX lập, sửa, hủy, phát lịch thời vụ", a)
		}
	}
	if htxroles.Allows(member, "KHMV-FN-03-LICH", htxroles.Create) || htxroles.Allows(rep, "KHMV-FN-01", htxroles.Create) {
		t.Error("Thành viên HTX chỉ xem lịch thời vụ; Đại diện HTX chỉ xem danh mục SOP (KH-04 tách ticket)")
	}
	if !htxroles.Allows(member, "KHMV-NTF", htxroles.Read) || htxroles.Allows(rep, "KHMV-NTF", htxroles.Read) || htxroles.Allows(has(htxroles.Admin), "KHMV-NTF", htxroles.Read) {
		t.Error("KHMV-NTF: chỉ Thành viên HTX nhận thông báo của BRD Kế hoạch mùa vụ")
	}
	// Kế hoạch mùa vụ chưa có API riêng: grants của FN-04 bằng FN-03 ở cả ba vai.
	for _, r := range []string{htxroles.Admin, htxroles.Representative, htxroles.Member} {
		if htxroles.Grants(r, "KHMV-FN-04") != htxroles.Grants(r, "KHMV-FN-03") {
			t.Errorf("KHMV-FN-04/%s khác KHMV-FN-03", r)
		}
	}
	if htxroles.Allows(has("kn_officer"), "KHMV-FN-03", htxroles.Read) {
		t.Error("vai ngoài ma trận không có quyền theo ma trận")
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
