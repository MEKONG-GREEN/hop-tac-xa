# ADR-0063: Kênh SMS cho mã OTP đặt lại mật khẩu (FN-10)

**Trạng thái:** Chấp nhận. **Ngày:** 22/11/2026. **Nguồn:** AH-280 (FN-10, TECH-03), trả lời PO 15/11/2026.

FN-10 cho người dùng realm HTX quên mật khẩu tự đặt lại bằng mã OTP gửi qua SMS tới số điện thoại của tài khoản. Nhà cung cấp SMS Brandname, tên định danh và mẫu tin chưa được nhà mạng duyệt. PO chốt: trong lúc chờ, môi trường thử chuyển tin SMS thành email vào hộp thư giả (MailHog); nơi khác chưa có kênh thì báo liên hệ người cấp tài khoản để được cấp mật khẩu tạm (AH-281). Chú thích của kênh Zalo ZNS yêu cầu ADR trước khi bật một kênh dùng số điện thoại; ADR này là ADR đó cho kênh SMS.

## Quyết định

- Luồng OTP nằm ở `pf/accounts` (cùng nơi đăng nhập FN-01), gửi qua cổng `port.SMSSender`. Đổi nhà cung cấp chỉ thêm một bản cài cổng này.
- Chọn kênh bằng `AG_SMS`:
  - trống hoặc `disabled` (mặc định): tắt. Bấm "Gửi mã OTP" không tạo mã, trả 503 với câu liên hệ người cấp tài khoản; không báo "đã gửi" giả (fail-closed).
  - `mailhog`: tin thành email gửi tới `sms@htx.test` qua kênh SMTP đã cấu hình, tiêu đề là số điện thoại nhận. Chỉ hợp lệ khi `AG_ENV` là development, dev, local hoặc test; nơi khác hoặc thiếu SMTP thì máy chủ không khởi động.
  - Nhà cung cấp thật: ticket riêng, khi đã có tên định danh.
- Số điện thoại lấy từ IAM lúc gửi (thuộc tính tài khoản FN-07), không lưu ở PF, không vào log. Module notify không giữ số.
- Mã 06 chữ số (crypto/rand), dùng một lần, hiệu lực 05 phút, sai 05 lần thì hủy, gửi lại sau 60 giây, tối đa 05 mã / tài khoản / 24 giờ trượt (gồm cả lần Admin, Đại diện HTX gửi hộ). Bảng `pf_accounts.password_otp_challenge` chỉ lưu HMAC-SHA256 (khóa `AG_LOGIN_ID_KEY`) của mã gắn với id thử thách; không lưu mã, số điện thoại hay IP; không DELETE.
- EX-01 báo rõ "không tìm thấy tài khoản" theo BRD; bù lại bằng giới hạn theo IP như route đăng nhập.
- Đặt lại thành công: mật khẩu cố định qua Admin REST (chính sách realm `length(6)`), bỏ `UPDATE_PASSWORD` (QTPQ-Q-07), thu hồi phiên cũ.

## Hệ quả

- Môi trường chính thức chưa có SMS: người dùng quên mật khẩu phải nhờ Admin/Đại diện HTX cấp mật khẩu tạm (AH-281).
- MailHog chỉ được thêm vào cấu hình triển khai của môi trường thử; không dùng ở môi trường có dữ liệu thật.
