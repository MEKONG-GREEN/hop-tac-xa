# Đăng nhập riêng của App HTX (FN-01)

Hướng dẫn bật màn đăng nhập riêng của `apps/web-htx` ([ADR-0060](../adr/0060-man-dang-nhap-rieng-app-htx.md)): biến môi trường, cập nhật realm Keycloak đã nhập, tạo Admin đầu tiên. Admin đăng nhập bằng mật khẩu và mã OTP gửi qua email ([ADR-0061](../adr/0061-admin-dang-nhap-otp-email.md), mục 6).

## 1. Biến môi trường (`api-platform`)

Thiếu một biến thì route `POST /v1/public/auth/{login,refresh,logout}` trả `503 accounts.login_unavailable`; đặt thiếu một phần thì `api-platform` từ chối khởi động và nêu tên biến. Không có chế độ nới lỏng.

| Biến | Ý nghĩa |
|---|---|
| `AG_KC_ADMIN_URL`, `AG_KC_REALM` | cùng Keycloak với Admin REST của `pf/accounts` (đã có) |
| `AG_KC_LOGIN_CLIENT_ID`, `AG_KC_LOGIN_CLIENT_SECRET` | client `agrigreen-htx-login` — phát token thật, luồng có OTP |
| `AG_LOGIN_ID_KEY` | khóa HMAC của bộ đếm lần sai cho mã định danh không tồn tại: base64 chuẩn, ≥ 32 byte (`head -c 32 /dev/urandom \| base64`) |
| `AG_KC_QUICK_LOGIN_WAIT` | (tùy chọn) khóa nhanh của realm, mặc định `60s` = `minimumQuickLoginWaitSeconds` |
| `AG_SMTP_HOST`, `AG_SMTP_PORT`, `AG_SMTP_FROM`, `AG_SMTP_USERNAME`, `AG_SMTP_PASSWORD` | máy chủ thư gửi mã OTP đăng nhập của Admin (ADR-0061). Thiếu `AG_SMTP_HOST` thì Admin **không đăng nhập được** (`503 accounts.login_otp_unavailable`); các vai khác không bị ảnh hưởng. Mật khẩu SMTP để ở `deploy/compose/.env` (bí mật). Cổng 587 dùng STARTTLS; máy chủ thư không có TLS cần `AG_SMTP_ALLOW_PLAIN=true` (chỉ mạng nội bộ) |

`AG_KC_LOCKOUT_WAIT` (mặc định 15m) phải khớp `maxFailureWaitSeconds` của realm; test `TestDefaultLockoutWaitMatchesRealms` giữ các hằng khớp.

Docker Compose Local: chạy `scripts/gen-dev-secrets` để bổ sung hai bí mật vào `deploy/compose/.env`, rồi tạo lại container `keycloak` (nhập lại realm) và `api-platform`.

Web (`apps/web-htx`): `NEXT_PUBLIC_SUPPORT_NAME`, `NEXT_PUBLIC_SUPPORT_PHONE`, `NEXT_PUBLIC_SUPPORT_EMAIL` — hộp "Liên hệ quản trị viên" (QTPQ-Q-04). Thiếu bất kỳ giá trị nào thì ẩn liên kết.

## 2. Cập nhật realm đã nhập ở môi trường thật

Realm chỉ nhập một lần; môi trường đã chạy phải cập nhật tay (Admin Console hoặc `kcadm.sh`) cho khớp `deploy/compose/keycloak/agrigreen-realm.json`:

