"use client";

import { unwrap } from "@/api/client";
import { useAuth } from "@/auth/AuthProvider";
import { Dialog } from "@/components/Dialog";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Loading } from "@/components/ui";
import { useCatalogNames } from "@/htx/common";
import { formatDate } from "@/lib/format";
import { useAsync } from "@/lib/useAsync";

const ACTION: Record<string, string> = { opened: "Mở vụ", corrected: "Sửa (bản ghi bù)", completed: "Hoàn thành vụ", cancelled: "Hủy vụ" };

/** Chi tiết và lịch sử một vụ, tải theo mã vụ — mở từ danh sách vụ hoặc từ thông báo (AH-22, deep link `?season=`). */
export function SeasonDetailDialog({ seasonRef, onClose }: { seasonRef: string; onClose: () => void }) {
  const { api } = useAuth();
  const d = useAsync(() => unwrap(api.GET("/v1/cropcycle/seasons/{ref}", { params: { path: { ref: seasonRef } } })), [api, seasonRef]);
  const varietyName = useCatalogNames("rice_variety");
  const title = d.data ? `Vụ ${d.data.season_code} ${d.data.season_year} — lô ${d.data.plot_code}` : "Chi tiết vụ";
  return (
    <Dialog title={title} onClose={onClose}>
      {d.error ? <ProblemAlert problem={d.error} /> : null}
      {d.loading && !d.data ? <Loading /> : null}
      {d.data ? (
        <div className="stack" style={{ gap: 10 }}>
          {d.data.progress ? (
            <p className="small">
              Tiến độ SOP (chỉ nhật ký đã duyệt): <b style={{ display: "inline" }}>{d.data.progress.sop_steps_done}/{d.data.progress.sop_steps}</b> bước · nhật ký chờ duyệt {d.data.progress.pending_logs}
            </p>
          ) : null}
          <ol style={{ margin: "0 0 0 18px" }} aria-label="Lịch sử vụ">
            {d.data.events.map((ev) => (
              <li key={ev.id} className="small" style={{ marginBottom: 4 }}>
                <b style={{ display: "inline" }}>{ACTION[ev.action] ?? ev.action}</b> · {formatDate(ev.event_time)} · giống {ev.variety_code ? (varietyName.get(ev.variety_code) ?? ev.variety_code) : "—"} · sạ{formatDate(ev.sowing_date)}
                {ev.correction_reason ? <div className="mute">Lý do: {ev.correction_reason}</div> : null}
              </li>
            ))}
          </ol>
        </div>
      ) : null}
    </Dialog>
  );
}
