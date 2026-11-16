# Đăng nhập riêng của App HTX (FN-01)

Hướng dẫn bật màn đăng nhập riêng của `apps/web-htx` ([ADR-0060](../adr/0060-man-dang-nhap-rieng-app-htx.md)): biến môi trường, cập nhật realm Keycloak đã nhập, tạo Admin đầu tiên.

## 1. Biến môi trường (`api-platform`)

Thiếu một biến thì route `POST /v1/public/auth/{login,refresh,logout}` trả `503 accounts.login_unavailable`; đặt thiếu một phần thì `api-platform` từ chối khởi động và nêu tên biến. Không có chế độ nới lỏng.

| Biến | Ý nghĩa |
|---|---|
| `AG_KC_ADMIN_URL`, `AG_KC_REALM` | cùng Keycloak với Admin REST của `pf/accounts` (đã có) |
| `AG_KC_LOGIN_CLIENT_ID`, `AG_KC_LOGIN_CLIENT_SECRET` | client `agrigreen-htx-login` — phát token thật, luồng có OTP |
| `AG_KC_LOGIN_CHECK_CLIENT_ID`, `AG_KC_LOGIN_CHECK_CLIENT_SECRET` | client `agrigreen-htx-login-check` — chỉ kiểm mật khẩu (token không dùng được) |
| `AG_LOGIN_ID_KEY` | khóa HMAC của bộ đếm lần sai cho mã định danh không tồn tại: base64 chuẩn, ≥ 32 byte (`head -c 32 /dev/urandom \| base64`) |
| `AG_KC_QUICK_LOGIN_WAIT` | (tùy chọn) khóa nhanh của realm, mặc định `60s` = `minimumQuickLoginWaitSeconds` |

`AG_KC_LOCKOUT_WAIT` (mặc định 15m) phải khớp `maxFailureWaitSeconds` của realm; test `TestDefaultLockoutWaitMatchesRealms` giữ các hằng khớp.

Docker Compose Local: chạy `scripts/gen-dev-secrets` để bổ sung ba bí mật vào `deploy/compose/.env`, rồi tạo lại container `keycloak` (nhập lại realm) và `api-platform`.

Web (`apps/web-htx`): `NEXT_PUBLIC_SUPPORT_NAME`, `NEXT_PUBLIC_SUPPORT_PHONE`, `NEXT_PUBLIC_SUPPORT_EMAIL` — hộp "Liên hệ quản trị viên" (QTPQ-Q-04). Thiếu bất kỳ giá trị nào thì ẩn liên kết.

## 2. Cập nhật realm đã nhập ở môi trường thật

Realm chỉ nhập một lần; môi trường đã chạy phải cập nhật tay (Admin Console hoặc `kcadm.sh`) cho khớp `deploy/compose/keycloak/agrigreen-realm.json`:

1. **Flow** `agrigreen htx password check` (top level, `basic-flow`): `direct-grant-validate-username` REQUIRED, rồi `direct-grant-validate-password` REQUIRED (cấu hình AMR `agrigreen-amr-pwd`). Không có nhánh OTP.
2. **Client** `agrigreen-htx-login`: bí mật (`client-secret`), chỉ bật Direct access grants (tắt Standard flow, Implicit flow, Service accounts); scope mặc định `basic, roles, web-origins, profile, agrigreen-claims`; `client.session.idle.timeout` 14400, `client.session.max.lifespan` 36000; dùng `directGrantFlow` của realm (`agrigreen direct grant`, có OTP khi đã cấu hình).
3. **Client** `agrigreen-htx-login-check`: bí mật, chỉ bật Direct access grants; ghi đè luồng Direct grant bằng flow ở bước 1; scope mặc định chỉ `basic`, không scope tùy chọn; tắt Full scope allowed — token không có `aud`, vai hay tenant nên API không nhận.
4. Đặt hai bí mật bằng đúng giá trị `AG_KC_LOGIN_CLIENT_SECRET`, `AG_KC_LOGIN_CHECK_CLIENT_SECRET` của `api-platform`.
5. Cập nhật mô tả của flow `agrigreen direct grant` (không còn "chỉ-dev"). Không đổi chính sách brute-force của realm (MULTIPLE, 5 lần, 15 phút, khóa nhanh 60 giây, cửa sổ 24 giờ).

`KC_HOSTNAME` phải cố định: token lấy qua kênh nội bộ (`http://keycloak:8080`) phải mang `iss` đúng bằng `AG_OIDC_ISSUER`, nếu không `internal/shared/authz` từ chối token. Kiểm nhanh: đăng nhập một tài khoản thử rồi gọi một API Application bằng `access_token` nhận được.

Realm `agrigreen-kn-admin` không đổi. App Flutter và các client OIDC khác vẫn đăng nhập qua trang Keycloak.

## 3. Tạo Admin đầu tiên (`cmd/bootstrap-admin`)

Không có màn hình tạo Admin đầu tiên. Cần `AG_KC_ADMIN_URL`, `AG_KC_REALM`, `AG_KC_CLIENT_ID`, `AG_KC_CLIENT_SECRET` (như `api-platform`):

```bash
go run ./cmd/bootstrap-admin -tenant <uuid tenant> -org <uuid đơn vị> -username <tên đăng nhập> [-email ...] [-first-name ...] [-last-name ...]
```

- `-tenant`, `-org`, `-username` bắt buộc, không có giá trị mặc định; thiếu hoặc sai UUID thì lệnh dừng và không tạo gì. Tên đăng nhập đã có thì dừng mà không đụng tài khoản đó.
- Tài khoản mang vai `platform_admin` (quản trị toàn hệ thống, bắt buộc TOTP), ở trạng thái phải đổi mật khẩu và đăng ký TOTP ở lần đăng nhập đầu.
- **Mật khẩu tạm chỉ in ra stdout một lần.** Không ghi qua log, không ghi tệp. Chuyển cho người nhận qua kênh an toàn; không dán vào ticket hay tài liệu. Mất thì đặt lại bằng "Quên mật khẩu?" (cần email đã xác minh) hoặc xóa tài khoản trong Keycloak rồi chạy lại lệnh.
- Image: `docker build --build-arg CMD=bootstrap-admin .` (Dockerfile không đổi).

Lần đầu đăng nhập ở màn mới, tài khoản nhận `accounts.login_setup_required`; bấm "Hoàn tất trên trang xác thực" để đổi mật khẩu và đăng ký OTP trên trang Keycloak. Sau đó đăng nhập lại ở màn mới: được hỏi OTP rồi vào trang đầu tiên được xem.

## 4. Vận hành

- Khóa tạm 15 phút (hoặc 60 giây khi hai lần sai cách nhau dưới 1 giây) là khóa của Keycloak, **không** hiện trong danh sách tài khoản; Admin mở sớm bằng nút "Mở khóa" ở chi tiết tài khoản. Đặt lại mật khẩu thành công cũng xóa bộ đếm.
- Mã định danh không tồn tại có bộ đếm ở bảng `pf_accounts.login_probe` (chỉ HMAC); dòng đã hết khóa và quá 24 giờ được dọn dần. Đổi `AG_LOGIN_ID_KEY` làm bộ đếm cũ vô nghĩa (an toàn: về 0).
- Nhật ký: mỗi lượt một dòng `event=accounts.login` với `outcome` (`authenticated`, `otp_required`, `failed`, `temp_locked`, `account_locked`, `setup_required`, `unknown_identifier`, `ambiguous_phone` — cảnh báo SĐT khớp nhiều tài khoản, chỉ kèm `id_hash`). Không có mã định danh, mật khẩu, OTP hay token.
