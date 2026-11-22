package port

import "context"

// SMSSender gửi một tin SMS tới số điện thoại của tài khoản (AH-280, ADR-0063). Số điện thoại lấy từ IAM lúc gửi,
// không lưu ở PF; nội dung tin không vào log. Đổi nhà cung cấp chỉ đổi bản cài của cổng này, không sửa luồng OTP.
type SMSSender interface {
	SendSMS(ctx context.Context, phone, text string) error
}
