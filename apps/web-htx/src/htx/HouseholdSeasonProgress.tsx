"use client";

import { useState } from "react";
import { unwrap } from "@/api/client";
import { usePagedList } from "@/api/pagination";
import { useAuth } from "@/auth/AuthProvider";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Empty, LoadMore, Loading } from "@/components/ui";
import { formatDecimal } from "@/lib/decimal";
import { useAsync } from "@/lib/useAsync";

const STATUS: Record<string, string> = { open: "Đang canh tác", completed: "Đã thu hoạch", cancelled: "Đã dừng vụ", closed: "Đã hoàn thành vụ" };

/**
 * AH-140 (FN-03.12, FN-03-BR-08): danh sách vụ nông hộ của HTX kèm trạng thái, % tiến độ, số việc đến hạn, trễ — chỉ
 * đọc, không có nút tạo/sửa/dừng. Lọc theo Thành viên HTX mở vụ và trạng thái. Máy chủ kiểm phạm vi HTX.
 */
export function HouseholdSeasonProgress({ coopRef, householdLabel }: { coopRef: string; householdLabel: (ref: string) => string }) {
  const { api } = useAuth();
  const [status, setStatus] = useState<"" | "open" | "completed" | "cancelled" | "closed">("open");
  const [member, setMember] = useState("");
  // Danh sách Thành viên để lọc: danh bạ người nhận lịch thời vụ (Đại diện/Admin); không đọc được thì để trống.
  const members = useAsync(async () => {
    try {
      const page = await unwrap(api.GET("/v1/cooperatives/{ref}/season-calendar-recipients", { params: { path: { ref: coopRef }, query: { limit: 100 } } }));
      return page.items.map((m) => ({ ref: m.actor_ref, label: m.label || `Thành viên ${m.actor_ref.slice(-4)}` }));
    } catch {
      return [];
    }
  }, [api, coopRef]);
  const list = usePagedList(
    (cursor) => unwrap(api.GET("/v1/cooperatives/{ref}/household-seasons", {
      params: { path: { ref: coopRef }, query: { status: status || undefined, opened_by: member || undefined, cursor, limit: 50 } },
    })),
    [api, coopRef, status, member],
  );
  return <section className="card sec" aria-label="Tiến độ vụ nông hộ">
    <h3>Tiến độ vụ nông hộ</h3>
    <p className="small mute">Chỉ xem: trạng thái, % tiến độ, việc đến hạn và trễ của các vụ trong HTX.</p>
    <form className="row" role="search" onSubmit={(e) => e.preventDefault()} style={{ alignItems: "flex-end" }}>
      <label className="field">Trạng thái vụ
        <select className="select" value={status} onChange={(e) => setStatus(e.target.value as typeof status)}>
          <option value="">Tất cả</option>
          <option value="open">Đang canh tác</option>
          <option value="completed">Đã thu hoạch</option>
          <option value="closed">Đã hoàn thành vụ</option>
          <option value="cancelled">Đã dừng vụ</option>
        </select>
      </label>
      <label className="field">Thành viên mở vụ
        <select className="select" value={member} onChange={(e) => setMember(e.target.value)}>
          <option value="">Tất cả</option>
          {(members.data ?? []).map((m) => <option key={m.ref} value={m.ref}>{m.label}</option>)}
        </select>
      </label>
    </form>
    {list.error ? <ProblemAlert problem={list.error} onRetry={list.reload} /> : null}
    {list.loading && !list.items.length ? <Loading /> : null}
    {!list.loading && !list.items.length && !list.error ? <Empty>Không có vụ phù hợp.</Empty> : null}
    {list.items.length ? <table className="tbl">
      <thead><tr><th>Nông hộ</th><th>Vụ</th><th>Diện tích</th><th>Trạng thái</th><th>Tiến độ</th><th>Đến hạn</th><th>Trễ</th><th>Người mở vụ</th></tr></thead>
      <tbody>{list.items.map((s) => <tr key={s.season_ref}>
        <td>{householdLabel(s.household_ref)}</td>
        <td>{s.season_code} {s.season_year} · {s.crop_code}{s.variety_code ? ` · ${s.variety_code}` : ""}</td>
        <td>{formatDecimal(s.area_ha, 2)} ha</td>
        <td>{STATUS[s.status] ?? s.status}</td>
        <td>{s.progress?.percent == null ? "—" : `${s.progress.percent}%`}</td>
        <td>{s.progress?.due ?? "—"}</td>
        <td>{s.progress?.overdue ?? "—"}</td>
        <td>{s.opened_by_label ?? `Thành viên ${s.opened_by.slice(-4)}`}</td>
      </tr>)}</tbody>
    </table> : null}
    <LoadMore hasMore={list.hasMore} loading={list.loading} onClick={list.loadMore} />
  </section>;
}
