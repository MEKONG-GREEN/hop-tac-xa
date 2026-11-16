// Lệnh bootstrap-admin: tạo tài khoản Admin đầu tiên của App HTX khi triển khai (FN-01, QTPQ-Q-01) — không có màn hình.
//
//	go run ./cmd/bootstrap-admin -tenant <uuid> -org <uuid> -username <tên đăng nhập> [-email ...] [-first-name ...] [-last-name ...]
//
// Tài khoản mang vai platform_admin (quản trị toàn hệ thống; bắt buộc TOTP theo chính sách realm), ở trạng thái phải đổi mật
// khẩu và đăng ký TOTP ở lần đăng nhập đầu (trang xác thực của Keycloak). Lệnh sinh mật khẩu tạm và in ra stdout ĐÚNG MỘT LẦN:
// không ghi qua slog, không ghi tệp, không dán vào ticket hay tài liệu. -tenant và -org bắt buộc, không có giá trị mặc định.
//
// Cấu hình: AG_KC_ADMIN_URL, AG_KC_REALM, AG_KC_CLIENT_ID, AG_KC_CLIENT_SECRET (như api-platform).
package main

import (
	"context"
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/port"
)

func main() {
	dir, err := accounts.DirectoryFromEnv()
	if err != nil {
		fmt.Fprintln(os.Stderr, "bootstrap-admin:", err)
		os.Exit(1)
	}
	if err := run(context.Background(), os.Args[1:], dir, os.Stdout, rand.Reader); err != nil {
		fmt.Fprintln(os.Stderr, "bootstrap-admin:", err)
		os.Exit(1)
	}
}

// run phân tích cờ, tạo tài khoản và in kết quả ra stdout. dir nil → lỗi chưa cấu hình Keycloak Admin.
func run(ctx context.Context, args []string, dir port.Directory, stdout io.Writer, rnd io.Reader) error {
	fs := flag.NewFlagSet("bootstrap-admin", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	tenantFlag := fs.String("tenant", "", "UUID tenant của Admin (bắt buộc)")
	orgFlag := fs.String("org", "", "UUID đơn vị của Admin (bắt buộc)")
	username := fs.String("username", "", "tên đăng nhập (bắt buộc)")
	email := fs.String("email", "", "email (tùy chọn)")
	first := fs.String("first-name", "", "tên (tùy chọn)")
	last := fs.String("last-name", "", "họ và tên đệm (tùy chọn)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	tenantID, err := uuid.Parse(*tenantFlag)
	if err != nil {
		fs.Usage()
		return errors.New("-tenant bắt buộc và phải là UUID")
	}
	org, err := uuid.Parse(*orgFlag)
	if err != nil {
		fs.Usage()
		return errors.New("-org bắt buộc và phải là UUID")
	}
	if *username == "" {
		fs.Usage()
		return errors.New("-username bắt buộc")
	}
	if dir == nil {
		return errors.New("chưa cấu hình AG_KC_ADMIN_URL (cần AG_KC_ADMIN_URL, AG_KC_REALM, AG_KC_CLIENT_ID, AG_KC_CLIENT_SECRET)")
	}
	id, password, err := accounts.BootstrapAdmin(ctx, dir, accounts.BootstrapAdminInput{
		TenantID: tenantID, OrgRef: org, Username: *username, Email: *email, FirstName: *first, LastName: *last,
	}, rnd)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "Đã tạo tài khoản Admin đầu tiên.\n"+
		"  Tên đăng nhập: %s\n"+
		"  Mã tài khoản:  %s\n"+
		"  Mật khẩu tạm:  %s\n\n"+
		"Mật khẩu chỉ hiển thị MỘT lần này — hãy chuyển cho người nhận qua kênh an toàn, không dán vào ticket hay tài liệu.\n"+
		"Lần đăng nhập đầu: bấm \"Hoàn tất trên trang xác thực\" để đổi mật khẩu và đăng ký OTP (TOTP) trên trang xác thực của Keycloak.\n",
		*username, id, password)
	return err
}
