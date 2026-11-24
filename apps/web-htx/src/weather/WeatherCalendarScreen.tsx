"use client";

import { useMemo, useState } from "react";
import { unwrap } from "@/api/client";
import { useAuth } from "@/auth/AuthProvider";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Loading, PageHead } from "@/components/ui";
import { CoopPicker, useHtxScope } from "@/htx/scope";
import { useAsync } from "@/lib/useAsync";
import { dualDate, pctText, rainText, tempText, updatedText, windText, type WeatherDay } from "./format";

const WEEK = ["T2", "T3", "T4", "T5", "T6", "T7", "CN"];

function vnToday(): string {
  return new Date(Date.now() + 7 * 3600_000).toISOString().slice(0, 10);
}
function shiftMonth(month: string, delta: number): string {
  const [y, m] = month.split("-").map(Number) as [number, number];
  const d = new Date(Date.UTC(y, m - 1 + delta, 1));
  return `${d.getUTCFullYear()}-${String(d.getUTCMonth() + 1).padStart(2, "0")}`;
}

/**
 * FN-TT-02 (AH-226): lịch tháng kết hợp thời tiết và âm lịch. Ngày sắp tới hiện dự báo, ngày đã qua hiện số liệu đã lưu
 * kèm nhãn "Thực đo" / "Dự báo cuối ngày"; ngày ngoài phạm vi dự báo không có số liệu. Chọn ngày để xem theo buổi.
 */
export function WeatherCalendarScreen() {
  const { api } = useAuth();
  const scope = useHtxScope();
  const coopRef = scope.coopRef || (scope.ownCoop ? "" : (scope.coops[0]?.ref ?? ""));
  const { ownCoop } = scope;
  const today = vnToday();
  const [month, setMonth] = useState(today.slice(0, 7));
  const [picked, setPicked] = useState<string>(today);
  const cal = useAsync(
    () => (coopRef ? unwrap(api.GET("/v1/weather/calendar", { params: { query: { month, ...(ownCoop ? {} : { cooperative_ref: coopRef }) } } })) : Promise.resolve(null)),
    [api, coopRef, ownCoop, month],
  );
  const days = useMemo(() => cal.data?.days ?? [], [cal.data]);
  const lead = useMemo(() => {
    if (!days.length) return 0;
    const [y, m, d] = days[0]!.date.split("-").map(Number) as [number, number, number];
    return (new Date(Date.UTC(y, m - 1, d)).getUTCDay() + 6) % 7;
  }, [days]);
  const selected = days.find((d) => d.date === picked) ?? null;
  const [y, m] = month.split("-");
  return (
    <div className="stack">
      <PageHead title="Lịch thời tiết" subtitle="Dương lịch và âm lịch; dự báo cho ngày sắp tới, số liệu đã lưu cho ngày đã qua" />
      {!ownCoop ? <CoopPicker scope={{ ...scope, coopRef }} allowAll={false} id="weather-calendar-coop" /> : null}
      <div className="row" style={{ gap: 8, alignItems: "center" }}>
        <button type="button" className="btn" onClick={() => setMonth(shiftMonth(month, -1))}>‹ Tháng trước</button>
        <strong aria-live="polite">Tháng {m}/{y}</strong>
        <button type="button" className="btn" onClick={() => setMonth(shiftMonth(month, 1))}>Tháng sau ›</button>
      </div>
      {cal.error ? <ProblemAlert problem={cal.error} /> : null}
      {cal.loading && !cal.data ? <Loading label="Đang tải lịch…" /> : null}
      {cal.data?.message ? <p role="status">{cal.data.message}</p> : null}
      {cal.data ? (
        <>
          <div role="grid" aria-label={`Lịch tháng ${m}/${y}`} style={{ display: "grid", gridTemplateColumns: "repeat(7, minmax(0, 1fr))", gap: 4 }}>
            {WEEK.map((w) => <div key={w} role="columnheader" className="small" style={{ textAlign: "center", fontWeight: 600 }}>{w}</div>)}
            {Array.from({ length: lead }, (_, i) => <div key={`pad-${i}`} aria-hidden="true" />)}
            {days.map((d) => <DayCell key={d.date} d={d} today={d.date === today} selected={d.date === picked} onPick={() => setPicked(d.date)} />)}
          </div>
          <p className="small">
            {cal.data.source.stale ? `${updatedText(cal.data.source.updated_at)} · ` : ""}
            {cal.data.source.label ? `Nguồn: ${cal.data.source.label}` : ""}
          </p>
          {selected ? <DayDetail d={selected} today={today} forecastDays={cal.data.forecast_days} /> : null}
        </>
      ) : null}
    </div>
  );
}

function DayCell({ d, today, selected, onPick }: { d: WeatherDay; today: boolean; selected: boolean; onPick: () => void }) {
  const day = Number(d.date.slice(8));
  const w = d.weather;
  const lunarMark = d.lunar.first_day || d.lunar.full_moon;
  return (
    <button type="button" role="gridcell" aria-selected={selected} aria-label={`${dualDate(d.date, d.lunar)}${w ? `, ${w.condition}` : ""}`}
      onClick={onPick} className="card" style={{ padding: 4, minHeight: 72, textAlign: "left", fontSize: 12, outline: selected ? "2px solid var(--primary, #1B7F18)" : undefined, fontWeight: today ? 700 : 400 }}>
      <div className="row" style={{ justifyContent: "space-between" }}>
        <span>{day}</span>
        <span className="small" style={{ color: lunarMark ? "var(--danger, #b42318)" : undefined }}>{d.lunar.label}</span>
      </div>
      {w ? (
        <>
          <div>{w.temperature_min_c ?? "—"}–{w.temperature_max_c ?? "—"}°</div>
          <div className="small">{rainText(w.precipitation_mm)}</div>
          {w.label ? <div className="small">{w.label}</div> : null}
        </>
      ) : null}
    </button>
  );
}

function DayDetail({ d, today, forecastDays }: { d: WeatherDay; today: string; forecastDays: number }) {
  const w = d.weather;
  const horizon = new Date(Date.parse(`${today}T00:00:00Z`) + (forecastDays - 1) * 86400_000).toISOString().slice(0, 10);
  return (
    <section className="card stack" aria-label="Chi tiết ngày">
      <strong>{dualDate(d.date, d.lunar)}{w?.label ? ` · ${w.label}` : ""}</strong>
      {!w ? (
        <p>{d.date > horizon ? "Ngoài phạm vi dự báo." : "Chưa có số liệu thời tiết cho ngày này."}</p>
      ) : w.hourly_available && w.periods.length ? (
        <div style={{ overflowX: "auto" }}>
          <table className="table">
            <thead><tr><th>Buổi</th><th>Trời</th><th>Nhiệt độ</th><th>Mưa</th><th>Khả năng mưa</th><th>Gió</th></tr></thead>
            <tbody>
              {w.periods.map((p) => (
                <tr key={p.name}><td>{p.name}</td><td>{p.condition}</td><td>{tempText(p.temperature_c)}</td><td>{rainText(p.precipitation_mm)}</td><td>{pctText(p.precipitation_probability_max)}</td><td>{windText(p.wind_speed_max_kmh)}</td></tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        <div aria-label="Cả ngày">
          <p>Cả ngày: {w.condition} · {tempText(w.temperature_min_c)}–{tempText(w.temperature_max_c)} · Mưa {rainText(w.precipitation_mm)} ({pctText(w.precipitation_probability_max)}) · Gió {windText(w.wind_speed_max_kmh, w.wind_direction)} · Độ ẩm {pctText(w.humidity_pct)}</p>
        </div>
      )}
    </section>
  );
}
