"use client";

import { useState } from "react";
import { unwrap } from "@/api/client";
import type { components } from "@/api/generated/application";
import { usePagedList } from "@/api/pagination";
import { useAuth } from "@/auth/AuthProvider";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Empty, LoadMore, Loading } from "@/components/ui";
import { formatDateVi, formatDecimalVi } from "@/lib/exactDecimal";
import { useAsync } from "@/lib/useAsync";

type Totals = components["schemas"]["HouseholdYieldTotals"];
type State = "pending" | "declared";

const STATUS: Record<string, string> = { open: "Đang canh tác", completed: "Đã thu hoạch", closed: "Đã hoàn thành vụ" };

/** Tấn 2 chữ số lẻ kiểu Việt Nam; null → "—". */
export function tonnesText(v: string | null | undefined): string {
  return v == null ? "—" : `${formatDecimalVi(v, { minDecimals: 2, maxDecimals: 2 })} tấn`;
}

/** Chênh lệch có dấu: "+0,90 tấn (+12,0%)", "−1,00 tấn (−25,0%)"; thiếu số → "—". */
export function diffText(diffT: string | null | undefined, diffPct: string | null | undefined): string {
  if (diffT == null) return "—";
  const sign = (v: string) => (v.startsWith("-") ? "−" : /^0(\.0+)?$/.test(v) ? "" : "+");
  const abs = (v: string) => v.replace(/^-/, "");
  const t = `${sign(diffT)}${formatDecimalVi(abs(diffT), { minDecimals: 2, maxDecimals: 2 })} tấn`;
  return diffPct == null ? t : `${t} (${sign(diffPct)}${formatDecimalVi(abs(diffPct), { minDecimals: 1, maxDecimals: 1 })}%)`;
}

/**
 * AH-153 (FN-06.6, DATA-03): sản lượng thực tế so với dự kiến của vụ nông hộ trong HTX — chỉ đọc, không có nút khai báo,
 * sửa, hủy (AC3). Hai nhóm: chờ khai báo (vụ đang canh tác, theo ngày thu hoạch dự kiến, đánh dấu quá ngày) và đã khai
 * báo (theo ngày thu hoạch). Lọc theo Thành viên mở vụ và mùa vụ; tổng hợp trên toàn bộ bộ lọc. Máy chủ kiểm phạm vi HTX.
 */
