-- AH-280 (FN-10, TECH-03; ADR-0063): quên mật khẩu, đặt lại bằng mã OTP gửi qua SMS tới số điện thoại của tài khoản
-- realm HTX.
--
--   password_otp_challenge  một mã đã gửi cho một tài khoản (user_ref = id Keycloak). Không lưu mã thô hay số điện
--                           thoại: code_hash là HMAC-SHA256 có khóa máy chủ (AG_LOGIN_ID_KEY) của mã gắn với id thử
--                           thách. Hạn 5 phút (expires_at), sai 5 lần thì hủy (attempts, closed_at), dùng một lần
--                           (used_at); mã mới đóng mọi mã còn mở của tài khoản. Chờ 60 giây giữa hai mã và tối đa 5 mã
--                           trong 24 giờ đếm theo created_at (khóa advisory theo tài khoản ở tầng ứng dụng). Lần gửi do
--                           Admin/Đại diện HTX bấm hộ (AH-281) cũng ghi vào bảng này nên cùng bị đếm.
--
-- Không DELETE (R-25), không PII.

SET LOCAL lock_timeout = '5s';

CREATE TABLE pf_accounts.password_otp_challenge (
  tenant_id   uuid        NOT NULL,
  id          uuid        NOT NULL,                    -- UUIDv7
  user_ref    uuid        NOT NULL,
  code_hash   bytea       NOT NULL CHECK (octet_length(code_hash) = 32),
  created_at  timestamptz NOT NULL,
  expires_at  timestamptz NOT NULL,
  attempts    smallint    NOT NULL DEFAULT 0 CHECK (attempts BETWEEN 0 AND 5),
  used_at     timestamptz NULL,
  closed_at   timestamptz NULL,
  recorded_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, id),
  CHECK (expires_at > created_at),
  CHECK (used_at IS NULL OR closed_at IS NULL)
);
CREATE INDEX password_otp_challenge_user ON pf_accounts.password_otp_challenge (tenant_id, user_ref, created_at DESC);
SELECT shared.apply_tenant_rls('pf_accounts.password_otp_challenge');

-- Quyền của r_accounts (vai nhóm của module, ADR-0028): không DELETE; UPDATE chỉ các cột trạng thái của mã.
GRANT SELECT, INSERT ON pf_accounts.password_otp_challenge TO r_accounts;
GRANT UPDATE (attempts, used_at, closed_at) ON pf_accounts.password_otp_challenge TO r_accounts;
