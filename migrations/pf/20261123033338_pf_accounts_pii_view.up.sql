-- AH-223 (FN-TK-02/BR-04): nhật ký truy cập khi xem số điện thoại đầy đủ của tài khoản HTX.
--
-- pf_accounts.pii_view: SỔ chỉ ghi thêm. Danh sách tài khoản chỉ trả SĐT đã che; xem đầy đủ ở màn chi tiết thì ghi một
-- dòng: ai xem (actor_ref), xem tài khoản nào (user_ref), trường nào (field), lúc nào (event_time, giờ máy chủ). Không
-- chứa SĐT hay dữ liệu cá nhân khác (quy tắc 7) — chỉ mã tham chiếu. Ghi lỗi thì máy chủ không trả SĐT đầy đủ (đóng khi
-- lỗi). stream_key = 'piiview:<user_ref>'.
SET LOCAL lock_timeout = '5s';

CREATE TABLE pf_accounts.pii_view (
  id                uuid        NOT NULL,                 -- UUIDv7 (R-11)
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,
  seq               bigint      NOT NULL,
  user_ref          uuid        NOT NULL,                 -- tài khoản được xem
  field             text        NOT NULL CHECK (field IN ('phone')),
  event_time        timestamptz NOT NULL,                 -- R-13: giờ máy chủ
  recorded_at       timestamptz NOT NULL DEFAULT now(),   -- R-13, khóa phân vùng
  supersedes_id     uuid        NULL,
  correction_reason text        NULL,
  actor_ref         uuid        NOT NULL,                 -- người xem
  device_id         text        NULL,
  idem_key          text        NOT NULL,
  rule_version      text        NOT NULL,
  prev_hash         bytea       NOT NULL,
  row_hash          bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (correction_reason IS NULL OR length(correction_reason) >= 10)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX pii_view_stream ON pf_accounts.pii_view (tenant_id, stream_key, seq);
CREATE INDEX pii_view_idem   ON pf_accounts.pii_view (tenant_id, idem_key);
CREATE INDEX pii_view_actor  ON pf_accounts.pii_view (tenant_id, actor_ref, recorded_at);
SELECT shared.apply_ledger('pf_accounts.pii_view', 'pf_accounts.pii_view');

GRANT SELECT, INSERT ON pf_accounts.pii_view TO r_accounts;
