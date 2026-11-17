# ADR-0061: Admin đăng nhập bằng mật khẩu và mã OTP gửi qua email

**Trạng thái:** Chấp nhận. **Ngày:** 17/11/2026 (AH-311). **Nguồn:** yêu cầu của người phụ trách dự án ngày 17/11/2026 ("login cho admin: nhập password và mã OTP được gửi đến mailserver") và các lựa chọn đã chốt cùng ngày: làm trong API, chỉ Admin dùng OTP email (vai khác chỉ cần mật khẩu), mã 6 số hiệu lực 5 phút, sai 5 lần thì hủy, gửi lại sau 60 giây, máy chủ ghi phiên đã qua mã để hai bề mặt API chấp nhận.

**Liên quan:** [ADR-0060](0060-man-dang-nhap-rieng-app-htx.md) (màn đăng nhập riêng — ADR này thay yếu tố thứ hai của Admin), [ADR-0041](0041-mfa-theo-nhom-d24.md) (TOTP theo nhóm vai D24), [ADR-0057](0057-tu-dat-lai-mat-khau-qua-email.md) (mẫu mã một lần qua email), [runbook đăng nhập App HTX](../runbooks/dang-nhap-app-htx.md).

## Bối cảnh

Theo ADR-0041 và ADR-0060, Admin (`platform_admin`) phải đăng ký TOTP bằng ứng dụng xác thực và nhập mã cùng mật khẩu trong một bước. Người phụ trách dự án muốn Admin nhận mã OTP qua email thay cho ứng dụng TOTP.

Keycloak 26.3 (bản đang chạy) không có bộ xác thực OTP qua email. Luồng direct grant mà màn đăng nhập riêng dùng cũng không hỏi tương tác được: mã phải được gửi trước khi người dùng có thể nhập. Admin API kiểm yếu tố thứ hai bằng claim `amr` của token Keycloak, mà Keycloak chỉ đặt `amr=otp` khi chính nó kiểm mã.

## Quyết định

1. **Vai đăng nhập bằng OTP email:** `accounts/port.EmailOTPRoles = [platform_admin]`. Trong realm, vai này không còn chứa vai đánh dấu `login_totp_required`; `LoginTOTPRoles` và danh mục vai gốc bỏ vai này nên tạo hoặc cấp vai không gắn `CONFIGURE_TOTP`. Các vai khác không đổi: vai HTX (`coop_manager`, `htx_member`) chỉ cần mật khẩu như trước; vai TOTP cũ của hệ KN giữ D24.
2. **Đăng nhập hai bước ở `POST /v1/public/auth/login`** (chỉ áp cho vai trên):
   - **Bước 1** (`identifier`, `password`): Keycloak kiểm mật khẩu như mọi tài khoản, kể cả bộ đếm lần sai. Đúng thì máy chủ thu hồi ngay phiên vừa mở (không trả token), sinh mã 6 số bằng `crypto/rand`, ghi thử thách vào `pf_accounts.login_otp_challenge` trong giao dịch ngắn, gửi email **ngoài giao dịch**, rồi trả `200 {status: otp_required, challenge_id, email_hint, expires_in, resend_after}`.
   - **Bước 2** (`identifier`, `password`, `challenge_id`, `otp`): web gửi lại mật khẩu đang giữ trong bộ nhớ trang, nên máy chủ không phải giữ token tạm. Keycloak kiểm lại mật khẩu, rồi máy chủ kiểm mã. Đúng thì trong cùng giao dịch máy chủ đánh dấu mã đã dùng và ghi phiên Keycloak (claim `sid`) vào `pf_accounts.login_mfa_session` (hạn 24 giờ, bằng hạn tối đa của phiên), rồi trả token. Sai thì thu hồi phiên và tính một lần sai của mã.
3. **Chính sách mã:**
   - Hiệu lực 5 phút; sai 5 lần thì hủy; dùng một lần.
   - Có mã mới thì mọi mã cũ hết dùng. Gọi lại bước 1 trong 60 giây thì trả lại thử thách cũ, không gửi thêm email.
   - Tối đa 10 mã mỗi giờ cho một tài khoản (đếm trong DB, khóa advisory theo tài khoản).
   - DB chỉ lưu HMAC-SHA256 của mã, có khóa máy chủ (`AG_LOGIN_ID_KEY`) và gắn với id thử thách. DB lộ ra cũng không dò ngược được mã 6 số.
