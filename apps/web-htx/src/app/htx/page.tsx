"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { cooperativesPage, coopPlace } from "@/api/adapters/cooperatives";
import { listChildUnits } from "@/api/adapters/units";
import { usePagedList } from "@/api/pagination";
import { useAuth } from "@/auth/AuthProvider";
import { hasRole, ROLES } from "@/auth/principal";
import { CoopImportPanel } from "@/htx/CoopImport";
import { Icon } from "@/components/Icon";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Empty, LoadMore, Loading, PageHead } from "@/components/ui";
import { formatDecimal, formatInt } from "@/lib/decimal";
import { useAsync } from "@/lib/useAsync";

function useDebounced<T>(value: T, ms = 300): T {
  const [v, setV] = useState(value);
  useEffect(() => {
    const t = setTimeout(() => setV(value), ms);
    return () => clearTimeout(t);
  }, [value, ms]);
  return v;
}

/** GET /v1/cooperatives — HTX/THT trong phạm vi, tìm theo tên/Mã HTX/MST, lọc theo tỉnh/xã (G-03). */
export default function CooperativesPage() {
  const { api, principal } = useAuth();
  const [q, setQ] = useState("");
  const [unit, setUnit] = useState("");
  const [includeInactive, setIncludeInactive] = useState(false);
  const [importing, setImporting] = useState(false);
  const [version, setVersion] = useState(0);
  const canImport = hasRole(principal, ROLES.registryAdmin, ROLES.platformAdmin, ROLES.knProvinceAdmin);
  const profileAudience = hasRole(principal, ROLES.platformAdmin, ROLES.coopManager, ROLES.htxMember);
  // FN-06 (AH-274): Đại diện HTX thấy thêm HTX đã duyệt chia sẻ cho HTX mình — chỉ xem.
  const sharedRow = (ref: string) => hasRole(principal, ROLES.coopManager) && !hasRole(principal, ROLES.platformAdmin) && !!principal?.orgRef && principal.orgRef !== ref;
  const dq = useDebounced(q);

  // Bộ lọc địa bàn: TW chọn tỉnh; tỉnh chọn xã trực thuộc; xã không cần lọc.
  const parent = principal?.level === "tinh" ? (principal.scopeUnits.find((u) => u.length === 2) ?? null) : null;
  const showFilter = principal?.level === "tw" || principal?.level === "tinh" || principal?.scopeAll;
  const units = useAsync(() => (showFilter ? listChildUnits(api, parent) : Promise.resolve([])), [api, parent, showFilter]);

  const list = usePagedList(
    (cursor) =>
      cooperativesPage(api, { q: dq || undefined, admin_unit: unit || undefined, include_inactive: includeInactive || undefined, cursor, limit: 20 }),
    [api, dq, unit, includeInactive, version],
  );

  return (
    <>
      <PageHead
        title="Quản lý HTX & nông hộ"
        subtitle="Danh sách HTX, tổ hợp tác trong phạm vi được phân quyền"
        actions={
          <>
          {hasRole(principal, ROLES.platformAdmin) ? <Link className="btn btn-p btn-sm" href="/htx/them-moi">Thêm HTX</Link> : null}
          {canImport ? (
            <button type="button" className="btn btn-o btn-sm" onClick={() => setImporting((v) => !v)} aria-expanded={importing}>
              <Icon name="upload" size={15} />
              Nhập HTX từ Excel
            </button>
          ) : null}
          </>
        }
      />
      {importing ? <CoopImportPanel onDone={() => setVersion((v) => v + 1)} /> : null}
      <div className="card sec">
        <form className="row" role="search" onSubmit={(e) => e.preventDefault()} style={{ marginBottom: 14 }}>
          <div className="field" style={{ flex: "1 1 260px" }}>
            <label htmlFor="htx-q">Tìm HTX</label>
            <input id="htx-q" className="input" type="search" placeholder="Tên, Mã HTX hoặc mã số thuế…" value={q} onChange={(e) => setQ(e.target.value)} />
          </div>
          {showFilter ? (
            <div className="field">
              <label htmlFor="htx-unit">{parent ? "Xã" : "Tỉnh"}</label>
              <select id="htx-unit" className="select" value={unit} onChange={(e) => setUnit(e.target.value)}>
                <option value="">Tất cả</option>
                {(units.data ?? []).map((u) => (
                  <option key={u.code} value={u.code}>
                    {u.name}
                  </option>
                ))}
              </select>
            </div>
          ) : null}
          <label className="row" style={{ alignSelf: "flex-end", paddingBottom: 8 }}>
            <input type="checkbox" checked={includeInactive} onChange={(e) => setIncludeInactive(e.target.checked)} />
            Gồm cả HTX đã vô hiệu hoá
          </label>
        </form>

        {list.error ? <ProblemAlert problem={list.error} onRetry={list.reload} /> : null}
        <div className="tbl-wrap">
          <table className="tbl">
            <caption className="sr-only">Danh sách HTX</caption>
            <thead>
              <tr>
                <th scope="col">HTX / THT</th>
                <th scope="col">Địa bàn</th>
                <th scope="col" className="num">Hộ thành viên</th>
                <th scope="col" className="num">Thửa số hoá</th>
                {profileAudience ? <><th scope="col" className="num">Trồng trọt (ha)</th><th scope="col" className="num">Chăn nuôi (ha)</th><th scope="col" className="num">Máy móc</th></> : <th scope="col" className="num">Diện tích đăng ký (ha)</th>}
                <th scope="col">Trạng thái</th>
              </tr>
            </thead>
            <tbody>
              {list.items.map((c) => (
                <tr key={c.ref}>
                  <td>
                    <Link href={`/htx/${c.ref}`} style={{ fontWeight: 600 }}>
                      {c.name}
                    </Link>
                    <div className="mute small">
                      {c.code}
                      {c.tax_code ? ` · MST ${c.tax_code}` : ""}
                    </div>
                    {sharedRow(c.ref) ? <span className="chip c-info">Được chia sẻ · chỉ xem</span> : null}
                  </td>
                  <td>{coopPlace(c)}</td>
                  <td className="num">{profileAudience ? formatInt(c.household_areas?.households) : formatInt(c.member_households)}</td>
                  <td className="num" title={c.digitized_plots === null ? "Chưa nối số liệu thửa (pf/geo)" : undefined}>
                    {c.digitized_plots === null || c.digitized_plots === undefined ? "—" : formatInt(c.digitized_plots)}
                  </td>
                  {profileAudience ? <><td className="num">{formatDecimal(c.household_areas?.cultivated_ha, 4)}</td><td className="num">{formatDecimal(c.household_areas?.livestock_ha, 4)}</td><td className="num">{formatInt(c.machine_count)}</td></> : <td className="num">{formatDecimal(c.area_ha, 1)}</td>}
                  <td>
                    {c.active ? (
                      <span className="chip c-ok">Đang hoạt động</span>
                    ) : (
                      <span className="chip c-mute" title={c.status_reason ?? undefined}>
                        Đã vô hiệu hoá
                      </span>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        {list.loading ? <Loading /> : null}
        {!list.loading && !list.error && list.items.length === 0 ? (
          <Empty>
            <Icon name="search" /> Không có HTX phù hợp bộ lọc.
          </Empty>
        ) : null}
        <LoadMore hasMore={list.hasMore} loading={list.loading} onClick={list.loadMore} />
      </div>
    </>
  );
}
