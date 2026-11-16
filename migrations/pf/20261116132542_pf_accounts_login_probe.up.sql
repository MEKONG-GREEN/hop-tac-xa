-- AH-268 (FN-01 Đăng nhập) / ADR-0060, quyết định nhóm dự án ngày 16/11/2026: bộ đếm lần đăng nhập sai cho mã định danh
-- (tên đăng nhập hoặc SĐT) KHÔNG khớp tài khoản nào. Tài khoản có thật dùng bộ đếm của Keycloak; mã không tồn tại phải cho
-- phản hồi y hệt (cùng thông báo, cùng ngưỡng 5 lần, cùng khóa 15 phút, khóa nhanh 60 giây, cửa sổ 24 giờ) để không dò
-- được tài khoản nào có thật.
--
-- Tên đăng nhập và SĐT là dữ liệu cá nhân (quy tắc 7): bảng chỉ lưu HMAC-SHA256 có khóa bí mật của mã đã chuẩn hóa
-- (key_hash, 32 byte), không lưu mã ở dạng đọc được. Bảng không có tenant (mã chưa gắn tài khoản nên chưa biết tenant) nên
-- ngoại lệ ag:no-rls, theo mẫu shared.chain_head: vai ứng dụng không có quyền bảng, chỉ gọi hai hàm SECURITY DEFINER.
-- Forward-only; dòng đã hết khóa và quá cửa sổ 24 giờ được dọn dần ở mỗi lần gọi login_probe_lock.
SET LOCAL lock_timeout = '5s';

CREATE TABLE pf_accounts.login_probe (
  key_hash        bytea       PRIMARY KEY CHECK (octet_length(key_hash) = 32),
  failures        int         NOT NULL CHECK (failures >= 0),
  last_failure_at timestamptz NULL,
  locked_until    timestamptz NULL,
  recorded_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX login_probe_last_failure ON pf_accounts.login_probe (last_failure_at);
COMMENT ON TABLE pf_accounts.login_probe IS 'ag:no-rls không có tenant: bộ đếm đăng nhập sai cho mã định danh không khớp tài khoản (FN-01), chỉ lưu HMAC, chỉ truy cập qua pf_accounts.login_probe_lock/login_probe_save (SECURITY DEFINER)';
REVOKE ALL ON pf_accounts.login_probe FROM PUBLIC;

-- Khóa dòng của mã định danh (tạo dòng rỗng nếu chưa có) và trả trạng thái hiện tại. Gọi trong giao dịch ngắn, không I/O mạng.
CREATE FUNCTION pf_accounts.login_probe_lock(p_key bytea)
RETURNS TABLE (failures int, last_failure_at timestamptz, locked_until timestamptz)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, pf_accounts
AS $$
BEGIN
  IF p_key IS NULL OR octet_length(p_key) <> 32 THEN
    RAISE EXCEPTION 'login_probe_lock: khóa phải là 32 byte' USING ERRCODE = 'check_violation';
  END IF;
  INSERT INTO pf_accounts.login_probe (key_hash, failures) VALUES (p_key, 0) ON CONFLICT (key_hash) DO NOTHING;
  DELETE FROM pf_accounts.login_probe d
   WHERE d.key_hash IN (
     SELECT p.key_hash FROM pf_accounts.login_probe p
      WHERE p.key_hash <> p_key
        AND p.last_failure_at < now() - interval '24 hours'
        AND (p.locked_until IS NULL OR p.locked_until < now())
      LIMIT 100);
  RETURN QUERY
    SELECT p.failures, p.last_failure_at, p.locked_until
      FROM pf_accounts.login_probe p WHERE p.key_hash = p_key FOR UPDATE;
END;
$$;

CREATE FUNCTION pf_accounts.login_probe_save(p_key bytea, p_failures int, p_last timestamptz, p_locked_until timestamptz)
RETURNS void
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, pf_accounts
AS $$
BEGIN
  UPDATE pf_accounts.login_probe
     SET failures = p_failures, last_failure_at = p_last, locked_until = p_locked_until
   WHERE key_hash = p_key;
END;
$$;

REVOKE EXECUTE ON FUNCTION pf_accounts.login_probe_lock(bytea) FROM PUBLIC;
REVOKE EXECUTE ON FUNCTION pf_accounts.login_probe_save(bytea, int, timestamptz, timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION pf_accounts.login_probe_lock(bytea) TO r_accounts;
GRANT EXECUTE ON FUNCTION pf_accounts.login_probe_save(bytea, int, timestamptz, timestamptz) TO r_accounts;
