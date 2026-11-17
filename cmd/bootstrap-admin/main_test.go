package main

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/port"
)

func TestRun_InMatKhauMotLan_ThieuCoThiLoi(t *testing.T) {
	ctx := context.Background()
	tid, org := uuid.NewString(), uuid.NewString()

	dir := accounts.NewMemoryDirectory()
	var out bytes.Buffer
	if err := run(ctx, []string{"-tenant", tid, "-org", org, "-username", "admin", "-email", "admin@example.vn"}, dir, &out, nil); err != nil {
		t.Fatal(err)
	}
	users, _ := dir.FindUsers(ctx, port.Query{Username: "admin"})
	if len(users) != 1 {
		t.Fatalf("phải tạo đúng một tài khoản, có %d", len(users))
	}
	var pw string
	for _, line := range strings.Split(out.String(), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "Mật khẩu tạm:"); ok {
			pw = strings.TrimSpace(v)
		}
	}
	if pw == "" || !dir.PasswordMatches(users[0].ID, pw) || strings.Count(out.String(), pw) != 1 {
		t.Fatalf("mật khẩu tạm phải in đúng một lần và là mật khẩu thật của tài khoản:\n%s", out.String())
	}
	if !strings.Contains(out.String(), users[0].ID.String()) {
		t.Fatal("phải in mã tài khoản")
	}

	for name, args := range map[string][]string{
		"thiếu tenant":   {"-org", org, "-username", "a"},
		"thiếu đơn vị":   {"-tenant", tid, "-username", "a"},
		"tenant sai":     {"-tenant", "khong-phai-uuid", "-org", org, "-username", "a"},
		"đơn vị sai":     {"-tenant", tid, "-org", "x", "-username", "a"},
		"thiếu username": {"-tenant", tid, "-org", org, "-email", "a@example.vn"},
		"thiếu email":    {"-tenant", tid, "-org", org, "-username", "a"},
	} {
		d := accounts.NewMemoryDirectory()
		var buf bytes.Buffer
		if err := run(ctx, args, d, &buf, nil); err == nil || buf.Len() != 0 {
			t.Errorf("%s: phải lỗi và không in gì (err=%v, out=%q)", name, err, buf.String())
		}
		if u, _ := d.FindUsers(ctx, port.Query{}); len(u) != 0 {
			t.Errorf("%s: không được tạo tài khoản", name)
		}
	}

	if err := run(ctx, []string{"-tenant", tid, "-org", org, "-username", "a", "-email", "a@example.vn"}, nil, io.Discard, nil); err == nil ||
		!strings.Contains(err.Error(), "AG_KC_ADMIN_URL") {
		t.Fatalf("thiếu Keycloak phải báo chưa cấu hình AG_KC_ADMIN_URL: %v", err)
	}
}
