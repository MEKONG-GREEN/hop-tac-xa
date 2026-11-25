SET LOCAL lock_timeout = '5s';
-- AH-87 / KH-05: sản lượng lúa, rơm dự kiến của vụ theo hộ. Sổ chỉ ghi thêm, mỗi vụ một stream; mỗi dòng mang trạng
-- thái đầy đủ (tham số đã chụp, giá trị hệ thống tính, giá trị Thành viên HTX nhập). Bản hiện hành = dòng seq lớn nhất.
--   computed   : lúc mở vụ (chưa có giá trị nhập).
--   entered    : Thành viên HTX nhập, sửa hoặc xóa giá trị của mình.
--   recomputed : sửa vụ (AH-175) đổi diện tích hoặc giống; giữ nguyên giá trị nhập.
CREATE TABLE pf_coop.household_yield_estimate_event (
 id uuid NOT NULL, tenant_id uuid NOT NULL, stream_key text NOT NULL, seq bigint NOT NULL,
 season_ref uuid NOT NULL, cooperative_ref uuid NOT NULL, household_ref uuid NOT NULL,
 action text NOT NULL CHECK(action IN ('computed','entered','recomputed')),
 area_ha numeric NOT NULL CHECK(area_ha > 0 AND scale(area_ha) <= 2),
 base_yield_t_per_ha numeric NOT NULL CHECK(base_yield_t_per_ha >= 0),
 is_rice boolean NOT NULL,
 straw_pct numeric NULL CHECK(straw_pct BETWEEN 0 AND 100 AND scale(straw_pct) <= 4),
 computed_rice_t numeric NOT NULL,
 computed_straw_t numeric NULL,
 entered_rice_t numeric NULL,
 entered_straw_t numeric NULL,
 event_time timestamptz NOT NULL, recorded_at timestamptz NOT NULL DEFAULT now(),
 actor_ref uuid NOT NULL, device_id text NULL, idem_key text NOT NULL, rule_version text NOT NULL,
 supersedes_id uuid NULL, correction_reason text NULL,
 prev_hash bytea NOT NULL, row_hash bytea NOT NULL,
 PRIMARY KEY(id, recorded_at),
 CHECK(stream_key = 'yield_estimate:' || season_ref::text),
 -- Giới hạn 99999999.99 tấn để ×1000 vẫn vừa yield_summary.expected_kg numeric(14,3).
 CHECK(computed_rice_t >= 0 AND computed_rice_t <= 99999999.99 AND scale(computed_rice_t) <= 2),
 CHECK(computed_straw_t IS NULL OR (computed_straw_t >= 0 AND computed_straw_t <= 99999999.99 AND scale(computed_straw_t) <= 2)),
 CHECK(entered_rice_t IS NULL OR (entered_rice_t >= 0 AND entered_rice_t <= 99999999.99 AND scale(entered_rice_t) <= 2)),
 CHECK(entered_straw_t IS NULL OR (entered_straw_t >= 0 AND entered_straw_t <= 99999999.99 AND scale(entered_straw_t) <= 2)),
 CHECK(is_rice = (straw_pct IS NOT NULL)),
 CHECK(is_rice = (computed_straw_t IS NOT NULL)),
 CHECK(is_rice OR entered_straw_t IS NULL),
 CHECK(action <> 'computed' OR (entered_rice_t IS NULL AND entered_straw_t IS NULL)),
 -- Mỗi dòng là trạng thái đầy đủ, không phải bản ghi bù.
 CHECK(supersedes_id IS NULL AND correction_reason IS NULL)
) PARTITION BY RANGE(recorded_at);
CREATE INDEX household_yield_estimate_current ON pf_coop.household_yield_estimate_event(tenant_id, stream_key, seq DESC);
CREATE INDEX household_yield_estimate_idem ON pf_coop.household_yield_estimate_event(tenant_id, idem_key);
SELECT shared.apply_ledger('pf_coop.household_yield_estimate_event', 'pf_coop.household_yield_estimate_event');
-- Fail-closed: không có vụ theo hộ khớp HTX/hộ thì không thấy, không ghi được dòng nào.
CREATE POLICY p_scope ON pf_coop.household_yield_estimate_event AS RESTRICTIVE FOR ALL TO r_cooperative
 USING(EXISTS(SELECT 1 FROM pf_coop.season_current s WHERE s.tenant_id = household_yield_estimate_event.tenant_id
   AND s.season_ref = household_yield_estimate_event.season_ref AND s.entry_kind = 'household'
   AND s.cooperative_ref = household_yield_estimate_event.cooperative_ref AND s.household_ref = household_yield_estimate_event.household_ref))
 WITH CHECK(EXISTS(SELECT 1 FROM pf_coop.season_current s WHERE s.tenant_id = household_yield_estimate_event.tenant_id
   AND s.season_ref = household_yield_estimate_event.season_ref AND s.entry_kind = 'household'
   AND s.cooperative_ref = household_yield_estimate_event.cooperative_ref AND s.household_ref = household_yield_estimate_event.household_ref));
ALTER TABLE pf_coop.household_yield_estimate_event OWNER TO own_cooperative;
REVOKE ALL ON pf_coop.household_yield_estimate_event FROM PUBLIC;
GRANT SELECT, INSERT ON pf_coop.household_yield_estimate_event TO r_cooperative;

-- DATA-03 (AH-283): tham chiếu HTX mới tham gia chốt ghi/xóa Nút HTX.
SELECT shared.publish_cooperative_references('pf_coop','pf_cooperative_pub','r_cooperative_pub');
