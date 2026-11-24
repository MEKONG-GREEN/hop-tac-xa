-- AH-224 (FN-TT-03, TECH-TP-03, DATA-TP-06; ADR-0065): module pf/weather — kết nối API thời tiết qua máy chủ.
--
--   provider_config  SỔ chỉ ghi thêm cấu hình kết nối của tenant (mỗi lần Admin lưu là một phiên bản; phiên bản mới
--                    nhất có hiệu lực từ lần lấy kế tiếp — FN-TT-03/AC-01). Khóa API lưu MÃ HÓA AES-256-GCM bằng khóa
--                    máy chủ (AG_WEATHER_SECRET_KEY, AAD = tenant_id) — api_key_cipher, api_key_nonce; chỉ giữ 4 ký tự
--                    cuối để hiển thị (BR-03, AC-03). Không có bảng nào khác chứa khóa.
--   location         điểm lấy số liệu: tọa độ đã làm thô theo lưới 0,01° (≈ 1,1 km); vị trí cách điểm có sẵn dưới 1 km
--                    dùng chung điểm đó (BR-02). Chỉ tọa độ, không mã HTX, không dữ liệu cá nhân.
--   forecast         bản dự báo gần nhất của mỗi điểm (đệm máy chủ — TECH-TP-03, FN-TT-01/BR-01), ghi đè mỗi lần lấy.
--   daily            số liệu theo NGÀY và điểm để hiện lại ngày đã qua (DATA-TP-06, FN-TT-03/BR-05): kind 'observed'
--                    (thực đo, nếu nhà cung cấp có) hoặc 'final_forecast' (bản dự báo lấy lần cuối trước 23:30 giờ Việt
--                    Nam của ngày đó). Giữ tối thiểu 24 tháng: không có lệnh DELETE, r_weather không có quyền DELETE.
--   fetch_state      trạng thái lấy số liệu của tenant: lần thử, lần thành công, số lần lỗi liên tiếp (EX-02: đến 3 thì
--                    phát pf.weather.fetch.failing đúng một lần — notified), giãn tần suất khi vượt hạn mức (EX-03:
--                    backoff_until = 00:00 hôm sau).
--
-- RLS theo tenant cho mọi bảng. Không UPDATE/DELETE sổ (R-25); bảng đệm/trạng thái chỉ UPDATE cột của chính nó.

SET LOCAL lock_timeout = '5s';

DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'r_weather' AND NOT rolcanlogin) THEN
    RAISE EXCEPTION 'thiếu vai nhóm r_weather (NOLOGIN) — chạy cmd/migrate bootstrap trước migrate up (ADR-0028)';
  END IF;
END
$$;

CREATE SCHEMA pf_weather;
REVOKE ALL ON SCHEMA pf_weather FROM PUBLIC;

CREATE TABLE pf_weather.provider_config (
  id                uuid        NOT NULL,                 -- UUIDv7 (R-11)
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'weather-config'
  seq               bigint      NOT NULL,
  provider          text        NOT NULL CHECK (provider IN ('open_meteo', 'fake')),
  refresh_minutes   integer     NOT NULL CHECK (refresh_minutes IN (30, 60, 120, 180, 360)),
  forecast_days     integer     NOT NULL CHECK (forecast_days BETWEEN 1 AND 14),
  api_key_cipher    bytea       NULL,
  api_key_nonce     bytea       NULL CHECK (api_key_nonce IS NULL OR octet_length(api_key_nonce) = 12),
  api_key_last4     text        NULL CHECK (api_key_last4 IS NULL OR char_length(api_key_last4) BETWEEN 1 AND 4),
  event_time        timestamptz NOT NULL,                 -- R-13: giờ máy chủ
  recorded_at       timestamptz NOT NULL DEFAULT now(),
  supersedes_id     uuid        NULL,
  correction_reason text        NULL,
  actor_ref         uuid        NOT NULL,
  device_id         text        NULL,
  idem_key          text        NOT NULL,
  rule_version      text        NOT NULL,
  prev_hash         bytea       NOT NULL,
  row_hash          bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (correction_reason IS NULL OR length(correction_reason) >= 10),
  CHECK ((api_key_cipher IS NULL) = (api_key_nonce IS NULL) AND (api_key_cipher IS NULL) = (api_key_last4 IS NULL))
) PARTITION BY RANGE (recorded_at);
CREATE INDEX provider_config_stream ON pf_weather.provider_config (tenant_id, stream_key, seq);
CREATE INDEX provider_config_idem   ON pf_weather.provider_config (tenant_id, idem_key);
SELECT shared.apply_ledger('pf_weather.provider_config', 'pf_weather.provider_config');

