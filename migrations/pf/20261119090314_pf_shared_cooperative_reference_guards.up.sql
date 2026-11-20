-- FN-03/DATA-03: giao thức transaction giữa xóa Nút HTX và nguồn tham chiếu.
-- Không tạo FK chéo module, không đọc bảng riêng của owner khác ở runtime.
-- Writer giữ shared xact lock; Party xóa giữ exclusive xact lock cùng khóa.
-- Dùng được với PgBouncer transaction pooling; không giữ kết nối qua I/O.
SET LOCAL lock_timeout = '5s';

CREATE FUNCTION shared.cooperative_reference_columns()
RETURNS TABLE(source_schema name,source_table name,source_column name,source_type text,reference_mode text)
LANGUAGE sql STABLE SET search_path=pg_catalog,pg_temp AS $$
 SELECT n.nspname,c.relname,a.attname,format_type(a.atttypid,a.atttypmod),
 CASE
  WHEN n.nspname='pf_coop' AND c.relname LIKE 'bilateral\_%' THEN 'private'
  -- Chỉ projection hồ sơ và audit của chính hồ sơ được loại khỏi dữ liệu phát sinh.
  WHEN (n.nspname,c.relname,a.attname) IN
   (('pf_party','org_event','org_ref'),('pf_party','htx_profile_completion','org_ref'),
    ('pf_coop','cooperative','org_ref')) THEN 'metadata'
  WHEN n.nspname='pf_party' AND c.relname='org' AND a.attname='ref' THEN 'lifecycle'
  ELSE 'enforce' END
 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
 JOIN pg_attribute a ON a.attrelid=c.oid AND a.attnum>0 AND NOT a.attisdropped
 WHERE n.nspname LIKE 'pf\_%' AND c.relkind IN ('r','p') AND NOT c.relispartition
 AND a.atttypid IN ('uuid'::regtype,'uuid[]'::regtype)
 AND (a.attname ~ '(org_ref|coop_ref|cooperative_ref|htx_ref|node_ref|node_id)s?$'
      OR (n.nspname='pf_party' AND c.relname='org' AND a.attname IN ('ref','parent_ref')))
 ORDER BY 1,2,3
$$;
REVOKE ALL ON FUNCTION shared.cooperative_reference_columns() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION shared.cooperative_reference_columns() TO ag_shared_user;

CREATE FUNCTION shared.guard_deleted_cooperative() RETURNS trigger
LANGUAGE plpgsql SECURITY INVOKER SET search_path=pg_catalog,pg_temp AS $$
DECLARE
 tenant uuid:=shared.current_tenant();
 previous_tenant text:=current_setting('app.tenant_id',true);
 temporary_tenant boolean:=false;
 refs uuid[]:='{}';
 ref uuid;
 field jsonb;
 row_data jsonb:=to_jsonb(NEW);