1. **Client** `agrigreen-htx-login`: bí mật (`client-secret`), chỉ bật Direct access grants (tắt Standard flow, Implicit flow, Service accounts); scope mặc định `basic, roles, web-origins, profile, agrigreen-claims`; `client.session.idle.timeout` 14400, `client.session.max.lifespan` 36000; dùng `directGrantFlow` của realm (`agrigreen direct grant`, có OTP khi đã cấu hình).
2. Đặt bí mật bằng đúng giá trị `AG_KC_LOGIN_CLIENT_SECRET` của `api-platform`.
3. Không tạo client hay flow chỉ kiểm mật khẩu (bản đầu của FN-01 có `agrigreen-htx-login-check`; đã bỏ vì xóa bộ đếm lần sai, xem ADR-0060). Nếu môi trường đã tạo thử, xóa client `agrigreen-htx-login-check` và flow `agrigreen htx password check`; không client nào ngoài `agrigreen-seed` (chỉ dev) và `agrigreen-htx-login` được bật Direct access grants.
4. Cập nhật mô tả của flow `agrigreen direct grant` (không còn "chỉ-dev"). Không đổi chính sách brute-force của realm (MULTIPLE, 5 lần, 15 phút, khóa nhanh 60 giây, cửa sổ 24 giờ).
5. **ADR-0061 (Admin dùng OTP email):**
   - *Realm roles* → `platform_admin` → tab *Associated roles*: gỡ `login_totp_required`.
   - *Realm settings* → *User profile* → thuộc tính `email` → *Who can edit*: chỉ `Admin` (bỏ `User`).
   - Với mỗi Admin hiện có (*Users*): đặt email nhận mã, bật *Email verified*, gỡ required action *Configure OTP*. Admin đã đăng ký TOTP thì xóa thông tin xác thực OTP ở tab *Credentials* (nếu không, Keycloak vẫn đòi TOTP ở bước 1).

`KC_HOSTNAME` phải cố định: token lấy qua kênh nội bộ (`http://keycloak:8080`) phải mang `iss` đúng bằng `AG_OIDC_ISSUER`, nếu không `internal/shared/authz` từ chối token. Kiểm nhanh: đăng nhập một tài khoản thử rồi gọi một API Application bằng `access_token` nhận được.

Realm `agrigreen-kn-admin` không đổi. App Flutter và các client OIDC khác vẫn đăng nhập qua trang Keycloak.

## 3. Tạo Admin đầu tiên (`cmd/bootstrap-admin`)

Không có màn hình tạo Admin đầu tiên. Cần `AG_KC_ADMIN_URL`, `AG_KC_REALM`, `AG_KC_CLIENT_ID`, `AG_KC_CLIENT_SECRET` (như `api-platform`):

```bash
go run ./cmd/bootstrap-admin -tenant <uuid tenant> -org <uuid đơn vị> -username <tên đăng nhập> -email <email nhận mã OTP> [-first-name ...] [-last-name ...]
```

- `-tenant`, `-org`, `-username`, `-email` bắt buộc, không có giá trị mặc định; thiếu hoặc sai thì lệnh dừng và không tạo gì. Tên đăng nhập hoặc email đã có thì dừng mà không đụng tài khoản đó.
- Tài khoản mang vai `platform_admin` (quản trị toàn hệ thống), email đánh dấu đã xác minh (Admin nhận mã OTP đăng nhập qua email này), ở trạng thái phải đổi mật khẩu ở lần đăng nhập đầu.
- **Mật khẩu tạm chỉ in ra stdout một lần.** Không ghi qua log, không ghi tệp. Chuyển cho người nhận qua kênh an toàn; không dán vào ticket hay tài liệu. Mất thì đặt lại bằng "Quên mật khẩu?" (cần email đã xác minh) hoặc xóa tài khoản trong Keycloak rồi chạy lại lệnh.
- Image: `docker build --build-arg CMD=bootstrap-admin .` (Dockerfile không đổi).

Lần đầu đăng nhập ở màn mới, tài khoản nhận `accounts.login_setup_required`; bấm "Hoàn tất trên trang xác thực" để đổi mật khẩu trên trang Keycloak. Sau đó đăng nhập lại ở màn mới: nhập mật khẩu mới, rồi nhập mã OTP 6 số gửi tới email của tài khoản (mục 6).

## 4. Vận hành

