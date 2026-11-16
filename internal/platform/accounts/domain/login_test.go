package domain_test

import (
	"testing"
	"time"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/domain"
)

var t0 = time.Date(2026, 11, 16, 8, 0, 0, 0, time.UTC)

func TestLoginProbe_MULTIPLE_15Phut_Nhanh60Giay_CuaSo24Gio(t *testing.T) {
	var p domain.LoginProbe
	now := t0
	for i := 1; i <= 4; i++ {
		p = p.Fail(now)
		if locked, _ := p.Locked(now); locked {
			t.Fatalf("lần sai %d không được khóa", i)
		}
		now = now.Add(2 * time.Second)
	}
	p = p.Fail(now)
	locked, left := p.Locked(now)
	if !locked || left != domain.LoginLockout {
		t.Fatalf("lần sai thứ 5 phải khóa 15 phút, được locked=%v left=%v", locked, left)
	}
	if locked, _ := p.Locked(now.Add(domain.LoginLockout)); locked {
		t.Fatal("hết 15 phút phải mở")
	}

	// Hai lần sai cách nhau dưới 1 giây → khóa nhanh 60 giây.
	q := domain.LoginProbe{}.Fail(t0).Fail(t0.Add(500 * time.Millisecond))
	if locked, left := q.Locked(t0.Add(500 * time.Millisecond)); !locked || left != domain.LoginQuickWait {
		t.Fatalf("khóa nhanh 60 giây, được locked=%v left=%v", locked, left)
	}

	// Lần sai trước cách quá 24 giờ → đếm lại từ 0.
	r := domain.LoginProbe{Failures: 4, LastFailure: t0}.Fail(t0.Add(25 * time.Hour))
	if r.Failures != 1 {
		t.Fatalf("quá cửa sổ 24 giờ phải đếm lại, Failures=%d", r.Failures)
	}
	if locked, _ := r.Locked(t0.Add(25 * time.Hour)); locked {
		t.Fatal("không khóa sau khi đếm lại")
	}
}

func TestAfterFailure_Lan4ConMot_Lan5Khoa(t *testing.T) {
	last := t0
	now := t0.Add(2 * time.Second)
	tests := []struct {
		name       string
		before     int
		last       *time.Time
		wantRemain int
		wantLock   time.Duration
	}{
		{"lần 1", 0, nil, 4, 0},
		{"lần 4 còn một lần thử", 3, &last, 1, 0},
		{"lần 5 khóa 15 phút", 4, &last, 0, domain.LoginLockout},
		{"lần 6 sau khi hết khóa còn 4", 5, &last, 4, 0},
		{"quá 24 giờ đếm lại", 4, &last, 4, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			at := now
			if tc.name == "quá 24 giờ đếm lại" {
				at = t0.Add(25 * time.Hour)
			}
			rem, lock := domain.AfterFailure(tc.before, tc.last, at)
			if rem != tc.wantRemain || lock != tc.wantLock {
				t.Fatalf("remaining=%d lock=%v, muốn %d %v", rem, lock, tc.wantRemain, tc.wantLock)
			}
		})
	}
	if _, lock := domain.AfterFailure(1, &last, t0.Add(500*time.Millisecond)); lock != domain.LoginQuickWait {
		t.Fatalf("hai lần sai dưới 1 giây phải khóa 60 giây, được %v", lock)
	}
}

func TestNormalizeLoginIdentifier_SDT_84_KhoangTrang(t *testing.T) {
	tests := []struct {
		in, canonical, phone string
	}{
		{"0912345678", "0912345678", "0912345678"},
		{"+84 912 345 678", "0912345678", "0912345678"},
		{" 091-234.5678 ", "0912345678", "0912345678"},
		{"  Admin.User ", "admin.user", ""},
		{"12345", "12345", ""},
	}
	for _, tc := range tests {
		c, p := domain.NormalizeLoginIdentifier(tc.in)
		if c != tc.canonical || p != tc.phone {
			t.Errorf("%q → (%q,%q), muốn (%q,%q)", tc.in, c, p, tc.canonical, tc.phone)
		}
	}
}

func TestCeilMinutes(t *testing.T) {
	for in, want := range map[time.Duration]int{0: 1, time.Second: 1, time.Minute: 1, time.Minute + time.Second: 2, 15 * time.Minute: 15} {
		if got := domain.CeilMinutes(in); got != want {
			t.Errorf("CeilMinutes(%v)=%d, muốn %d", in, got, want)
		}
	}
}
