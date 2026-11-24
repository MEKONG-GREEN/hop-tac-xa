"use client";

import { useRef, useState, type FormEvent } from "react";
import { unwrap } from "@/api/client";
import type { components } from "@/api/generated/application";
import { newIdempotencyKey } from "@/api/idempotency";
import type { ApiProblem } from "@/api/problem";
import { useAuth } from "@/auth/AuthProvider";
import { ProblemAlert } from "@/components/ProblemAlert";
import { toApiProblem } from "@/lib/useAsync";

type Season = components["schemas"]["HouseholdSeason"];

/** Kiểm sớm phía client; máy chủ kiểm lại (AH-70 AC2). */
export function stopReasonError(reason: string): string | null {
  const r = reason.trim();
  if (r.length < 10) return "Nhập lý do dừng vụ (ít nhất 10 ký tự).";
  if (r.length > 500) return "Lý do tối đa 500 ký tự.";
  return null;
}

/**
 * AH-70 (FN-03.10): dừng vụ đang canh tác kèm lý do. Vụ chuyển "Đã dừng vụ", diện tích được giải phóng, dữ liệu giữ
 * nguyên ở chế độ chỉ đọc. Hỏi xác nhận trước khi gửi; một Idempotency-Key cho mỗi nội dung gửi.
 */
export function StopSeasonForm({ coopRef, season, onStopped }: { coopRef: string; season: Season; onStopped: (s: Season) => void }) {
  const { api } = useAuth();
  const [open, setOpen] = useState(false);
  const [reason, setReason] = useState("");
  const [confirming, setConfirming] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const key = useRef<{ body: string; key: string } | null>(null);
  if (!open) return <button type="button" className="btn btn-o btn-sm" onClick={() => setOpen(true)}>Dừng vụ</button>;
  const submit = async (e: FormEvent) => {
    e.preventDefault(); if (busy) return;
    const err = stopReasonError(reason);
    setError(err); if (err) return;
    if (!confirming) { setConfirming(true); return; }
    const body = { expected_season_event_id: season.current_event_id, reason: reason.trim(), channel: "web" as const };
    const fingerprint = JSON.stringify(body);
    if (key.current?.body !== fingerprint) key.current = { body: fingerprint, key: newIdempotencyKey() };
    setBusy(true); setProblem(null);
    try {
      const stopped = await unwrap(api.POST("/v1/cooperatives/{ref}/households/{household_ref}/seasons/{season_ref}/stop", {
        params: { path: { ref: coopRef, household_ref: season.household_ref, season_ref: season.season_ref }, header: { "Idempotency-Key": key.current.key } }, body,
      }));
      onStopped(stopped);
    } catch (err) { setProblem(toApiProblem(err)); } finally { setBusy(false); }
  };
  return <form className="card stack" aria-label="Dừng vụ" onSubmit={(e) => void submit(e)}>
    <strong>Dừng vụ {season.season_code} {season.season_year}</strong>
    <p className="small mute">Vụ sẽ chuyển &quot;Đã dừng vụ&quot;, diện tích được giải phóng. Nhật ký giữ nguyên, chỉ xem; vụ không mở lại được.</p>
    <label className="field">Lý do dừng vụ<textarea className="input" maxLength={500} value={reason} disabled={busy} onChange={(e) => { setReason(e.target.value); setConfirming(false); }} /></label>
    {error ? <p role="alert">{error}</p> : null}
    {confirming ? <p role="status">Bấm &quot;Xác nhận dừng vụ&quot; để dừng. Thao tác không hoàn tác được.</p> : null}
    <div className="row">
      <button className="btn btn-p btn-sm" type="submit" disabled={busy}>{busy ? "Đang dừng…" : confirming ? "Xác nhận dừng vụ" : "Dừng vụ"}</button>
      <button className="btn btn-sm" type="button" disabled={busy} onClick={() => { setOpen(false); setConfirming(false); setReason(""); setError(null); setProblem(null); }}>Hủy</button>
    </div>
    {problem ? <ProblemAlert problem={problem} /> : null}
  </form>;
}
