package accounts

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/app"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/shared/residency"
)

// SMSMailer là kênh email dùng cho chế độ mailhog (cùng kiểu với mailer đặt lại mật khẩu).
type SMSMailer = app.ResetMailer

// SMSFromEnv chọn kênh SMS của mã OTP đặt lại mật khẩu (AH-280, ADR-0063) theo AG_SMS (getenv nil → os.Getenv):
//
//	"" | disabled  tắt (nil): không tạo mã, báo liên hệ người cấp tài khoản — không báo "đã gửi" giả
//	mailhog        chuyển tin thành email vào hộp thư giả (MailHog) qua kênh SMTP đã cấu hình; chỉ máy phát triển,
//	               môi trường thử (AG_ENV=development|dev|local|test). Nơi khác hoặc thiếu SMTP → lỗi khởi động.
//
// Nhà cung cấp SMS Brandname thật làm ở ticket riêng, chỉ thêm một bản cài port.SMSSender.
func SMSFromEnv(getenv func(string) string, mailer SMSMailer) (port.SMSSender, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	switch mode := strings.ToLower(strings.TrimSpace(getenv("AG_SMS"))); mode {
	case "", "disabled":
		return nil, nil
	case "mailhog":
		if !residency.LocalDev(getenv) {
			return nil, fmt.Errorf("accounts: AG_SMS=mailhog chỉ dùng ở môi trường thử (AG_ENV=development|dev|local|test)")
		}
		if mailer == nil {
			return nil, fmt.Errorf("accounts: AG_SMS=mailhog cần kênh SMTP (AG_SMTP_HOST trỏ tới MailHog)")
		}
		return mailhogSMS{mailer: mailer}, nil
	default:
		return nil, fmt.Errorf("accounts: AG_SMS=%q không hỗ trợ (disabled | mailhog)", mode)
	}
}

// mailhogSMS chuyển tin SMS thành email trong hộp thư giả: tiêu đề là số điện thoại nhận, nội dung là tin SMS.
type mailhogSMS struct{ mailer app.ResetMailer }

const mailhogSMSInbox = "sms@htx.test"

func (m mailhogSMS) SendSMS(ctx context.Context, phone, text string) error {
	return m.mailer.Send(ctx, []string{mailhogSMSInbox}, "SMS "+phone, text)
}

// UseSMS nối kênh SMS (nil = tắt).
func (m *Module) UseSMS(sender port.SMSSender) { m.svc.SMS = sender }
