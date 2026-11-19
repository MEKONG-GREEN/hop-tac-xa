-- Function do own_kref sở hữu; trigger chỉ trên nguồn Party có thẩm quyền.
-- Không cấp membership/DML k_ref cho vai Party.
CREATE TRIGGER trg_kref_deleted_cooperative AFTER DELETE ON pf_party.org
FOR EACH ROW EXECUTE FUNCTION k_ref.project_deleted_cooperative();
