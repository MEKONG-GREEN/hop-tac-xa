-- AH-23 — Ghi nhận đồng ý thu thập ảnh GPS và dữ liệu sản xuất (REQ-011, KH-24; NĐ 13/2023/NĐ-CP).
--
-- pf_accounts.data_consent: SỔ chỉ ghi thêm bằng chứng đồng ý của tài khoản HTX. Mỗi dòng: tài khoản đồng ý
-- (user_ref = sub), mã phiên bản nội dung (text_version), thời điểm theo giờ máy chủ (event_time). Không có cột họ
-- tên, số điện thoại (quy tắc 7): chỉ mã tài khoản. Nội dung và mã phiên bản hiện hành là tham số có phiên bản của
-- pf/masterdata (accounts.data_consent.version, accounts.data_consent.text:<mã>), không nằm trong DB này.
-- stream_key = 'dataconsent:<user_ref>'. Người đồng ý chính là người ghi (actor_ref = user_ref).
CREATE TABLE pf_accounts.data_consent (
  id                uuid        NOT NULL,                 -- UUIDv7 (R-11)
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,
  seq               bigint      NOT NULL,
  user_ref          uuid        NOT NULL,
  text_version      text        NOT NULL CHECK (text_version ~ '^[A-Za-z0-9_.-]{1,40}$'),
  event_time        timestamptz NOT NULL,                 -- R-13: giờ máy chủ
  recorded_at       timestamptz NOT NULL DEFAULT now(),   -- R-13, khóa phân vùng
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
  CHECK (actor_ref = user_ref)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX data_consent_stream ON pf_accounts.data_consent (tenant_id, stream_key, seq);
CREATE INDEX data_consent_idem   ON pf_accounts.data_consent (tenant_id, idem_key);
CREATE INDEX data_consent_user   ON pf_accounts.data_consent (tenant_id, user_ref, text_version);
SELECT shared.apply_ledger('pf_accounts.data_consent', 'pf_accounts.data_consent');

GRANT SELECT, INSERT ON pf_accounts.data_consent TO r_accounts;
