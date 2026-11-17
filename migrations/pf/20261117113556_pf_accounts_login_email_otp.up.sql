-- AH-311 / ADR-0061 (quyết định 17/11/2026): Admin (platform_admin) đăng nhập hai bước ở màn riêng của App HTX — mật khẩu, rồi mã OTP
-- 6 số gửi qua email. Keycloak không cấp amr otp cho mã email, nên máy chủ tự ghi phiên đã qua mã.
--
--   login_otp_challenge  một mã đã gửi cho một tài khoản (user_ref = id Keycloak). Không lưu mã thô hay email: code_hash là
--                        HMAC-SHA256 có khóa máy chủ (AG_LOGIN_ID_KEY) của mã gắn với id thử thách. Hạn 5 phút
--                        (expires_at), sai 5 lần thì hủy (attempts, closed_at), dùng một lần (used_at); mã mới đóng mọi mã
--                        còn mở của tài khoản. Trần 10 mã/giờ/tài khoản và chờ 60 giây giữa hai mã đếm theo created_at
--                        (khóa advisory theo tài khoản ở tầng ứng dụng).
--   login_mfa_session    phiên Keycloak (claim sid) đã qua mã email. Xác thực token của Admin API và Application API tra bảng
--                        này khi token của vai đăng nhập bằng OTP email không có amr otp. Hết hạn sau 24 giờ (hạn tối đa
--                        của phiên Keycloak).
--
-- Không DELETE (R-25), không PII.

SET LOCAL lock_timeout = '5s';

CREATE TABLE pf_accounts.login_otp_challenge (
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
CREATE INDEX login_otp_challenge_user ON pf_accounts.login_otp_challenge (tenant_id, user_ref, created_at DESC);
SELECT shared.apply_tenant_rls('pf_accounts.login_otp_challenge');

CREATE TABLE pf_accounts.login_mfa_session (
  tenant_id    uuid        NOT NULL,
  session_id   text        NOT NULL CHECK (length(session_id) BETWEEN 1 AND 128),
  user_ref     uuid        NOT NULL,
  challenge_id uuid        NOT NULL,
  verified_at  timestamptz NOT NULL,
  expires_at   timestamptz NOT NULL,
  recorded_at  timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, session_id),
  FOREIGN KEY (tenant_id, challenge_id) REFERENCES pf_accounts.login_otp_challenge (tenant_id, id),
  CHECK (expires_at > verified_at)
);
SELECT shared.apply_tenant_rls('pf_accounts.login_mfa_session');

-- Quyền của r_accounts (vai nhóm của module, ADR-0028): không DELETE; UPDATE chỉ các cột trạng thái của mã.
GRANT SELECT, INSERT ON pf_accounts.login_otp_challenge TO r_accounts;
GRANT UPDATE (attempts, used_at, closed_at) ON pf_accounts.login_otp_challenge TO r_accounts;
GRANT SELECT, INSERT ON pf_accounts.login_mfa_session TO r_accounts;