CREATE TABLE pf_weather.location (
  tenant_id   uuid         NOT NULL,
  id          uuid         NOT NULL,                     -- UUIDv7
  lat         numeric(7,2) NOT NULL CHECK (lat BETWEEN -90 AND 90),
  lon         numeric(7,2) NOT NULL CHECK (lon BETWEEN -180 AND 180),
  created_at  timestamptz  NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, id),
  UNIQUE (tenant_id, lat, lon)
);
SELECT shared.apply_tenant_rls('pf_weather.location');

CREATE TABLE pf_weather.forecast (
  tenant_id   uuid        NOT NULL,
  location_id uuid        NOT NULL,
  provider    text        NOT NULL CHECK (provider IN ('open_meteo', 'fake')),
  fetched_at  timestamptz NOT NULL,
  payload     jsonb       NOT NULL,
  PRIMARY KEY (tenant_id, location_id),
  FOREIGN KEY (tenant_id, location_id) REFERENCES pf_weather.location (tenant_id, id)
);
SELECT shared.apply_tenant_rls('pf_weather.forecast');

CREATE TABLE pf_weather.daily (
  tenant_id   uuid        NOT NULL,
  location_id uuid        NOT NULL,
  day         date        NOT NULL,                       -- ngày theo giờ Việt Nam
  kind        text        NOT NULL CHECK (kind IN ('observed', 'final_forecast')),
  provider    text        NOT NULL CHECK (provider IN ('open_meteo', 'fake')),
  fetched_at  timestamptz NOT NULL,
  summary     jsonb       NOT NULL,
  hourly      jsonb       NULL,                           -- NULL: nhà cung cấp không có số liệu theo giờ (FN-TT-02/EX-01)
  PRIMARY KEY (tenant_id, location_id, day),
  FOREIGN KEY (tenant_id, location_id) REFERENCES pf_weather.location (tenant_id, id)
);
SELECT shared.apply_tenant_rls('pf_weather.daily');

CREATE TABLE pf_weather.fetch_state (
  tenant_id            uuid        NOT NULL PRIMARY KEY,
  last_attempt_at      timestamptz NULL,
  last_success_at      timestamptz NULL,
  consecutive_failures integer     NOT NULL DEFAULT 0 CHECK (consecutive_failures >= 0),
  last_error           text        NULL CHECK (last_error IS NULL OR last_error ~ '^[a-z_]{1,40}$'),
  backoff_until        timestamptz NULL,
  notified             boolean     NOT NULL DEFAULT false
);
SELECT shared.apply_tenant_rls('pf_weather.fetch_state');

GRANT USAGE ON SCHEMA pf_weather TO r_weather;
GRANT SELECT, INSERT ON pf_weather.provider_config, pf_weather.location TO r_weather;
GRANT SELECT, INSERT, UPDATE ON pf_weather.forecast, pf_weather.daily, pf_weather.fetch_state TO r_weather;

-- k_ref như mọi module pf sinh sau 000092 (R-DB-05).
GRANT USAGE ON SCHEMA k_ref TO r_weather;
GRANT EXECUTE ON FUNCTION k_ref.ctx_reject(text), k_ref.ctx_text(text), k_ref.ctx_uuid(text) TO r_weather;
GRANT SELECT ON k_ref.org, k_ref.node_closure, k_ref.admin_unit, k_ref.share_grant_active, k_ref.share_scope TO r_weather;

-- Job weather_fetch ở worker tìm tenant đã cấu hình bằng ag_ops_jobs (chỉ cột tenant_id — mẫu pf 000114); việc thật
-- chạy bằng r_weather trong ngữ cảnh từng tenant (R-05).
GRANT USAGE ON SCHEMA pf_weather TO ag_ops_jobs;
GRANT SELECT (tenant_id) ON pf_weather.provider_config TO ag_ops_jobs;
