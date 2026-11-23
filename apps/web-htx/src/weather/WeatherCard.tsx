"use client";

import { unwrap } from "@/api/client";
import { useAuth } from "@/auth/AuthProvider";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Loading } from "@/components/ui";
import { CoopPicker, useHtxScope } from "@/htx/scope";
import { useAsync } from "@/lib/useAsync";
import { longDate, pctText, rainText, tempText, updatedText, windText, type WeatherToday } from "./format";

/**
 * FN-TT-01 (AH-225): thẻ Thời tiết hôm nay ở đầu Trang chủ của ba vai. Chỉ số liệu từ bộ đệm máy chủ — không lời khuyên,
 * chỉ số hay cảnh báo (BR-02). Admin chọn HTX ở bộ lọc, thẻ đổi theo trụ sở HTX đó (AC-06).
 */
export function WeatherCard() {
  const { api } = useAuth();
  const scope = useHtxScope();
  // Admin: chưa chọn thì dùng HTX đầu tiên để có số liệu ngay.
  const coopRef = scope.coopRef || (scope.ownCoop ? "" : (scope.coops[0]?.ref ?? ""));
  const { ownCoop } = scope;
  const today = useAsync(
    () => (coopRef ? unwrap(api.GET("/v1/weather/today", { params: { query: ownCoop ? {} : { cooperative_ref: coopRef } } })) : Promise.resolve(null)),
    [api, coopRef, ownCoop],
  );
  return (
    <section className="card stack" aria-label="Thời tiết hôm nay" style={{ marginBottom: 16 }}>
      <div className="row" style={{ justifyContent: "space-between", flexWrap: "wrap", gap: 8 }}>
        <strong>Thời tiết hôm nay</strong>
        {!ownCoop ? <CoopPicker scope={{ ...scope, coopRef }} allowAll={false} id="weather-coop" /> : null}
      </div>
      {today.error ? <ProblemAlert problem={today.error} /> : today.loading && !today.data ? <Loading label="Đang tải thời tiết…" /> : today.data ? <TodayBody t={today.data} /> : null}
    </section>
  );
}

function TodayBody({ t }: { t: WeatherToday }) {
  const head = (
    <p className="small">
      {longDate(t.date)} · {t.lunar.label} ÂL
    </p>
  );
  if (t.status !== "ok") {
    return (
      <>
        {head}
        <p role="status">{t.message}</p>
      </>
    );
  }
  const c = t.current;
  const d = t.today;
  return (
    <>
      {head}
      <div className="row" style={{ alignItems: "baseline", gap: 12, flexWrap: "wrap" }}>
        <span style={{ fontSize: 28, fontWeight: 700 }}>{tempText(c?.temperature_c ?? d?.temperature_max_c)}</span>
        <span>{c?.condition || d?.condition}</span>
        {d ? <span className="small">Thấp nhất {tempText(d.temperature_min_c)} · Cao nhất {tempText(d.temperature_max_c)}</span> : null}
      </div>
      <dl className="row" style={{ gap: 16, flexWrap: "wrap", margin: 0 }}>
        <div><dt className="small">Mưa</dt><dd style={{ margin: 0 }}>{rainText(d?.precipitation_mm ?? c?.precipitation_mm)} ({pctText(d?.precipitation_probability_max ?? c?.precipitation_probability)})</dd></div>
        <div><dt className="small">Gió</dt><dd style={{ margin: 0 }}>{windText(c?.wind_speed_kmh ?? d?.wind_speed_max_kmh, c?.wind_direction || d?.wind_direction)}</dd></div>
        <div><dt className="small">Độ ẩm</dt><dd style={{ margin: 0 }}>{pctText(c?.humidity_pct ?? d?.humidity_pct)}</dd></div>
      </dl>
      <p className="small">
        {t.source.stale ? <span role="status">{updatedText(t.source.updated_at)} · </span> : null}
        Nguồn: {t.source.label}
      </p>
    </>
  );
}
