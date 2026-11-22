"use client";

import Link from "next/link";
import { unwrap, type ApiPaths } from "@/api/client";
import { useAuth } from "@/auth/AuthProvider";
import { hasRole, ROLES, type Principal } from "@/auth/principal";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Empty, Loading } from "@/components/ui";
import { formatDateTime } from "@/lib/format";
import { useAsync } from "@/lib/useAsync";

type Sharing = ApiPaths["/v1/cooperatives/{ref}/htx-sharing"]["get"]["responses"][200]["content"]["application/json"]["items"][number];
const STATES: Record<Sharing["state"], string> = { requested: "Gửi yêu cầu", approved: "Duyệt", rejected: "Từ chối", revoked: "Thu hồi" };

/**
 * FN-03, QTPQ-Q-55: ai thấy tab "Liên kết chia sẻ dữ liệu" trên trang một HTX — Đại diện HTX trên HTX của mình (có
 * nút sang màn Liên kết & chia sẻ dữ liệu), Admin trên mọi HTX ở chế độ chỉ xem. Thành viên HTX không có tab (AC-N1).
 */
export function sharingTabAudience(principal: Principal | null, coopRef: string): "manage" | "view" | null {
  if (hasRole(principal, ROLES.platformAdmin)) return "view";
  if (hasRole(principal, ROLES.coopManager) && principal?.orgRef === coopRef) return "manage";
  return null;
}

export function CoopSharingTab({ coopRef, manage }: { coopRef: string; manage: boolean }) {
  const { api } = useAuth();
  const list = useAsync(() => unwrap(api.GET("/v1/cooperatives/{ref}/htx-sharing", { params: { path: { ref: coopRef } } })), [api, coopRef]);
  const name = (ref: string, label?: string) => (ref === coopRef ? "HTX này" : label || ref.slice(-8));
  const rows = list.data?.items ?? [];
  return (
    <div className="stack">
      <div className="row" style={{ justifyContent: "space-between", alignItems: "center" }}>
        <p className="small mute" style={{ margin: 0 }}>
          {manage ? "Yêu cầu chia sẻ HTX của bạn đã gửi và nhận được. Toàn bộ dữ liệu được chia sẻ ở chế độ chỉ xem." : "Chỉ xem: việc chia sẻ dữ liệu do các HTX tự quyết định."}
        </p>
        {manage ? <Link className="btn btn-p btn-sm" href="/htx/chia-se">Liên kết & chia sẻ dữ liệu</Link> : null}
      </div>
      {list.error ? <ProblemAlert problem={list.error} onRetry={list.reload} /> : null}
      {list.loading && !list.data ? <Loading /> : null}
      {list.data && rows.length === 0 ? <Empty>Chưa có liên kết chia sẻ dữ liệu.</Empty> : null}
      {rows.length > 0 ? (
        <div className="tbl-wrap">
          <table className="tbl">
            <caption className="sr-only">Liên kết chia sẻ dữ liệu</caption>
            <thead>
              <tr>
                <th scope="col">HTX yêu cầu xem</th>
                <th scope="col">HTX được yêu cầu</th>
                <th scope="col">Trạng thái</th>
                <th scope="col">Ngày gửi</th>
                <th scope="col">Ngày xử lý</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((r) => (
                <tr key={r.ref}>
                  <td>{name(r.requester_cooperative_ref, r.requester_name)}</td>
                  <td>{name(r.owner_cooperative_ref, r.owner_name)}</td>
                  <td>{STATES[r.state]}</td>
                  <td>{formatDateTime(r.requested_at)}</td>
                  <td>{r.processed_at ? formatDateTime(r.processed_at) : "—"}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}
    </div>
  );
}
