-- AH-99 KH-16: camera trực tiếp trên Web cần tải ảnh mã hóa từ thiết bị
-- đăng ký. Web chỉ dùng nhật ký hộ; không mở đồng bộ hiện trường/Zalo.
SET LOCAL lock_timeout = '5s';
ALTER TABLE pf_sync.device DROP CONSTRAINT device_platform_check;
ALTER TABLE pf_sync.device ADD CONSTRAINT device_platform_check
 CHECK (platform IN ('android', 'ios', 'web')) NOT VALID;
ALTER TABLE pf_sync.device VALIDATE CONSTRAINT device_platform_check;
