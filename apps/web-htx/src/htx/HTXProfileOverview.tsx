"use client";

import { useAuth } from "@/auth/AuthProvider";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Loading } from "@/components/ui";
import { formatDecimal, formatInt } from "@/lib/decimal";
import { useAsync } from "@/lib/useAsync";
import { getHTXProfile } from "./profiles";

export function HTXProfileOverview({ cooperativeRef, machinesOnly = false }: { cooperativeRef: string; machinesOnly?: boolean }) {
  const { api, principal } = useAuth();
  const identity = `${principal?.subject}:${principal?.tenantId}:${principal?.orgRef}:${principal?.roles.join("|")}`;
  const result = useAsync(() => getHTXProfile(api, cooperativeRef), [api, cooperativeRef, identity]);
  if (result.error) return <ProblemAlert problem={result.error} onRetry={result.reload} />;
  if (!result.data || result.loading) return <Loading />;
  const profile = result.data;
  if (machinesOnly) return <section aria-label="Máy móc hiện có">
    <p>Tổng số máy: <strong>{formatInt(profile.machine_count)}</strong></p>
    {profile.machine_count == null ? <p className="mute">Chưa có dữ liệu từ phân hệ Vật tư kho và máy móc.</p> : profile.machine_groups?.length ? <table className="tbl">
      <thead><tr><th scope="col">Loại máy</th><th scope="col" className="num">Số lượng</th></tr></thead>
      <tbody>{profile.machine_groups.map((group) => <tr key={group.type_ref}><td>{group.type_name}</td><td className="num">{formatInt(group.quantity)}</td></tr>)}</tbody>
    </table> : <p>Chưa có máy móc.</p>}
  </section>;
  const areas = profile.household_areas;
  const rows = [
    ["Mã HTX", profile.code], ["Tên HTX", profile.name], ["Tỉnh / thành phố", profile.province_name ?? "—"],
    ["Xã / phường / đặc khu", profile.commune_name ?? "—"], ["Địa chỉ trụ sở", profile.address ?? "—"],
    ["Người đại diện", profile.representative_name || "Chưa cập nhật"], ["Số điện thoại", profile.phone || "Chưa cập nhật"],
    ["Diện tích trồng trọt (ha)", formatDecimal(areas?.cultivated_ha, 4)], ["Diện tích chăn nuôi (ha)", formatDecimal(areas?.livestock_ha, 4)],
    ["Số máy móc hiện có", formatInt(profile.machine_count)],
  ];
  return <section aria-label="Hồ sơ HTX">
    <dl style={{ display: "grid", gridTemplateColumns: "minmax(140px, 220px) 1fr", gap: "10px 16px" }}>{rows.map(([label, value]) => <div key={label} style={{ display: "contents" }}><dt className="mute">{label}</dt><dd>{value}</dd></div>)}</dl>
    {areas && (areas.missing_cultivated > 0 || areas.missing_livestock > 0) ? <p className="mute small">Một số hộ chưa khai đủ diện tích; tổng tương ứng sẽ hiện khi dữ liệu đầy đủ.</p> : null}
    <p className="mute small">Diện tích cộng từ hồ sơ các hộ hiện thuộc HTX; không nhập tại hồ sơ HTX.</p>
  </section>;
}
