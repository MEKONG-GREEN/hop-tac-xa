-- AH-283 / FN-03 BR-03: nguồn diện tích chăn nuôi thuộc hồ sơ hộ, không nhập tại HTX.
-- NULL ở dữ liệu cũ là chưa khai báo, không suy diễn thành số 0.
SET LOCAL lock_timeout = '5s';
ALTER TABLE pf_coop.household ADD COLUMN livestock_area_ha numeric(14,4)
  CHECK (livestock_area_ha >= 0);
COMMENT ON COLUMN pf_coop.household.livestock_area_ha IS
  'Diện tích chăn nuôi (ha), tối đa 4 số lẻ; NULL = chưa khai báo';
