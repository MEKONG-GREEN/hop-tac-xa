-- Chỉ đổi FK D-7 của projection hồ sơ đã có. Không cascade nông hộ/sổ/dữ liệu
-- nghiệp vụ; các nguồn đó phải chặn xóa bằng protocol + view công bố.
SET LOCAL lock_timeout = '5s';
ALTER TABLE pf_coop.cooperative DROP CONSTRAINT cooperative_tenant_id_org_ref_fkey;
ALTER TABLE pf_coop.cooperative ADD CONSTRAINT cooperative_tenant_id_org_ref_fkey
 FOREIGN KEY(tenant_id,org_ref) REFERENCES pf_party.org(tenant_id,ref) ON DELETE CASCADE NOT VALID;
ALTER TABLE pf_coop.cooperative VALIDATE CONSTRAINT cooperative_tenant_id_org_ref_fkey;