BEGIN
 IF TG_OP NOT IN ('INSERT','UPDATE') OR TG_LEVEL<>'ROW' OR TG_NARGS=0 THEN
  RAISE EXCEPTION 'invalid cooperative reference guard' USING ERRCODE='42501';
 END IF;
 IF tenant IS NULL AND EXISTS(SELECT 1 FROM pg_roles WHERE rolname=current_user AND (rolsuper OR rolbypassrls)) THEN
  -- Fixture/bootstrap đặc quyền vẫn được khóa và kiểm tombstone đúng tenant;
  -- vai ứng dụng không được đi nhánh này. Giữ nguyên context sau câu lệnh.
  tenant:=NEW.tenant_id;temporary_tenant:=true;
  PERFORM set_config('app.tenant_id',tenant::text,true);
 END IF;
 IF tenant IS NULL OR tenant IS DISTINCT FROM NEW.tenant_id THEN
  IF TG_TABLE_SCHEMA='pf_coop' AND TG_TABLE_NAME IN ('bilateral_pair_event','bilateral_record')
     AND tenant IS NOT NULL AND tenant=(row_data->>'counterparty_tenant_id')::uuid THEN
   -- Bên MKG ghi hợp lệ theo p_pair; khóa/dấu xóa thuộc tenant chủ HTX.
   tenant:=NEW.tenant_id;temporary_tenant:=true;
   PERFORM set_config('app.tenant_id',tenant::text,true);
  ELSE
   -- Giữ chính sách RLS làm nguồn từ chối tenant sai; không tiết lộ tombstone
   -- của tenant khác và không thay lỗi row-level security của đường hiện hữu.
   RETURN NEW;
  END IF;
 END IF;
 FOR i IN 0..TG_NARGS-1 LOOP
  -- UPDATE một Nút hiện hữu không phục hồi được Nút đã bị xóa. Caller đã
  -- khóa dòng Nút trước UPDATE; không lấy thêm khóa trên chính ref ở đây
  -- để tránh đảo thứ tự với xóa (lifecycle lock -> row lock).
  IF TG_TABLE_SCHEMA='pf_party' AND TG_TABLE_NAME='org' AND TG_OP='UPDATE' AND TG_ARGV[i]='ref'
     AND row_data->'ref'=to_jsonb(OLD)->'ref' THEN CONTINUE; END IF;
  field:=row_data->TG_ARGV[i];
  IF field IS NULL OR field='null'::jsonb THEN CONTINUE; END IF;
  IF jsonb_typeof(field)='array' THEN
   SELECT refs||coalesce(array_agg(value::uuid),ARRAY[]::uuid[]) INTO refs
    FROM jsonb_array_elements_text(field) WHERE value IS NOT NULL;
  ELSE
   refs:=array_append(refs,(field#>>'{}')::uuid);
  END IF;
 END LOOP;
 -- Thứ tự ổn định khi một bản ghi có nhiều HTX/người sở hữu.
 FOR ref IN SELECT DISTINCT value FROM unnest(refs) value ORDER BY value LOOP
  PERFORM pg_advisory_xact_lock_shared(hashtextextended('cooperative-life:'||tenant::text||':'||ref::text,283));
  IF EXISTS(SELECT 1 FROM pf_party_pub.v1_cooperative_tombstones d
            WHERE d.tenant_id=tenant AND d.cooperative_ref=ref) THEN
   RAISE EXCEPTION 'cooperative was permanently deleted'
    USING ERRCODE='23503',CONSTRAINT='cooperative_permanently_deleted';
  END IF;
 END LOOP;
 IF temporary_tenant THEN PERFORM set_config('app.tenant_id',coalesce(previous_tenant,''),true); END IF;
 RETURN NEW;
END $$;
REVOKE ALL ON FUNCTION shared.guard_deleted_cooperative() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION shared.guard_deleted_cooperative() TO ag_shared_user;

-- Chỉ pipeline migration gọi helper DDL. Mỗi migration owner gọi cho schema
-- của mình; view chỉ công bố tenant/ref/loại đếm, không đưa tên hay actor ra ngoài.
CREATE FUNCTION shared.publish_cooperative_references(src name,pub name,pub_role name) RETURNS void
LANGUAGE plpgsql SECURITY INVOKER SET search_path=pg_catalog,pg_temp AS $$
DECLARE
 r record;
 source record;
 statements text[]:='{}';
 args text;
 category text;
 record_expr text;
 ref_expr text;
 from_expr text;
 predicate text;
BEGIN
 IF src NOT LIKE 'pf\_%' OR pub NOT LIKE 'pf\_%\_pub' OR pub_role NOT LIKE 'r\_%\_pub' THEN
  RAISE EXCEPTION 'invalid cooperative reference publication';
 END IF;
 FOR source IN SELECT source_table,string_agg(quote_literal(source_column),',' ORDER BY source_column) AS arguments
  FROM shared.cooperative_reference_columns() WHERE source_schema=src AND reference_mode<>'metadata'
  GROUP BY source_table ORDER BY source_table LOOP
  args:=source.arguments;
  EXECUTE format('DROP TRIGGER IF EXISTS zzzz_cooperative_lifecycle ON %I.%I',src,source.source_table);
  EXECUTE format('CREATE TRIGGER zzzz_cooperative_lifecycle BEFORE INSERT OR UPDATE ON %I.%I FOR EACH ROW EXECUTE FUNCTION shared.guard_deleted_cooperative(%s)',src,source.source_table,args);
 END LOOP;
 FOR r IN SELECT * FROM shared.cooperative_reference_columns()
   WHERE source_schema=src AND reference_mode='enforce' ORDER BY source_table,source_column LOOP
  category:='business'; record_expr:='';
  IF src='pf_coop' AND r.source_table='household_membership' THEN
   category:='households'; record_expr:='s.household_ref';
  ELSIF src='pf_accounts' AND r.source_table IN
    ('account','account_event','farmer_link','role_assignment_current','role_assignment_event') THEN
   category:='accounts'; record_expr:='s.user_ref';
  ELSIF src='pf_accounts' AND r.source_table='cooperative_identity_reservation' THEN
   category:='pending_accounts';record_expr:='s.operation_ref';
  ELSIF src='pf_party' AND r.source_table LIKE '%share%' THEN
   category:='sharing';
  END IF;
  from_expr:=format('%I.%I s',src,r.source_table);
  IF r.source_type='uuid[]' THEN
   from_expr:=from_expr||format(' CROSS JOIN LATERAL unnest(s.%I) refs(cooperative_ref)',r.source_column);
   ref_expr:='refs.cooperative_ref';
  ELSE ref_expr:=format('s.%I',r.source_column);
  END IF;
  IF record_expr='' THEN record_expr:=ref_expr; END IF;
  predicate:=ref_expr||' IS NOT NULL';
  IF category='pending_accounts' THEN predicate:=predicate||' AND s.released_at IS NULL'; END IF;
  statements:=array_append(statements,format(
   'SELECT s.tenant_id,%s AS cooperative_ref,%L::text AS category,%s AS record_ref,%L::text AS source FROM %s WHERE %s',
   ref_expr,category,record_expr,src||'.'||r.source_table||'.'||r.source_column,from_expr,predicate));
 END LOOP;
 IF cardinality(statements)=0 THEN RAISE EXCEPTION 'no cooperative reference sources in %',src; END IF;
 EXECUTE format('CREATE OR REPLACE VIEW %I.v1_cooperative_references WITH(security_barrier=true) AS SELECT DISTINCT * FROM (%s) refs',pub,array_to_string(statements,' UNION ALL '));
 EXECUTE format('REVOKE ALL ON %I.v1_cooperative_references FROM PUBLIC',pub);
 EXECUTE format('GRANT SELECT ON %I.v1_cooperative_references TO %I',pub,pub_role);
END $$;
REVOKE ALL ON FUNCTION shared.publish_cooperative_references(name,name,name) FROM PUBLIC;

-- Thêm bảng/cột mới mà chưa có guard HOẶC chưa xuất hiện trong view của owner
-- sẽ chặn xóa ở runtime và làm test coverage hỏng. Không có danh sách nguồn
-- ngầm coi "không được nối" là số 0.
CREATE FUNCTION shared.unprotected_cooperative_references()
RETURNS TABLE(source_schema name,source_table name,source_column name)
LANGUAGE sql STABLE SECURITY INVOKER SET search_path=pg_catalog,pg_temp AS $$
 SELECT r.source_schema,r.source_table,r.source_column
 FROM shared.cooperative_reference_columns() r
 JOIN pg_namespace n ON n.nspname=r.source_schema
 JOIN pg_class c ON c.relnamespace=n.oid AND c.relname=r.source_table
 JOIN pg_attribute a ON a.attrelid=c.oid AND a.attname=r.source_column
 WHERE r.reference_mode<>'metadata' AND (
 NOT EXISTS(SELECT 1 FROM pg_trigger t WHERE t.tgrelid=c.oid AND NOT t.tgisinternal
  AND t.tgfoid='shared.guard_deleted_cooperative()'::regprocedure AND t.tgenabled IN ('O','A')
  AND (t.tgtype::integer & 23)=23
  AND position(('\x00'::bytea||convert_to(a.attname::text,'UTF8')||'\x00'::bytea)
      IN ('\x00'::bytea||t.tgargs))>0)
 OR (r.reference_mode='enforce' AND NOT EXISTS(
  SELECT 1 FROM pg_namespace p JOIN pg_class v ON v.relnamespace=p.oid
  JOIN pg_rewrite w ON w.ev_class=v.oid
  JOIN pg_depend d ON d.classid='pg_rewrite'::regclass AND d.objid=w.oid
  WHERE p.nspname=CASE WHEN r.source_schema='pf_coop' THEN 'pf_cooperative_pub' ELSE r.source_schema||'_pub' END
   AND v.relname='v1_cooperative_references' AND v.relkind='v'
   AND d.refclassid='pg_class'::regclass AND d.refobjid=c.oid AND d.refobjsubid=a.attnum))
 OR (r.reference_mode='private' AND (r.source_table NOT IN ('bilateral_pair_event','bilateral_record') OR NOT EXISTS(
  SELECT 1 FROM pg_trigger t JOIN pg_proc f ON f.oid=t.tgfoid
   JOIN pg_namespace fn ON fn.oid=f.pronamespace
   WHERE t.tgrelid='pf_party.org'::regclass AND NOT t.tgisinternal
   AND fn.nspname='pf_coop' AND f.proname='guard_private_cooperative_delete' AND f.pronargs=0
   AND t.tgenabled IN ('O','A') AND (t.tgtype::integer & 11)=11))))
$$;
REVOKE ALL ON FUNCTION shared.unprotected_cooperative_references() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION shared.unprotected_cooperative_references() TO ag_shared_user;