4. **Admin API và Application API nhận phiên đã qua mã.**
   - `httpx.AuthenticateAttested` hỏi `pf/accounts` (`SecondFactorVerified`) khi token không có yếu tố thứ hai trong `amr`; phiên đã ghi và còn hạn thì đặt `Principal.MFA = true`.
   - Admin API giữ `RequireMFA`. Application API thêm `RequireMFAForRoles(EmailOTPRoles...)`, nên token Admin chỉ có mật khẩu (ví dụ lấy qua trang đăng nhập Keycloak, vì vai không còn bắt TOTP ở đó) bị chặn ở cả hai bề mặt.
   - Tra cứu là một giao dịch chỉ đọc theo khóa chính và chỉ chạy cho vai OTP email. Lỗi tra cứu trả 503, không cho qua.
5. **Email nhận mã phải tin được.**
   - Chỉ gửi tới email **đã xác minh** của tài khoản. `cmd/bootstrap-admin` bắt buộc `-email` và đánh dấu đã xác minh: người chạy lệnh là quản trị hệ thống.
   - Hồ sơ người dùng của realm chỉ cho quản trị sửa email. Nếu người dùng tự đổi được email trong trang tài khoản Keycloak thì ai có mật khẩu cũng tự nhận được mã.
6. **Fail-closed.**
   - api-platform chỉ nhận kênh SMTP thật (`AG_SMTP_*`). Thiếu thì Admin không đăng nhập được (`503 accounts.login_otp_unavailable`), không bao giờ báo "đã gửi" giả. Chế độ `AG_MAILER=log` không dùng vì sẽ in mã ra log.
   - Không có email đã xác minh → `403 accounts.login_otp_email_missing`. Gửi lỗi → hủy mã vừa tạo và trả 503.
7. **Không có mã, mật khẩu, token hay email trong log hoặc lỗi.** Log chỉ có `user_ref`, `challenge_id` và `outcome`; màn hình chỉ thấy email đã che (`a***@tên-miền`).

## Đối chiếu với lý do ADR-0060 bỏ thiết kế hai bước

ADR-0060 bỏ đường "kiểm mật khẩu không kèm yếu tố thứ hai", vì mỗi lần kiểm đúng mật khẩu Keycloak lại xóa bộ đếm lần sai, trong khi lượt đoán TOTP ở trang Keycloak được đếm chung bộ đếm đó. Ở đây:

- Tài khoản dùng TOTP vẫn đăng nhập một bước như cũ: chỉ vai OTP email mới đi bước 1.
- Vai OTP email không còn TOTP trong Keycloak, nên trang Keycloak không có mã nào để đoán. Phiên lấy qua trang đó không có `amr=otp` và bị cả hai API chặn.
- Lượt đoán mã email do máy chủ tự đếm theo thử thách (5 lần) và theo tài khoản (10 mã mỗi giờ), không dựa vào bộ đếm của Keycloak.

## Đánh đổi

- **Người đã biết mật khẩu đoán được tối đa 50 mã mỗi giờ** (10 thử thách × 5 lần) trên 1 000 000 khả năng: khoảng 0,005% mỗi giờ, 0,12% mỗi ngày. Mỗi thử thách gửi một email cho chủ tài khoản, nên bị dò là thấy ngay. Muốn chặt hơn thì giảm trần mỗi giờ hoặc thêm trần mỗi ngày.
- **Email yếu hơn ứng dụng TOTP:** ai chiếm được hộp thư và biết mật khẩu thì vào được. Đây là đánh đổi người phụ trách dự án đã chọn để Admin không phải cài ứng dụng.
- **Mỗi request của Admin tốn thêm một lần đọc DB** khi token không có `amr=otp`. Vai khác không bị ảnh hưởng.
- **Thời gian phản hồi bước 1 gồm thời gian gửi SMTP** (vài giây). Đổi lại, màn hình chỉ báo "đã gửi" khi máy chủ thư đã nhận thư, không qua outbox như ADR-0057.
- **Web giữ mật khẩu trong bộ nhớ trang tới khi xong bước 2** (tối đa vài phút), không ghi vào storage. Đổi lại máy chủ không giữ token hay refresh token tạm.
- **Kho định danh (`identity-vault`) vẫn chỉ kiểm `amr`** cho ngăn dữ liệu nhạy cảm (`AG_VAULT_REQUIRE_MFA`, mặc định bật ở production). Admin đăng nhập bằng OTP email chưa đọc được ngăn nhạy cảm ở production; khi cần thì cho kho tra phiên đã ghi qua API nội bộ (việc riêng).
- **Môi trường đã nhập realm** (realm chỉ nhập ở lần khởi động đầu) phải tự làm các bước theo runbook: bỏ `login_totp_required` khỏi vai `platform_admin`, chỉ cho quản trị sửa email, gỡ `CONFIGURE_TOTP` và đặt email đã xác minh cho Admin hiện có.

## Ngoài phạm vi

OTP email cho vai khác; OTP qua SMS hoặc Zalo; đăng nhập Admin trên app Flutter (app dùng trang Keycloak); màn đổi mật khẩu lần đầu (AH-269); cho kho định danh nhận phiên đã qua OTP email.
