package domain

import "testing"

func TestCooperativeNameKey(t *testing.T) {
	for _, name := range []string{" HTX  LÚA XANH ", "htx\tlúa\nxanh", "HTX LU\u0301A XANH"} {
		if got := CooperativeNameKey(name); got != "htx lúa xanh" {
			t.Errorf("%q: %q", name, got)
		}
	}
	if CooperativeNameKey("HTX Lúa Xanh") == CooperativeNameKey("HTX Lua Xanh") {
		t.Fatal("không được bỏ dấu làm nhập nhằng tên khác nhau")
	}
}