export function HouseholdYieldPanel({ coopRef, householdLabel }: { coopRef: string; householdLabel: (ref: string) => string }) {
  const { api } = useAuth();
  const [state, setState] = useState<State>("pending");
  const [member, setMember] = useState("");
  const [seasonCode, setSeasonCode] = useState("");
  const [year, setYear] = useState("");
  const [totals, setTotals] = useState<Totals | null>(null);
  const members = useAsync(async () => {
    try {
      const page = await unwrap(api.GET("/v1/cooperatives/{ref}/season-calendar-recipients", { params: { path: { ref: coopRef }, query: { limit: 100 } } }));
      return page.items.map((m) => ({ ref: m.actor_ref, label: m.label || `Thành viên ${m.actor_ref.slice(-4)}` }));
    } catch {
      return [];
    }
  }, [api, coopRef]);
  const yearNum = /^\d{4}$/.test(year) ? Number(year) : undefined;
  const code = seasonCode.trim() || undefined;
  const list = usePagedList(
    async (cursor) => {
      const page = await unwrap(api.GET("/v1/cooperatives/{ref}/household-yields", {
        params: { path: { ref: coopRef }, query: { state, opened_by: member || undefined, season_code: code, season_year: yearNum, cursor, limit: 50 } },
      }));
      if (!cursor) setTotals(page.totals);
      return page;
    },
    [api, coopRef, state, member, code, yearNum],
  );
  return <section className="card sec" aria-label="Sản lượng vụ nông hộ">
    <h3>Vụ nông hộ: thực tế so với dự kiến</h3>
    <p className="small mute">Chỉ xem. Dự kiến là số Thành viên nhập, không có thì hệ thống tính; chênh lệch = thực tế − dự kiến.</p>
    <div className="row" role="tablist" aria-label="Nhóm vụ">
      <button type="button" role="tab" aria-selected={state === "pending"} className={`btn btn-sm ${state === "pending" ? "btn-p" : "btn-o"}`} onClick={() => setState("pending")}>
        Chờ khai báo{totals ? ` (${totals.pending})` : ""}
      </button>
      <button type="button" role="tab" aria-selected={state === "declared"} className={`btn btn-sm ${state === "declared" ? "btn-p" : "btn-o"}`} onClick={() => setState("declared")}>
        Đã khai báo{totals ? ` (${totals.declared})` : ""}
      </button>
    </div>
    <form className="row" role="search" onSubmit={(e) => e.preventDefault()} style={{ alignItems: "flex-end" }}>
      <label className="field">Thành viên mở vụ
        <select className="select" value={member} onChange={(e) => setMember(e.target.value)}>
          <option value="">Tất cả</option>
          {(members.data ?? []).map((m) => <option key={m.ref} value={m.ref}>{m.label}</option>)}
        </select>
      </label>
      <label className="field">Mã mùa vụ
        <input className="input" value={seasonCode} maxLength={32} placeholder="VD: DX" onChange={(e) => setSeasonCode(e.target.value)} />
      </label>
      <label className="field">Năm
        <input className="input" inputMode="numeric" value={year} maxLength={4} placeholder="VD: 2026" onChange={(e) => setYear(e.target.value)} />
      </label>
    </form>
    {totals ? <p role="status" aria-label="Tổng hợp">
      {state === "pending"
        ? <>Dự kiến của {totals.pending} vụ chờ khai báo: {tonnesText(totals.pending_expected_t)}</>
        : <>Thực tế {tonnesText(totals.actual_t)} · dự kiến {tonnesText(totals.expected_t)} · chênh lệch {diffText(totals.diff_t, totals.diff_pct)}
          {totals.declared_without_estimate ? ` · ${totals.declared_without_estimate} vụ thiếu dự kiến không tính vào tổng` : ""}</>}
    </p> : null}
    {list.error ? <ProblemAlert problem={list.error} onRetry={list.reload} /> : null}
    {list.loading && !list.items.length ? <Loading /> : null}
    {!list.loading && !list.items.length && !list.error ? <Empty>Không có vụ phù hợp.</Empty> : null}
    {list.items.length ? <table className="tbl">
      <thead><tr>
        <th>Nông hộ</th><th>Vụ</th><th>Diện tích</th><th>Ngày thu hoạch dự kiến</th><th>Dự kiến</th>
        {state === "declared" ? <><th>Thực tế</th><th>Ngày thu hoạch</th><th>Chênh lệch</th><th>Trạng thái</th></> : null}
        <th>Người mở vụ</th>
      </tr></thead>
      <tbody>{list.items.map((s) => <tr key={s.season_ref}>
        <td>{householdLabel(s.household_ref)}</td>
        <td>{s.season_code} {s.season_year} · {s.crop_code}{s.variety_code ? ` · ${s.variety_code}` : ""}</td>
        <td>{formatDecimalVi(s.area_ha, { minDecimals: 2, maxDecimals: 2 })} ha</td>
        <td>{s.expected_harvest_date ? formatDateVi(s.expected_harvest_date) : "—"}{s.overdue ? <span className="c-warn"> · Quá ngày dự kiến</span> : null}</td>
        <td>{tonnesText(s.expected_t)}{s.expected_source === "entered" ? <span className="small mute"> (TV nhập)</span> : null}</td>
        {state === "declared" ? <>
          <td>{tonnesText(s.actual_t)}{s.zero_confirmed ? <span className="small mute"> (đã xác nhận 0)</span> : null}</td>
          <td>{s.harvest_date ? formatDateVi(s.harvest_date) : "—"}</td>
          <td>{diffText(s.diff_t, s.diff_pct)}</td>
          <td>{STATUS[s.status] ?? s.status}</td>
        </> : null}
        <td>{s.opened_by_label ?? `Thành viên ${s.opened_by.slice(-4)}`}</td>
      </tr>)}</tbody>
    </table> : null}
    <LoadMore hasMore={list.hasMore} loading={list.loading} onClick={list.loadMore} />
  </section>;
}
