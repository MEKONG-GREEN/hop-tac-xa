"use client";

import { useState, type FormEvent } from "react";
import { unwrap } from "@/api/client";
import type { components } from "@/api/generated/admin";
import { newIdempotencyKey } from "@/api/idempotency";
import { fieldErrorMap, type ApiProblem } from "@/api/problem";
import { useAuth } from "@/auth/AuthProvider";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Loading, PageHead } from "@/components/ui";
import { toApiProblem, useAsync } from "@/lib/useAsync";

type Input = components["schemas"]["WeatherConfigInput"];

const REFRESH_LABEL: Record<number, string> = { 30: "30 phút/lần", 60: "1 giờ/lần", 120: "2 giờ/lần", 180: "3 giờ/lần", 360: "6 giờ/lần" };

/**
 * FN-TT-03 (AH-224): Admin cấu hình kết nối nhà cung cấp thời tiết. Kiểm tra kết nối lỗi thì không lưu được (EX-01, AC-04);
 * khóa API chỉ hiện 4 ký tự cuối (AC-03); đổi tần suất có hiệu lực ở lần lấy kế tiếp (AC-01).
 */
export function WeatherConfigScreen() {
  const { adminApi } = useAuth();
  const cfg = useAsync(() => unwrap(adminApi.GET("/v1/weather/config")), [adminApi]);
  const [edited, setForm] = useState<{ provider: string; refresh: number; days: number; key: string; dropKey: boolean } | null>(null);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [notice, setNotice] = useState("");
  // Form bắt đầu từ cấu hình hiện hành; chỉ thành trạng thái riêng khi người dùng sửa.
  const form = edited ?? (cfg.data ? { provider: cfg.data.provider, refresh: cfg.data.refresh_minutes, days: cfg.data.forecast_days, key: "", dropKey: false } : null);
  if (cfg.error) return <ProblemAlert problem={cfg.error} />;
  if (!cfg.data || !form) return <Loading />;
  const c = cfg.data;
  const errors = problem ? fieldErrorMap(problem) : {};
  const body = (): Input => ({ provider: form.provider as Input["provider"], refresh_minutes: form.refresh as Input["refresh_minutes"], forecast_days: form.days,
    ...(form.dropKey ? { api_key: "" } : form.key.trim() ? { api_key: form.key.trim() } : {}) });
  const test = async () => {
    if (busy) return;
    setBusy(true); setProblem(null); setNotice("");
    try {
      const r = await unwrap(adminApi.POST("/v1/weather/config/test", { params: { header: { "Idempotency-Key": newIdempotencyKey() } }, body: body() }));
      setNotice(`Kết nối được${r.temperature_c != null ? `: ${r.temperature_c}°C` : ""}${r.condition ? `, ${r.condition}` : ""}.`);
    } catch (e) { setProblem(toApiProblem(e)); } finally { setBusy(false); }
  };
  const save = async (e: FormEvent) => {
    e.preventDefault(); if (busy) return;
    setBusy(true); setProblem(null); setNotice("");
    try {
      await unwrap(adminApi.POST("/v1/weather/config/revisions", { params: { header: { "Idempotency-Key": newIdempotencyKey() } }, body: body() }));
      setForm(null);
      setNotice("Đã lưu. Cấu hình có hiệu lực từ lần lấy số liệu kế tiếp.");
      cfg.reload();
    } catch (err) { setProblem(toApiProblem(err)); } finally { setBusy(false); }
  };
  return (
    <div className="stack">
      <PageHead title="Kết nối thời tiết" subtitle="Máy chủ lấy số liệu theo tọa độ trụ sở HTX, lưu đệm và lưu theo ngày; ứng dụng không gọi nhà cung cấp trực tiếp" />
      <form className="card stack" onSubmit={(e) => void save(e)} aria-label="Cấu hình kết nối thời tiết" noValidate>
        {problem ? <ProblemAlert problem={problem} /> : null}
        {notice ? <p role="status">{notice}</p> : null}
        <label className="field">Nhà cung cấp
          <select className="select" value={form.provider} onChange={(e) => setForm({ ...form, provider: e.target.value })}>
            {c.providers.map((p) => <option key={p.code} value={p.code}>{p.label}</option>)}
          </select>
          {errors.provider ? <span className="err">{errors.provider}</span> : null}
        </label>
        <label className="field">Tần suất cập nhật
          <select className="select" value={form.refresh} onChange={(e) => setForm({ ...form, refresh: Number(e.target.value) })}>
            {c.refresh_options.map((m) => <option key={m} value={m}>{REFRESH_LABEL[m] ?? `${m} phút/lần`}</option>)}
          </select>
        </label>
        <label className="field">Số ngày dự báo
          <input className="input" type="number" min={1} max={c.max_forecast_days} value={form.days} onChange={(e) => setForm({ ...form, days: Number(e.target.value) })} aria-invalid={!!errors.forecast_days} />
          {errors.forecast_days ? <span className="err">{errors.forecast_days}</span> : null}
        </label>
        <label className="field">Khóa API
          <input className="input" type="password" autoComplete="off" value={form.key} disabled={form.dropKey}
            placeholder={c.api_key_set ? `${c.api_key_masked} — để trống nếu giữ nguyên` : "Không bắt buộc với gói miễn phí"} onChange={(e) => setForm({ ...form, key: e.target.value })} />
          {errors.api_key ? <span className="err">{errors.api_key}</span> : null}
        </label>
        {c.api_key_set ? (
          <label><input type="checkbox" checked={form.dropKey} onChange={(e) => setForm({ ...form, dropKey: e.target.checked, key: "" })} /> Bỏ khóa API đang lưu ({c.api_key_masked})</label>
        ) : null}
        <div className="row" style={{ gap: 8 }}>
          <button type="button" className="btn" disabled={busy} onClick={() => void test()}>Kiểm tra kết nối</button>
          <button type="submit" className="btn btn-p" disabled={busy}>{busy ? "Đang xử lý…" : "Lưu"}</button>
        </div>
        {c.configured && c.updated_at ? <p className="small">Phiên bản {c.version} · lưu lúc {new Date(c.updated_at).toLocaleString("vi-VN", { timeZone: "Asia/Ho_Chi_Minh" })}</p> : <p className="small">Chưa cấu hình: máy chủ chưa lấy số liệu thời tiết.</p>}
      </form>
    </div>
  );
}