- Khóa tạm 15 phút (hoặc 60 giây khi hai lần sai cách nhau dưới 1 giây) là khóa của Keycloak, **không** hiện trong danh sách tài khoản; Admin mở sớm bằng nút "Mở khóa" ở chi tiết tài khoản. Đặt lại mật khẩu thành công cũng xóa bộ đếm.
- Mã định danh không tồn tại có bộ đếm ở bảng `pf_accounts.login_probe` (chỉ HMAC); dòng đã hết khóa và quá 24 giờ được dọn dần. Đổi `AG_LOGIN_ID_KEY` làm bộ đếm cũ vô nghĩa (an toàn: về 0).
- Nhật ký: mỗi lượt một dòng `event=accounts.login` với `outcome` (`authenticated`, `failed`, `temp_locked`, `account_locked`, `setup_required`, `unknown_identifier`, `ambiguous_phone` — cảnh báo SĐT khớp nhiều tài khoản, chỉ kèm `id_hash`). Không có mã định danh, mật khẩu, OTP hay token.

## 5. Khóa tài khoản trong lúc đang dùng (FN-01/EX-06, AH-301)

- Khóa có hiệu lực ở **yêu cầu kế tiếp**, không chờ token hết hạn: middleware của pf/accounts tra `pf_accounts.account` ở mỗi yêu cầu đã đăng nhập (cả Application lẫn Admin API) và trả 403 `accounts.account_locked` kèm câu EX-03. Web xóa phiên và về màn đăng nhập; app về màn đăng nhập ở lượt đồng bộ kế tiếp (chậm nhất 2 phút), dữ liệu chờ gửi giữ nguyên.
- CSDL không đọc được thì **mọi** API cần đăng nhập trả 503 `accounts.dependency_unavailable`, kể cả yêu cầu đọc (fail-closed). Keycloak không nằm trên đường mỗi yêu cầu: Keycloak sập không làm API hỏng.
- Tài khoản chỉ bị tắt trong console Keycloak mà pf chưa ghi thì **không** bị lớp này chặn; token đã phát dùng được tới khi hết hạn. Muốn chặn ngay thì khóa bằng nút "Khóa" của ứng dụng.

## 6. Admin đăng nhập bằng mật khẩu và mã OTP email (ADR-0061)

- **Hai bước trên cùng màn:** nhập tên đăng nhập và mật khẩu → máy chủ gửi mã 6 số tới email đã xác minh của tài khoản, màn chuyển sang ô nhập mã (hiện email đã che, vd `a***@mekonggreen.vn`) → nhập mã → vào ứng dụng. Chỉ vai `platform_admin`; vai khác đăng nhập một bước như cũ.
- **Mã:** hiệu lực 5 phút, dùng một lần, sai 5 lần thì hủy. Nút "Gửi lại mã" mở sau 60 giây; mã mới làm mã cũ hết dùng. Tối đa 10 mã mỗi giờ cho một tài khoản (`429 accounts.login_otp_rate_limited`).
- **Phiên đã qua mã** ghi ở `pf_accounts.login_mfa_session` (theo claim `sid` của token, hạn 24 giờ). Admin API và Application API nhận token Admin khi có `amr=otp` hoặc phiên đã ghi; token Admin chỉ có mật khẩu (vd đăng nhập ở trang Keycloak) bị trả `403 mfa_required`.
- **Không nhận được mã:** kiểm `AG_SMTP_*` của `api-platform` (log khởi động có cảnh báo "chưa cấu hình SMTP"), hộp thư rác, và email của tài khoản trong Keycloak (phải bật *Email verified*). Tài khoản chưa có email đã xác minh nhận `403 accounts.login_otp_email_missing`.
- **Nhật ký:** `outcome` thêm `otp_sent`, `otp_resend_wait`, `otp_send_failed`, `otp_unavailable`, `otp_email_missing`, `otp_rate_limited`, `otp_invalid`, `otp_exhausted`, `otp_expired`, `authenticated_email_otp`. Không có mã, email hay mật khẩu trong log.
- **Đổi email nhận mã:** chỉ quản trị đổi được (Keycloak Admin Console hoặc màn quản trị tài khoản); người dùng không tự đổi trong trang tài khoản Keycloak.
