package domain

import (
	"bytes"
	"regexp"
	"testing"

	"github.com/google/uuid"
)

func TestNewLoginOTPCode_SauChuSo(t *testing.T) {
	re := regexp.MustCompile(`^[0-9]{6}$`)
	seen := map[string]bool{}
	for range 200 {
		c, err := NewLoginOTPCode(nil)
		if err != nil || !re.MatchString(c) {
			t.Fatalf("mã %q, lỗi %v", c, err)
		}
		seen[c] = true
	}
	if len(seen) < 190 {
		t.Fatalf("mã phải ngẫu nhiên, chỉ có %d mã khác nhau trong 200 lần", len(seen))
	}
	// Nguồn ngẫu nhiên cạn → lỗi, không trả mã rỗng.
	if c, err := NewLoginOTPCode(bytes.NewReader(nil)); err == nil || c != "" {
		t.Fatalf("nguồn ngẫu nhiên hỏng phải lỗi: %q %v", c, err)
	}
}

func TestLoginOTPHash_GanVoiKhoaVaThuThach(t *testing.T) {
	key, other := bytes.Repeat([]byte{1}, 32), bytes.Repeat([]byte{2}, 32)
	ch, ch2 := uuid.New(), uuid.New()
	h := LoginOTPHash(key, ch, "123456")
	switch {
	case !LoginOTPMatches(key, ch, "123456", h):
		t.Fatal("mã đúng phải khớp")
	case LoginOTPMatches(key, ch, "123457", h):
		t.Fatal("mã sai không được khớp")
	case LoginOTPMatches(other, ch, "123456", h):
		t.Fatal("khóa khác không được khớp")
	case LoginOTPMatches(key, ch2, "123456", h):
		t.Fatal("mã của thử thách này không dùng được cho thử thách khác")
	case LoginOTPMatches(key, ch, "123456", h[:31]):
		t.Fatal("băm sai độ dài không được khớp")
	}
}

func TestMaskEmail(t *testing.T) {
	for in, want := range map[string]string{
		"quan.tri@mekonggreen.vn": "q***@mekonggreen.vn",
		" Ánh@example.vn ":        "Á***@example.vn",
		"khong-co-a-cong":         "***",
		"@example.vn":             "***",
		"a@":                      "***",
	} {
		if got := MaskEmail(in); got != want {
			t.Errorf("MaskEmail(%q) = %q, muốn %q", in, got, want)
		}
	}
}
